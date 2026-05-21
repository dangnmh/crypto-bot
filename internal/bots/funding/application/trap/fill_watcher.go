package trap

import (
	"context"
	"log/slog"
	"time"

	shared "crypto-bot/internal/domain"

	"crypto-bot/internal/bots/funding/application/cycle"
	"crypto-bot/internal/bots/funding/application/events"
	"crypto-bot/internal/infrastructure/exchange"

	"github.com/ThreeDotsLabs/watermill/message"
)

const (
	trapRetryCount          = 3
	trapMethodFallbackClose = "fallback_close"
)

func subscribeFillWatcher(ctx context.Context, rt *cycle.Runtime) {
	rt.Subscribe(ctx, events.TopicTrapOrderPlaced, func(msg *message.Message) {
		evt, err := cycle.Unmarshal[events.TrapFiredEvent](msg.Payload)
		if err != nil {
			rt.Log().Error("Unmarshal TrapFiredEvent failed", slog.Any("error", err))
			return
		}
		setupFillWatcher(ctx, rt, evt.OrderID, evt.Side, evt.CloseSide)
	})
}

func setupFillWatcher(ctx context.Context, rt *cycle.Runtime, orderID string, side, closeSide shared.Side) {
	if orderID == "" {
		return
	}

	rt.Deps().OrderNotifier.OnOrderUpdate(ctx, orderID, 5*time.Second, func(deal exchange.WsOrderDeal) {
		if !exchange.IsTerminalOrderState(deal.State) {
			return
		}

		rt.Deps().OrderNotifier.RemoveOrderCallback(deal.GetOrderID())

		if deal.DealVol <= 0 {
			rt.Log().Warn("No fill", slog.String("flow", events.FlowTrap), slog.String("orderID", deal.GetOrderID()))
			return
		}

		rt.Log().Info("Position opened",
			slog.String("flow", events.FlowTrap),
			slog.Float64("entry", deal.DealAvgPrice),
			slog.Float64("vol", deal.DealVol),
		)

		reqID := rt.GetReqID()
		fillEvt := events.OrderFilledEvent{
			Flow:      events.FlowTrap,
			Symbol:    rt.Config().Symbol,
			OrderID:   deal.GetOrderID(),
			FillPrice: deal.DealAvgPrice,
			FillVol:   deal.DealVol,
			Side:      side,
			CloseSide: closeSide,
		}
		rt.MarkTrapFill(fillEvt)
		rt.RecordAndPublish(reqID, events.TopicTrapOrderFilled, fillEvt)
		rt.StartExcursionPriceStream(ctx, reqID)
	})
}
