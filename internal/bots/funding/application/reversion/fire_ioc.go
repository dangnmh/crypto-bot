package reversion

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"crypto-bot/internal/bots/funding/application/orders"
	fundingdomain "crypto-bot/internal/bots/funding/domain"
	"crypto-bot/internal/infrastructure/exchange"
)

func (r *StatelessRunner) handleFireIOC(ctx context.Context, confirmedEvt ConfirmedEvent) error {
	r.log.Info("handleFireIOC SettleTime", slog.Time("settle", confirmedEvt.SettleTime))
	settleTime := confirmedEvt.SettleTime
	if settleTime.IsZero() {
		err := errors.New("settle time not found")
		r.abort(ctx, confirmedEvt.Symbol, err.Error())
		return err
	}

	cfg, ok := r.getSymbolConfig(confirmedEvt.Symbol)
	if !ok {
		err := errors.New("symbol config not found")
		r.abort(ctx, confirmedEvt.Symbol, err.Error())
		return err
	}

	latencyMs := r.deps.Clock.LatencyMs()
	maxLatency := time.Duration(cfg.FundingReversion.MaxLatency)
	if maxLatency > 0 && time.Duration(latencyMs)*time.Millisecond > maxLatency {
		err := errors.New("latency too high")
		r.abort(ctx, confirmedEvt.Symbol, err.Error())
		return err
	}

	oneWayMs := latencyMs / 2
	bufferTime := time.Duration(cfg.FundingReversion.BufferTime)
	fireOffset := time.Duration(oneWayMs)*time.Millisecond + bufferTime

	snapshotOffset := 50 * time.Millisecond
	if fireOffset > snapshotOffset {
		snapshotOffset = fireOffset
	}
	if !r.WaitUntil(ctx, confirmedEvt.Symbol, settleTime.Add(-snapshotOffset)) {
		r.abort(ctx, confirmedEvt.Symbol, "wait snapshot context canceled")
		return ctx.Err()
	}

	c := confirmedEvt.Candidate
	if err := r.refreshPrice(ctx, &c); err != nil {
		r.abort(ctx, c.Symbol, "refresh price fail: "+err.Error())
		return err
	}
	c.Volume = c.CalculateVolume()
	safety := r.globalCfg.System.Safety
	c.SafetyResult = c.ApplySafetySizing(fundingdomain.SafetyLimits{
		MaxImpactRatio: safety.MaxImpactRatio,
		MinVol24USD:    safety.MinVol24USD,
	})
	if !c.SafetyResult.Passed {
		evt := IOCFiredEvent{
			Flow:          FlowReversion,
			Symbol:        c.Symbol,
			Side:          c.Side,
			CloseSide:     c.CloseSide,
			FireTimestamp: r.deps.Clock.Now(),
			SettleTime:    settleTime,
			Error:         c.SafetyResult.RejectReason,
		}
		_ = r.publishEvent(ctx, TopicReversionIOCFired, evt)
		r.abort(ctx, c.Symbol, c.SafetyResult.RejectReason)
		return errors.New(c.SafetyResult.RejectReason)
	}

	if !r.WaitUntil(ctx, confirmedEvt.Symbol, settleTime.Add(-fireOffset)) {
		r.abort(ctx, confirmedEvt.Symbol, "wait fire context canceled")
		return ctx.Err()
	}
	fireTime := r.deps.Clock.Now()

	res := orders.FireIOC(ctx, r.deps.Client, &c, r.deps.Clock, r.log)

	if res.IsSuccess() {
		evt := IOCFiredEvent{
			Flow:          FlowReversion,
			Symbol:        c.Symbol,
			OrderID:       res.OrderID,
			Side:          c.Side,
			CloseSide:     c.CloseSide,
			OrderType:     exchange.OrderTypeIOC,
			IntendedPrice: res.Price,
			Volume:        res.Volume,
			TPPrice:       res.TakeProfitPrice,
			SLPrice:       res.StopLossPrice,
			SettleTime:    settleTime,
			FireTimestamp: fireTime,
			LatencyRTTMs:  latencyMs,
		}
		return r.publishEvent(ctx, TopicReversionIOCFired, evt)
	}

	errText := "IOC order failed"
	if res.Error != nil {
		errText = res.Error.Error()
	}
	evt := IOCFiredEvent{
		Flow:          FlowReversion,
		Symbol:        c.Symbol,
		OrderID:       res.OrderID,
		Side:          c.Side,
		CloseSide:     c.CloseSide,
		IntendedPrice: res.Price,
		Volume:        res.Volume,
		SettleTime:    settleTime,
		FireTimestamp: fireTime,
		LatencyRTTMs:  latencyMs,
		Error:         errText,
	}
	_ = r.publishEvent(ctx, TopicReversionIOCFired, evt)
	r.abort(ctx, c.Symbol, errText)
	return errors.New(errText)
}
