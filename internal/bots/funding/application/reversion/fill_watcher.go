package reversion

import (
	"context"
	"log/slog"
	"time"

	"crypto-bot/internal/bots/funding/application/cycle"
	"crypto-bot/internal/bots/funding/application/events"
	"crypto-bot/internal/infrastructure/exchange"
	applogger "crypto-bot/pkg/logger"

	"github.com/ThreeDotsLabs/watermill/message"
)

func subscribeFillWatcher(ctx context.Context, rt *cycle.Runtime) {
	rt.Subscribe(ctx, events.TopicReversionIOCFired, func(msg *message.Message) {
		evt, err := cycle.Unmarshal[events.IOCFiredEvent](msg.Payload)
		if err != nil {
			applogger.WithCtx(ctx, rt.Log()).Error("Unmarshal IOCFiredEvent failed", slog.Any("error", err))
			return
		}
		if evt.OrderID == "" || evt.Error != "" {
			return
		}
		setupFillWatcher(ctx, rt, evt)
	})
}

func setupFillWatcher(ctx context.Context, rt *cycle.Runtime, evt events.IOCFiredEvent) {
	rt.Deps().OrderNotifier.OnOrderUpdate(ctx, evt.OrderID, 5*time.Second, func(deal exchange.WsOrderDeal) {
		if !exchange.IsTerminalOrderState(deal.State) {
			return
		}

		orderID := deal.GetOrderID()
		rt.Deps().OrderNotifier.RemoveOrderCallback(orderID)

		if deal.DealVol <= 0 {
			applogger.WithCtx(ctx, rt.Log()).Warn("IOC terminal with no fill", slog.String("orderID", orderID))
			if rt.TryMarkFlowTerminal(events.FlowReversion) {
				reqID := rt.GetReqID()
				rt.RecordAndPublishCtx(ctx, reqID, events.TopicReversionTimeout, events.CycleTimeoutEvent{
					Flow:    events.FlowReversion,
					Symbol:  evt.Symbol,
					Timeout: 0,
					Reason:  reversionReasonNoFill,
				})
			}
			return
		}

		fillEvt := events.OrderFilledEvent{
			Flow:      events.FlowReversion,
			Symbol:    evt.Symbol,
			OrderID:   orderID,
			Side:      evt.Side,
			CloseSide: evt.CloseSide,
			FillPrice: deal.DealAvgPrice,
			FillVol:   deal.DealVol,
			Fee:       deal.TakerFee + deal.MakerFee,
			Profit:    deal.Profit,
			TPPrice:   evt.TPPrice,
			SLPrice:   evt.SLPrice,
		}

		rt.MarkReversionFill(fillEvt)
		reqID := rt.GetReqID()
		rt.RecordAndPublishCtx(ctx, reqID, events.TopicReversionOrderFilled, fillEvt)
		rt.StartExcursionPriceStream(ctx, reqID)
		watchStaticCloseDeal(ctx, rt, fillEvt)
	})
}
