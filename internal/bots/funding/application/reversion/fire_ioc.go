package reversion

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"crypto-bot/internal/bots/funding/application/orders"
	fundingdomain "crypto-bot/internal/bots/funding/domain"
	shared "crypto-bot/internal/domain"
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
		r.abortAfter(ctx, confirmedEvt.BaseReversionEvent, confirmedEvt.Symbol, ReversionReason(err.Error()))
		return err
	}

	latencyMs := r.deps.Clock.LatencyMs()
	maxLatency := time.Duration(confirmedEvt.Candidate.Config.FundingReversion.MaxLatency)
	if maxLatency > 0 && time.Duration(latencyMs)*time.Millisecond > maxLatency {
		err := errors.New("latency too high")
		r.abortAfter(ctx, confirmedEvt.BaseReversionEvent, confirmedEvt.Symbol, ReversionReason(err.Error()))
		return err
	}

	evt := MarginModeReadyEvent{
		BaseReversionEvent: nextReversionBase(confirmedEvt.BaseReversionEvent, confirmedEvt.Symbol, r.deps.Clock.Now()),
		Candidate:          confirmedEvt.Candidate,
		FundingRate:        confirmedEvt.FundingRate,
	}

	return r.publishEvent(ctx, TopicReversionMarginModeReady, evt)
}

func (r *StatelessRunner) handleMarginModeReady(ctx context.Context, evt MarginModeReadyEvent) error {
	r.log.Info("handleMarginModeReady", slog.String("symbol", evt.Symbol))

	marginMode := "ISOLATED"
	if evt.Candidate.Config.ParsedOpenType == 2 {
		marginMode = "CROSS"
	}

	err := r.deps.Client.SwitchMarginMode(ctx, evt.Symbol, marginMode, evt.Candidate.Config.Leverage, evt.Candidate.Side)
	if err != nil {
		r.log.ErrorContext(ctx, "Failed to switch margin mode", slog.Any("error", err), slog.String("symbol", evt.Symbol))
		r.abortAfter(ctx, evt.BaseReversionEvent, evt.Symbol, ReversionReason("switch margin mode failed: "+err.Error()))
		return fmt.Errorf("switch margin mode failed: %w", err)
	}

	latencyMs := r.deps.Clock.LatencyMs()
	oneWayMs := latencyMs / 2
	bufferTime := time.Duration(evt.Candidate.Config.FundingReversion.BufferTime)
	fireOffset := time.Duration(oneWayMs)*time.Millisecond + bufferTime

	// Ensure snapshotOffset is at least fireOffset + 300ms, and at least 300ms overall
	// to avoid race conditions during the price refresh and safety calculation.
	snapshotOffset := max(fireOffset+300*time.Millisecond, 300*time.Millisecond)

	nextEvt := FireTimingReadyEvent{
		BaseReversionEvent: nextReversionBase(evt.BaseReversionEvent, evt.Symbol, r.deps.Clock.Now()),
		Candidate:          evt.Candidate,
		FundingRate:        evt.FundingRate,
		LatencyRTTMs:       latencyMs,
		FireOffsetMs:       fireOffset.Milliseconds(),
		SnapshotOffsetMs:   snapshotOffset.Milliseconds(),
	}

	return r.publishEvent(ctx, TopicReversionFireTimingReady, nextEvt)
}

