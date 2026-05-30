package reversion

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"time"

	"crypto-bot/internal/bots/funding/application/orders"
	fundingdomain "crypto-bot/internal/bots/funding/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
)

const (
	iocOutcomePollTimeout  = 2 * time.Second
	iocOutcomePollInterval = 200 * time.Millisecond
)

func (r *StatelessRunner) handleFireIOC(ctx context.Context, confirmedEvt ConfirmedEvent) error {
	r.log.Info("handleFireIOC SettleTime", slog.Time("settle", confirmedEvt.SettleTime))
	settleTime := confirmedEvt.SettleTime
	if settleTime.IsZero() {
		err := errors.New("settle time not found")
		r.abortAfter(ctx, confirmedEvt.BaseReversionEvent, confirmedEvt.Symbol, err.Error())
		return err
	}

	cfg, ok := r.getSymbolConfig(confirmedEvt.Symbol)
	if !ok {
		err := errors.New("symbol config not found")
		r.abortAfter(ctx, confirmedEvt.BaseReversionEvent, confirmedEvt.Symbol, err.Error())
		return err
	}

	latencyMs := r.deps.Clock.LatencyMs()
	maxLatency := time.Duration(cfg.FundingReversion.MaxLatency)
	if maxLatency > 0 && time.Duration(latencyMs)*time.Millisecond > maxLatency {
		err := errors.New("latency too high")
		r.abortAfter(ctx, confirmedEvt.BaseReversionEvent, confirmedEvt.Symbol, err.Error())
		return err
	}

	oneWayMs := latencyMs / 2
	bufferTime := time.Duration(cfg.FundingReversion.BufferTime)
	fireOffset := time.Duration(oneWayMs)*time.Millisecond + bufferTime

	// Ensure snapshotOffset is at least fireOffset + 300ms, and at least 500ms overall
	// to avoid race conditions during the price refresh and safety calculation.
	snapshotOffset := max(fireOffset+300*time.Millisecond, 500*time.Millisecond)

	evt := FireTimingReadyEvent{
		BaseReversionEvent: nextReversionBase(confirmedEvt.BaseReversionEvent, confirmedEvt.Symbol, r.deps.Clock.Now()),
		Candidate:          confirmedEvt.Candidate,
		FundingRate:        confirmedEvt.FundingRate,
		LatencyRTTMs:       latencyMs,
		FireOffsetMs:       fireOffset.Milliseconds(),
		SnapshotOffsetMs:   snapshotOffset.Milliseconds(),
	}

	return r.publishEvent(ctx, TopicReversionFireTimingReady, evt)
}

func (r *StatelessRunner) handleFireTimingReady(ctx context.Context, evt FireTimingReadyEvent) error {
	snapshotOffset := time.Duration(evt.SnapshotOffsetMs) * time.Millisecond
	if err := r.waitUntilFuture(ctx, evt.Symbol, evt.SettleTime.Add(-snapshotOffset)); err != nil {
		r.abortAfter(ctx, evt.BaseReversionEvent, evt.Symbol, "wait snapshot failed: "+err.Error())
		return err
	}

	c := evt.Candidate
	if err := r.refreshPrice(ctx, &c); err != nil {
		r.abortAfter(ctx, evt.BaseReversionEvent, c.Symbol, "refresh price fail: "+err.Error())
		return err
	}

	requestedVolume := c.CalculateVolume()
	c.Volume = requestedVolume
	ioc, err := c.CalculateIOCPrice()
	if err != nil {
		r.abortAfter(ctx, evt.BaseReversionEvent, c.Symbol, "IOC calc failed: "+err.Error())
		return err
	}
	refPrice := executionRefPrice(c)
	if ioc > 0 && refPrice > 0 {
		c.Slippage = decmath.Mul(decmath.Div(math.Abs(decmath.Sub(ioc, refPrice)), refPrice), 100.0)
	}

	safety := r.globalCfg.System.Safety
	c.SafetyResult = c.ApplySafetySizing(fundingdomain.SafetyLimits{
		MaxImpactRatio: safety.MaxImpactRatio,
		MinVol24USD:    safety.MinVol24USD,
	})
	passed := c.SafetyResult != nil && c.SafetyResult.Passed
	rejectReason := ""
	if c.SafetyResult != nil {
		rejectReason = c.SafetyResult.RejectReason
	}

	next := FirePlanCheckedEvent{
		BaseReversionEvent: nextReversionBase(evt.BaseReversionEvent, c.Symbol, r.deps.Clock.Now()),
		Candidate:          c,
		LatencyRTTMs:       evt.LatencyRTTMs,
		FireOffsetMs:       evt.FireOffsetMs,
		BestBid:            c.BestBid,
		BestAsk:            c.BestAsk,
		LastPrice:          c.LastPrice,
		IOCPrice:           ioc,
		RefPrice:           refPrice,
		Slippage:           c.Slippage,
		RequestedVolume:    requestedVolume,
		AdjustedVolume:     c.Volume,
		Passed:             passed,
		RejectReason:       rejectReason,
	}

	return r.publishEvent(ctx, TopicReversionFirePlanChecked, next)
}

