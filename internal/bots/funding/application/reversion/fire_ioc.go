package reversion

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	fundingdomain "crypto-bot/internal/bots/funding/domain"
	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	ordermanager "crypto-bot/internal/trading/ordermanager"
	"crypto-bot/pkg/decmath"
)

func (r *StatelessRunner) now() time.Time {
	if r != nil && r.deps.Clock != nil {
		return r.deps.Clock.Now()
	}
	return time.Now()
}

func (r *StatelessRunner) latencyMs() int64 {
	if r != nil && r.deps.Clock != nil {
		return r.deps.Clock.LatencyMs()
	}
	return 0
}

func (r *StatelessRunner) handleFireIOC(ctx context.Context, confirmedEvt ConfirmedEvent) error {
	r.log.Info("handleFireIOC SettleTime", slog.Time("settle", confirmedEvt.SettleTime))
	settleTime := confirmedEvt.SettleTime
	if settleTime.IsZero() {
		err := errors.New("settle time not found")
		r.abortAfter(ctx, confirmedEvt.BaseReversionEvent, confirmedEvt.Symbol, err.Error())
		return err
	}

	latencyMs := r.latencyMs()
	maxLatency := time.Duration(confirmedEvt.Candidate.Config.FundingReversion.MaxLatency)
	if maxLatency > 0 && time.Duration(latencyMs)*time.Millisecond > maxLatency {
		err := errors.New("latency too high")
		r.abortAfter(ctx, confirmedEvt.BaseReversionEvent, confirmedEvt.Symbol, err.Error())
		return err
	}

	evt := MarginModeReadyEvent{
		BaseReversionEvent: nextReversionBase(confirmedEvt.BaseReversionEvent, confirmedEvt.Symbol, r.now()),
		Candidate:          confirmedEvt.Candidate,
	}

	return r.publishEvent(ctx, TopicReversionMarginModeReady, evt)
}

func (r *StatelessRunner) dispatchOrderManagerIntent(
	ctx context.Context,
	evt FirePlanCheckedEvent,
	targetTime time.Time,
	iocPrice, tpPrice, slPrice float64,
) error {
	r.log.InfoContext(ctx, "Dispatching Funding Reversion order via OrderManager with fresh price snapshot",
		slog.String("symbol", evt.Symbol),
		slog.Float64("ioc_price", iocPrice),
		slog.Float64("tp_price", tpPrice),
		slog.Float64("sl_price", slPrice),
		slog.Float64("volume", evt.AdjustedVolume),
		slog.Time("fire_time", targetTime),
	)

	latencyMs := evt.LatencyRTTMs
	now := r.now()
	oneWayMs := max(latencyMs/2, 0)
	bufferTime := time.Duration(evt.Candidate.Config.FundingReversion.BufferTime)

	cand := evt.Candidate
	marginMode := shared.MarginModeIsolated
	if cand.Config.ParsedOpenType == 2 {
		marginMode = shared.MarginModeCross
	}
	posMode := shared.PositionMode(cand.Config.ParsedPositionMode)
	leverage := cand.Config.Leverage

	var settleTimePtr *time.Time
	if !evt.SettleTime.IsZero() {
		settleTimePtr = &evt.SettleTime
	}

	orderIntent := ordermanager.OrderIntentEvent{
		ReqID:                evt.ReqID,
		ClientOrderID:        evt.ExternalID,
		Symbol:               evt.Symbol,
		Exchange:             evt.Exchange,
		MarketType:           ordermanager.MarketTypeFuture,
		StrategyType:         ordermanager.StrategyFundingReversion,
		PreTopic:             TopicReversionFirePlanChecked,
		NextTopic:            ordermanager.TopicOrderIntent,
		Timestamp:            now,
		Side:                 cand.Side,
		OrderType:            ordermanager.OrderTypeIOC,
		Price:                iocPrice,
		Volume:               evt.AdjustedVolume,
		ContractSize:         cand.ContractSize,
		MarginMode:           marginMode,
		PositionMode:         posMode,
		Leverage:             leverage,
		MarginUSDT:           cand.Config.MarginUSDT,
		FundingRate:          cand.FundingRate,
		Vol24hUSDT:           cand.Vol24USDT,
		TakeProfitPrice:      tpPrice,
		StopLossPrice:        slPrice,
		PositionCloseTimeout: time.Duration(cand.Config.FundingReversion.PostSettleTimeout),
		FireTime:             targetTime,
		MaxLatency:           time.Duration(cand.Config.FundingReversion.MaxLatency),
		SettleTime:           settleTimePtr,
		SkipPreFlight:        true,
		Extra: map[string]any{
			"buffer_time_ms":    bufferTime.Milliseconds(),
			"fire_offset_ms":    evt.FireOffsetMs,
			"one_way_ms":        oneWayMs,
			"funding_rate":      cand.FundingRate,
			"vol_24h_usdt":      cand.Vol24USDT,
			"tp_pct":            cand.Config.FundingReversion.TakeProfitPct,
			"sl_pct":            cand.Config.FundingReversion.StopLossPct,
			"ioc_price":         iocPrice,
			"take_profit_price": tpPrice,
			"stop_loss_price":   slPrice,
		},
	}

	return r.publishEvent(ctx, ordermanager.TopicOrderIntent, orderIntent)
}

