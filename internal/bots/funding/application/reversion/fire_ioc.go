package reversion

import (
	"context"
	"errors"
	"time"

	"crypto-bot/internal/bots/funding/application"
	"crypto-bot/internal/bots/funding/application/orders"
	fundingdomain "crypto-bot/internal/bots/funding/domain"
	"crypto-bot/internal/infrastructure/exchange"
)

func (s *Strategy) handleFireIOC(ctx context.Context, confirmedEvt ConfirmedEvent) error {
	s.mu.Lock()
	settleTime := s.settleTime
	s.mu.Unlock()

	if settleTime.IsZero() {
		err := errors.New("settle time not found")
		s.abort(ctx, err.Error())
		return err
	}

	cfg := s.cfg
	latencyMs := s.deps.Clock.LatencyMs()
	maxLatency := time.Duration(cfg.FundingReversion.MaxLatency)
	if maxLatency > 0 && time.Duration(latencyMs)*time.Millisecond > maxLatency {
		err := errors.New("latency too high")
		s.abort(ctx, err.Error())
		return err
	}

	oneWayMs := latencyMs / 2
	bufferTime := time.Duration(cfg.FundingReversion.BufferTime)
	fireOffset := time.Duration(oneWayMs)*time.Millisecond + bufferTime

	snapshotOffset := 50 * time.Millisecond
	if fireOffset > snapshotOffset {
		snapshotOffset = fireOffset
	}
	if !s.WaitUntil(ctx, settleTime.Add(-snapshotOffset)) {
		s.abort(ctx, "wait snapshot context canceled")
		return ctx.Err()
	}

	c := s.getCandidateCopy()
	if err := s.refreshPrice(ctx, &c); err != nil {
		s.abort(ctx, "refresh price fail: "+err.Error())
		return err
	}
	c.Volume = c.CalculateVolume()
	safety := s.global.System.Safety
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
			FireTimestamp: s.deps.Clock.Now(),
			SettleTime:    settleTime,
			Error:         c.SafetyResult.RejectReason,
		}
		_ = s.publishEvent(ctx, TopicReversionIOCFired, evt)
		s.abort(ctx, c.SafetyResult.RejectReason)
		return errors.New(c.SafetyResult.RejectReason)
	}

	if !s.WaitUntil(ctx, settleTime.Add(-fireOffset)) {
		s.abort(ctx, "wait fire context canceled")
		return ctx.Err()
	}
	fireTime := s.deps.Clock.Now()

	res := orders.FireIOC(ctx, s.deps.Client, &c, s.deps.Clock, s.log)
	s.setCandidate(c)

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
		s.mu.Lock()
		s.order = &application.OrderRef{
			Symbol:    c.Symbol,
			OrderID:   res.OrderID,
			OrderType: exchange.OrderTypeIOC,
		}
		s.mu.Unlock()

		return s.publishEvent(ctx, TopicReversionIOCFired, evt)
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
	_ = s.publishEvent(ctx, TopicReversionIOCFired, evt)
	s.abort(ctx, errText)
	return errors.New(errText)
}