func (r *StatelessRunner) handleFireTimingReady(ctx context.Context, evt FireTimingReadyEvent) error {
	snapshotOffset := time.Duration(evt.SnapshotOffsetMs) * time.Millisecond
	if err := r.waitUntilFuture(ctx, evt.Symbol, evt.SettleTime.Add(-snapshotOffset)); err != nil {
		r.abortAfter(ctx, evt.BaseReversionEvent, evt.Symbol, ReversionReason("wait snapshot failed: "+err.Error()))
		return err
	}

	c := evt.Candidate
	if err := r.refreshPrice(ctx, &c); err != nil {
		r.abortAfter(ctx, evt.BaseReversionEvent, c.Symbol, ReversionReason("refresh price fail: "+err.Error()))
		return err
	}

	requestedVolume := c.CalculateVolume()
	c.Volume = requestedVolume
	ioc, err := c.CalculateIOCPrice()
	if err != nil {
		r.abortAfter(ctx, evt.BaseReversionEvent, c.Symbol, ReversionReason("IOC calc failed: "+err.Error()))
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
			Candidate:          c,
			Side:               c.Side,
			CloseSide:          c.CloseSide,
			FireTimestamp:      r.deps.Clock.Now(),
			Error:              evt.RejectReason,
		}
		_ = r.publishEvent(ctx, TopicReversionIOCSubmitted, submitted)
		r.abortAfter(ctx, evt.BaseReversionEvent, c.Symbol, ReversionReason(evt.RejectReason))
		return errors.New(evt.RejectReason)
	}

	fireOffset := time.Duration(evt.FireOffsetMs) * time.Millisecond
	targetTime := evt.SettleTime.Add(-fireOffset)
	if r.deps.Clock.Until(targetTime) > 0 {
		if err := r.waitUntilFuture(ctx, evt.Symbol, targetTime); err != nil {
			r.abortAfter(ctx, evt.BaseReversionEvent, evt.Symbol, ReversionReason("wait fire failed: "+err.Error()))
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
	timeout := time.Duration(evt.Candidate.Config.FundingReversion.PostSettleTimeout)
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	// Preemptively set the configured leverage on the exchange before the fire window to eliminate any order placement latency.
	leverage := evt.Candidate.Config.Leverage
	if leverage > 0 && !evt.SupportLeverageOnOrder {
		r.log.InfoContext(ctx, "Adjusting leverage before fire window", slog.String("symbol", evt.Symbol), slog.Int("leverage", leverage))
		posType := exchange.PositionTypeLong
		if !evt.Candidate.Side.IsLong() {
			posType = exchange.PositionTypeShort
		}

		err := r.deps.Client.ChangeLeverage(ctx, exchange.ChangeLeverageRequest{
			Symbol:       evt.Symbol,
			Leverage:     leverage,
			OpenType:     shared.OpenType(evt.Candidate.Config.ParsedOpenType),
			PositionType: posType,
		})
		if err != nil {
			r.log.ErrorContext(ctx, "Failed to adjust leverage", slog.Any("error", err), slog.String("symbol", evt.Symbol))
			r.abortAfter(ctx, evt.BaseReversionEvent, evt.Symbol, ReversionReason("change leverage failed: "+err.Error()))
			return fmt.Errorf("change leverage failed: %w", err)
		}
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
			Candidate:          c,
			OrderID:            res.OrderID,
			ExternalID:         res.ExternalID,
			Side:               c.Side,
			CloseSide:          c.CloseSide,
			OrderType:          exchange.OrderTypeIOC,
			IntendedPrice:      res.Price,
			Volume:             res.Volume,
			TPPrice:            res.TakeProfitPrice,
			SLPrice:            res.StopLossPrice,
			TPSLSubmitted:      res.TPSLSubmitted,
			FireTimestamp:      evt.FireTimestamp,
			LatencyRTTMs:       evt.LatencyRTTMs,
		}

		if !res.TPSLSubmitted && (res.TakeProfitPrice > 0 || res.StopLossPrice > 0) {
			tpslEvt := TPSLRequiredEvent{
				BaseReversionEvent: nextReversionBase(evt.BaseReversionEvent, c.Symbol, r.deps.Clock.Now()),
				OrderID:            res.OrderID,
				Side:               c.Side,
				PositionMode:       shared.PositionMode(c.Config.ParsedPositionMode),
				TakeProfitPrice:    res.TakeProfitPrice,
				StopLossPrice:      res.StopLossPrice,
				Volume:             res.Volume,
			}
			go r.publishTPSLBackground(ctx, tpslEvt)
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
		Candidate:          c,
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
	r.abortAfter(ctx, evt.BaseReversionEvent, c.Symbol, ReversionReason(errText))
	return errors.New(errText)
}

func (r *StatelessRunner) publishTPSLBackground(ctx context.Context, evt TPSLRequiredEvent) {
	detached := context.WithoutCancel(ctx)
	if err := r.publishEvent(detached, TopicReversionTPSLRequired, evt); err != nil {
		r.log.Error("Failed to publish TopicReversionTPSLRequired", slog.Any("error", err))
	}
}
