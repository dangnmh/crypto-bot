package trap

import (
	"context"
	"log/slog"
	"time"

	shared "crypto-bot/internal/domain"
	"crypto-bot/pkg/decmath"

	"crypto-bot/internal/bots/funding/application/cycle"
	"crypto-bot/internal/bots/funding/application/events"
	"crypto-bot/internal/infrastructure/exchange"

	"github.com/ThreeDotsLabs/watermill/message"
)

func subscribeTrailing(ctx context.Context, rt *cycle.Runtime) {
	rt.Subscribe(ctx, events.TopicTrapOrderFilled, func(msg *message.Message) {
		evt, err := cycle.Unmarshal[events.OrderFilledEvent](msg.Payload)
		if err != nil {
			rt.Log().Error("Unmarshal OrderFilledEvent failed", slog.Any("error", err))
			return
		}
		handleTrailing(ctx, rt, evt)
	})
}

func handleTrailing(ctx context.Context, rt *cycle.Runtime, evt events.OrderFilledEvent) {
	c := rt.CandidateCopy()
	trailCfg := c.Config.FundingTrap.Trailing
	if !trailCfg.Enabled {
		rt.Log().Info("Trailing disabled, position requires manual close", slog.String("flow", evt.Flow))
		return
	}

	closeSide := evt.CloseSide
	var activePrice float64
	if trailCfg.ActivationPct > 0 {
		if closeSide == shared.SideCloseLong {
			activePrice = decmath.Mul(evt.FillPrice, decmath.Add(1, trailCfg.ActivationPct))
		} else {
			activePrice = decmath.Mul(evt.FillPrice, decmath.Sub(1, trailCfg.ActivationPct))
		}
	}

	req := exchange.SubmitTrackOrderRequest{
		Symbol:       evt.Symbol,
		Leverage:     c.Config.Leverage,
		Side:         int(closeSide),
		Vol:          evt.FillVol,
		OpenType:     c.Config.ParsedOpenType,
		PositionMode: c.Config.ParsedPositionMode,
		Trend:        1,
		ActivePrice:  activePrice,
		BackType:     1,
		BackValue:    trailCfg.CallbackPct,
		ReduceOnly:   true,
	}

	rt.Log().Info("Placing TrackOrder (Trailing)",
		slog.String("flow", evt.Flow),
		slog.Int("side", req.Side),
		slog.Float64("vol", req.Vol),
		slog.Float64("activePrice", activePrice),
		slog.Float64("callbackPct", decmath.Mul(req.BackValue, 100)),
	)

	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	trackID, err := rt.Deps().Client.CreateTrackOrder(reqCtx, req)
	if err != nil {
		rt.Log().Error("TrackOrder failed - fallback close", slog.Any("error", err), slog.String("flow", evt.Flow))
		fallbackCloseAfterTrailingFailure(ctx, rt, evt)
		return
	}

	rt.Log().Info("TrackOrder placed successfully", slog.String("trackID", trackID), slog.String("flow", evt.Flow))
	reqID := rt.GetReqID()
	rt.RecordAndPublish(reqID, events.TopicTrapTrailingPlaced, events.TrailingPlacedEvent{
		Flow:        events.FlowTrap,
		Symbol:      evt.Symbol,
		TrackID:     trackID,
		ActivePrice: activePrice,
		CallbackPct: trailCfg.CallbackPct,
	})

	watchTrapCloseDeal(ctx, rt, evt)
}

func watchTrapCloseDeal(ctx context.Context, rt *cycle.Runtime, evt events.OrderFilledEvent) {
	timeout := time.Duration(rt.Config().FundingTrap.PostSettleTimeout)
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	rt.Deps().OrderNotifier.OnOrderDealBySymbolSide(ctx, evt.Symbol, int(evt.CloseSide), timeout, func(deal exchange.PersonalOrderDeal) {
		if deal.Vol <= 0 || rt.IsFlowTerminal(events.FlowTrap) {
			return
		}
		if !rt.TryMarkFlowTerminal(events.FlowTrap) {
			return
		}
		rt.MarkTrapTerminal()
		reqID := rt.GetReqID()
		rt.RecordAndPublish(reqID, events.TopicTrapPositionClosed, events.PositionClosedEvent{
			Flow:       events.FlowTrap,
			Symbol:     evt.Symbol,
			ClosePrice: deal.Price,
			CloseVol:   deal.Vol,
			Reason:     "trailing",
			Profit:     deal.Profit,
			Fee:        deal.Fee,
			Method:     "track_order",
		})
	})
}

func fallbackCloseAfterTrailingFailure(ctx context.Context, rt *cycle.Runtime, evt events.OrderFilledEvent) {
	positionMode := rt.CandidateCopy().Config.ParsedPositionMode
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	exactRetries, err := rt.RetryWithBackoff(closeCtx, trapRetryCount, func() error {
		return rt.Deps().Client.ClosePosition(closeCtx, evt.Symbol, evt.CloseSide, evt.FillVol, positionMode)
	})
	if err != nil {
		rt.Log().Error("Exact-leg close failed - fallback close all",
			slog.Any("error", err),
			slog.String("symbol", evt.Symbol),
			slog.String("flow", events.FlowTrap),
			slog.Any("closeSide", evt.CloseSide),
			slog.Float64("vol", evt.FillVol),
		)
		allRetries, allErr := rt.RetryWithBackoff(closeCtx, trapRetryCount, func() error {
			return rt.Deps().Client.CloseAllPositions(closeCtx, evt.Symbol)
		})
		exactRetries += allRetries
		if allErr != nil {
			reason := "critical_close_failed: " + allErr.Error()
			reqID := rt.GetReqID()
			rt.RecordAndPublish(reqID, events.TopicTrapError, events.CycleErrorEvent{
				Flow:   events.FlowTrap,
				Symbol: evt.Symbol,
				Error:  reason,
			})
			rt.RecordAndPublish(reqID, events.TopicTrapAbort, events.CycleAbortEvent{
				Flow:   events.FlowTrap,
				Symbol: evt.Symbol,
				Reason: reason,
			})
			rt.MarkTrapTerminal()
			rt.Log().Error("CRITICAL close failed after exact-leg close failure",
				slog.Any("error", allErr),
				slog.String("symbol", evt.Symbol),
				slog.String("flow", events.FlowTrap),
			)
			return
		}
	}

	if !rt.TryMarkFlowTerminal(events.FlowTrap) {
		return
	}
	reqID := rt.GetReqID()
	rt.MarkTrapTerminal()
	rt.RecordAndPublish(reqID, events.TopicTrapPositionClosed, events.PositionClosedEvent{
		Flow:     events.FlowTrap,
		Symbol:   evt.Symbol,
		Reason:   "trailing_failed_fallback",
		CloseVol: evt.FillVol,
		Method:   trapMethodFallbackClose,
	})
	rt.Log().Info("Trap fallback close completed", slog.Int("retries", exactRetries))
}
