package application

import (
	"context"
	"time"

	shared "crypto-bot/internal/domain"

	"crypto-bot/internal/bots/funding_reversion/application/events"
	"crypto-bot/internal/bots/funding_reversion/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"

	"github.com/ThreeDotsLabs/watermill/message"
)

// subscribeTrailing handles cycle.order.filled → places TrackOrder (trailing stop) on MEXC.
func (o *CycleOrchestrator) subscribeTrailing(ctx context.Context) {
	o.consumeTopic(ctx, events.TopicOrderFilled, func(msg *message.Message) {
		evt, err := unmarshal[events.OrderFilledEvent](msg.Payload)
		if err != nil {
			o.deps.Log.Error("🔴 Unmarshal OrderFilledEvent failed", "error", err)
			return
		}
		o.handleTrailing(ctx, evt)
	})
}

func (o *CycleOrchestrator) handleTrailing(ctx context.Context, evt events.OrderFilledEvent) {
	c := o.candidate

	var trailCfg domain.TrailingConfig
	if evt.Phase == "trap" {
		trailCfg = c.Config.FundingTrap.Trailing
	} else {
		trailCfg = c.Config.FundingReversion.Trailing
	}

	if !trailCfg.Enabled {
		o.deps.Log.Info("⏭️ Trailing disabled, position requires manual close", "phase", evt.Phase)
		return
	}

	closeSide := shared.Side(evt.CloseSide)
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
		"phase", evt.Phase,
		"side", req.Side,
		"vol", req.Vol,
		"activePrice", activePrice,
		"callbackPct", decmath.Mul(req.BackValue, 100),
	)

	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	trackID, err := o.deps.Client.CreateTrackOrder(reqCtx, req)
	if err != nil {
		o.deps.Log.Error("🔴 TrackOrder failed - fallback close", "error", err, "phase", evt.Phase)
		_ = o.deps.Client.CloseAllPositions(reqCtx, evt.Symbol)

		o.publishOrLog(events.TopicPositionClosed, events.PositionClosedEvent{
			Symbol: evt.Symbol,
			Reason: "trailing_failed_fallback",
		})
		return
	}

	o.deps.Log.Info("✅ TrackOrder placed successfully", "trackID", trackID, "phase", evt.Phase)
	o.publishOrLog(events.TopicTrailingPlaced, events.TrailingPlacedEvent{
		Symbol:      evt.Symbol,
		TrackID:     trackID,
		ActivePrice: activePrice,
		CallbackPct: trailCfg.CallbackPct,
		Phase:       evt.Phase,
	})
}