func (r *StatelessRunner) handleMarginModeReady(ctx context.Context, evt MarginModeReadyEvent) error {
	r.log.Info("handleMarginModeReady", slog.String("symbol", evt.Symbol))

	marginMode := shared.MarginModeIsolated
	if evt.Candidate.Config.ParsedOpenType == 2 {
		marginMode = shared.MarginModeCross
	}

	err := r.deps.Client.SwitchMarginMode(ctx, evt.Symbol, marginMode, evt.Candidate.Config.Leverage, evt.Candidate.Side)
	if err != nil {
		r.log.ErrorContext(ctx, "Failed to switch margin mode", slog.Any("error", err), slog.String("symbol", evt.Symbol))
		r.abortAfter(ctx, evt.BaseReversionEvent, evt.Symbol, "switch margin mode failed: "+err.Error())
		return fmt.Errorf("switch margin mode failed: %w", err)
	}

	if switcher, ok := r.deps.Client.(exchange.PositionModeSwitcher); ok {
		targetMode := shared.PositionMode(evt.Candidate.Config.ParsedPositionMode)
		if err := switcher.SwitchPositionMode(ctx, evt.Symbol, targetMode); err != nil {
			r.log.ErrorContext(ctx, "Failed to switch position mode", slog.Any("error", err), slog.String("symbol", evt.Symbol))
			r.abortAfter(ctx, evt.BaseReversionEvent, evt.Symbol, "switch position mode failed: "+err.Error())
			return fmt.Errorf("switch position mode failed: %w", err)
		}
	}

	// Preemptively set the configured leverage on the exchange before the fire window to eliminate any order placement latency.
	leverage := evt.Candidate.Config.Leverage

	if leverage > 0 && !r.deps.Client.SupportLeverageOnOrder() {
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
			r.abortAfter(ctx, evt.BaseReversionEvent, evt.Symbol, "change leverage failed: "+err.Error())
			return fmt.Errorf("change leverage failed: %w", err)
		}
	}

	latencyMs := r.latencyMs()
	oneWayMs := latencyMs / 2
	bufferTime := time.Duration(evt.Candidate.Config.FundingReversion.BufferTime)
	fireOffset := time.Duration(oneWayMs)*time.Millisecond + bufferTime

	// Ensure snapshotOffset is at least fireOffset + 300ms, and at least 300ms overall
	// to avoid race conditions during the price refresh and safety calculation.
	snapshotOffset := max(fireOffset+300*time.Millisecond, 300*time.Millisecond)

	nextEvt := FireTimingReadyEvent{
		BaseReversionEvent: nextReversionBase(evt.BaseReversionEvent, evt.Symbol, r.now()),
		Candidate:          evt.Candidate,
		LatencyRTTMs:       latencyMs,
		FireOffsetMs:       fireOffset.Milliseconds(),
		SnapshotOffsetMs:   snapshotOffset.Milliseconds(),
	}

	return r.publishEvent(ctx, TopicReversionFireTimingReady, nextEvt)
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

	safety := r.globalCfg.Reversion.Safety
	c.SafetyResult = c.ApplySafetySizing(fundingdomain.SafetyLimits{
		MaxImpactRatio: safety.MaxImpactRatio,
		MinVol24USD:    c.Config.MinVol24USD,
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
		IOCPrice:           ioc,
		RefPrice:           refPrice,
		AdjustedVolume:     c.Volume,
		Passed:             passed,
		RejectReason:       rejectReason,
	}

	return r.publishEvent(ctx, TopicReversionFirePlanChecked, next)
}

func (r *StatelessRunner) handleFirePlanChecked(ctx context.Context, evt FirePlanCheckedEvent) error {
	c := evt.Candidate
	// Clean up WebSocket ticker subscription as Reversion phase completes and OrderManager takes over
	r.unsubscribeWS(ctx, c.Symbol)

	if !evt.Passed {
		r.abortAfter(ctx, evt.BaseReversionEvent, c.Symbol, evt.RejectReason)
		return errors.New(evt.RejectReason)
	}

	fireOffset := time.Duration(evt.FireOffsetMs) * time.Millisecond
	targetTime := evt.SettleTime.Add(-fireOffset)

	tpPrice, slPrice := c.CalculateOrderTPSL(ctx, evt.IOCPrice, r.log)
	return r.dispatchOrderManagerIntent(ctx, evt, targetTime, evt.IOCPrice, tpPrice, slPrice)
}