func (r *StatelessRunner) handleFirePlanChecked(ctx context.Context, evt FirePlanCheckedEvent) error {
	c := evt.Candidate
	if !evt.Passed {
		submitted := IOCSubmittedEvent{
			BaseReversionEvent: nextReversionBase(evt.BaseReversionEvent, c.Symbol, r.deps.Clock.Now()),
			Side:               c.Side,
			CloseSide:          c.CloseSide,
			FireTimestamp:      r.deps.Clock.Now(),
			Error:              evt.RejectReason,
		}
		_ = r.publishEvent(ctx, TopicReversionIOCSubmitted, submitted)
		r.abortAfter(ctx, evt.BaseReversionEvent, c.Symbol, evt.RejectReason)
		return errors.New(evt.RejectReason)
	}

	fireOffset := time.Duration(evt.FireOffsetMs) * time.Millisecond
	targetTime := evt.SettleTime.Add(-fireOffset)
	if r.deps.Clock.Until(targetTime) > 0 {
		if err := r.waitUntilFuture(ctx, evt.Symbol, targetTime); err != nil {
			r.abortAfter(ctx, evt.BaseReversionEvent, evt.Symbol, "wait fire failed: "+err.Error())
			return err
		}
	}

	fireTime := r.deps.Clock.Now()

	next := FireWindowReachedEvent{
		BaseReversionEvent: nextReversionBase(evt.BaseReversionEvent, c.Symbol, fireTime),
		Candidate:          c,
		LatencyRTTMs:       evt.LatencyRTTMs,
		FireTimestamp:      fireTime,
	}

	return r.publishEvent(ctx, TopicReversionFireWindowReached, next)
}

func (r *StatelessRunner) handleFireWindowReached(ctx context.Context, evt FireWindowReachedEvent) error {
	cfg, ok := r.getSymbolConfig(evt.Symbol)
	if !ok {
		r.log.Error("Symbol config not found for position watch", slog.String("symbol", evt.Symbol))
		return nil
	}
	timeout := time.Duration(cfg.FundingReversion.PostSettleTimeout)
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	next := PositionWatchReadyEvent{
		BaseReversionEvent: nextReversionBase(evt.BaseReversionEvent, evt.Symbol, r.deps.Clock.Now()),
		Candidate:          evt.Candidate,
		LatencyRTTMs:       evt.LatencyRTTMs,
		FireTimestamp:      evt.FireTimestamp,
		Timeout:            timeout,
	}
	watchBase := next.BaseReversionEvent
	watchBase.Topic = TopicReversionPositionWatchReady
	r.deps.OrderNotifier.OnPositionUpdate(ctx, evt.Symbol, timeout*2, func(pos exchange.PersonalPositionUpdate) {
		r.handlePositionUpdate(ctx, pos, watchBase)
	})
	return r.publishEvent(ctx, TopicReversionPositionWatchReady, next)
}

func (r *StatelessRunner) handlePositionWatchReady(ctx context.Context, evt PositionWatchReadyEvent) error {
	c := evt.Candidate
	res := orders.FireIOC(ctx, r.deps.Client, &c, r.deps.Clock, r.log)

	if res.IsSuccess() {
		base := nextNotifyReversionBase(evt.BaseReversionEvent, c.Symbol, evt.FireTimestamp)
		base.OrderID = res.OrderID
		base.ExternalID = res.ExternalID
		next := IOCSubmittedEvent{
			BaseReversionEvent: base,
			OrderID:            res.OrderID,
			ExternalID:         res.ExternalID,
			Side:               c.Side,
			CloseSide:          c.CloseSide,
			OrderType:          exchange.OrderTypeIOC,
			IntendedPrice:      res.Price,
			Volume:             res.Volume,
			TPPrice:            res.TakeProfitPrice,
			SLPrice:            res.StopLossPrice,
			FireTimestamp:      evt.FireTimestamp,
			LatencyRTTMs:       evt.LatencyRTTMs,
		}
		return r.publishEvent(ctx, TopicReversionIOCSubmitted, next)
	}

	errText := "IOC order failed"
	if res.Error != nil {
		errText = res.Error.Error()
	}
	base := nextReversionBase(evt.BaseReversionEvent, c.Symbol, evt.FireTimestamp)
	base.OrderID = res.OrderID
	base.ExternalID = res.ExternalID
	next := IOCSubmittedEvent{
		BaseReversionEvent: base,
		OrderID:            res.OrderID,
		ExternalID:         res.ExternalID,
		Side:               c.Side,
		CloseSide:          c.CloseSide,
		IntendedPrice:      res.Price,
		Volume:             res.Volume,
		FireTimestamp:      evt.FireTimestamp,
		LatencyRTTMs:       evt.LatencyRTTMs,
		Error:              errText,
	}
	_ = r.publishEvent(ctx, TopicReversionIOCSubmitted, next)
	r.abortAfter(ctx, evt.BaseReversionEvent, c.Symbol, errText)
	return errors.New(errText)
}
