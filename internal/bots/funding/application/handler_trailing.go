package application

import (
	"context"
	"log/slog"
	"time"

	shared "crypto-bot/internal/domain"

	"crypto-bot/internal/bots/funding/application/events"
	"crypto-bot/internal/bots/funding/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"

	"github.com/ThreeDotsLabs/watermill/message"
)

// subscribeTrailing handles flow-scoped order_filled events → places TrackOrder on MEXC.
func (o *CycleOrchestrator) subscribeTrailing(ctx context.Context) {
	o.consumeTopic(ctx, events.TopicReversionOrderFilled, func(msg *message.Message) {
		evt, err := unmarshal[events.OrderFilledEvent](msg.Payload)
		if err != nil {
			o.deps.Log.Error("🔴 Unmarshal OrderFilledEvent failed", slog.Any("error", err))
			return
		}
		o.handleTrailing(ctx, evt)
	})
	o.consumeTopic(ctx, events.TopicTrapOrderFilled, func(msg *message.Message) {
		evt, err := unmarshal[events.OrderFilledEvent](msg.Payload)
		if err != nil {
			o.deps.Log.Error("🔴 Unmarshal OrderFilledEvent failed", slog.Any("error", err))
			return
		}
		o.handleTrailing(ctx, evt)
	})
}

func (o *CycleOrchestrator) handleTrailing(ctx context.Context, evt events.OrderFilledEvent) {
	var c domain.Candidate
	o.withLock(func() {
		c = o.candidate
	})

	var trailCfg domain.TrailingConfig
	if evt.Flow == events.FlowTrap {
		trailCfg = c.Config.FundingTrap.Trailing
	} else {
		trailCfg = c.Config.FundingReversion.Trailing
	}

	if !trailCfg.Enabled {
		o.deps.Log.Info("⏭️ Trailing disabled, position requires manual close", slog.String("flow", evt.Flow))
		return
	}

	closeSide := evt.CloseSide
	var activePrice float64
	if trailCfg.ActivationPct > 0 {
		if closeSide == shared.SideCloseLong {
			activePrice = decmath.Mul(evt.DealAvgPrice, decmath.Add(1, trailCfg.ActivationPct))
		} else {
			activePrice = decmath.Mul(evt.DealAvgPrice, decmath.Sub(1, trailCfg.ActivationPct))
		}
	}

	req := exchange.SubmitTrackOrderRequest{
		Symbol:       evt.Symbol,
		Leverage:     c.Config.Leverage,
		Side:         int(closeSide),
		Vol:          evt.DealVol,
		OpenType:     c.Config.ParsedOpenType,
		PositionMode: c.Config.ParsedPositionMode,
		Trend:        1,
		ActivePrice:  activePrice,
		BackType:     1,
		BackValue:    trailCfg.CallbackPct,
		ReduceOnly:   true,
	}

	o.deps.Log.Info("🏃 Placing TrackOrder (Trailing)",
		slog.String("flow", evt.Flow),
		slog.Int("side", req.Side),
		slog.Float64("vol", req.Vol),
		slog.Float64("activePrice", activePrice),
		slog.Float64("callbackPct", decmath.Mul(req.BackValue, 100)),
	)

	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	trackID, err := o.deps.Client.CreateTrackOrder(reqCtx, req)
	if err != nil {
		o.deps.Log.Error("🔴 TrackOrder failed - fallback close", slog.Any("error", err), slog.String("flow", evt.Flow))
		o.fallbackCloseAfterTrailingFailure(ctx, evt)
		return
	}

	o.deps.Log.Info("✅ TrackOrder placed successfully", slog.String("trackID", trackID), slog.String("flow", evt.Flow))

	// Capture trailing data for cycle record thread-safely.
	o.recorder.Mutate(func(b *domain.CycleRecordBuilder) {
		b.TrailingActivated = true
		b.TrailingActivePrice = activePrice
		b.TrailingCallbackPct = trailCfg.CallbackPct
	})

	topic := events.TopicReversionTrailingPlaced
	flow := events.FlowReversion
	if evt.Flow == events.FlowTrap {
		topic = events.TopicTrapTrailingPlaced
		flow = events.FlowTrap
	}
	o.publishOrLog(topic, events.TrailingPlacedEvent{
		Flow:        flow,
		Symbol:      evt.Symbol,
		TrackID:     trackID,
		ActivePrice: activePrice,
		CallbackPct: trailCfg.CallbackPct,
	})
}

func (o *CycleOrchestrator) fallbackCloseAfterTrailingFailure(ctx context.Context, evt events.OrderFilledEvent) {
	flow := events.FlowReversion
	closeTopic := events.TopicReversionPositionClosed
	errorTopic := events.TopicReversionError
	abortTopic := events.TopicReversionAbort
	if evt.Flow == events.FlowTrap {
		flow = events.FlowTrap
		closeTopic = events.TopicTrapPositionClosed
		errorTopic = events.TopicTrapError
		abortTopic = events.TopicTrapAbort
	}
	var positionMode int
	o.withLock(func() {
		positionMode = o.candidate.Config.ParsedPositionMode
	})

	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	if err := o.deps.Client.ClosePosition(closeCtx, evt.Symbol, evt.CloseSide, evt.DealVol, positionMode); err != nil {
		o.deps.Log.Error("🔴 Exact-leg close failed - fallback close all",
			slog.Any("error", err),
			slog.String("symbol", evt.Symbol),
			slog.String("flow", flow),
			slog.Any("closeSide", evt.CloseSide),
			slog.Float64("vol", evt.DealVol),
		)
		if allErr := o.deps.Client.CloseAllPositions(closeCtx, evt.Symbol); allErr != nil {
			reason := "critical_close_failed: " + allErr.Error()
			o.recorder.Mutate(func(b *domain.CycleRecordBuilder) {
				b.AbortReason = reason
				b.AbortFlow = flow
				b.AbortTopic = abortTopic
				b.ErrorFlow = flow
				b.ErrorTopic = errorTopic
			})
			o.deps.Log.Error("🔴 CRITICAL close failed after exact-leg close failure",
				slog.Any("error", allErr),
				slog.String("symbol", evt.Symbol),
				slog.String("flow", flow),
			)
			o.publishOrLog(errorTopic, events.CycleErrorEvent{
				Flow:   flow,
				Symbol: evt.Symbol,
				Error:  reason,
			})
			o.publishOrLog(abortTopic, events.CycleAbortEvent{
				Flow:   flow,
				Symbol: evt.Symbol,
				Reason: reason,
			})
			return
		}
	}

	o.publishOrLog(closeTopic, events.PositionClosedEvent{
		Flow:   flow,
		Symbol: evt.Symbol,
		Reason: "trailing_failed_fallback",
	})
}
