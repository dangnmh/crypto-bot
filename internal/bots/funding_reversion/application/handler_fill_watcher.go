package application

import (
	"context"
	"time"

	"crypto-bot/internal/bots/funding_reversion/application/events"
	"crypto-bot/internal/infrastructure/exchange"

	"github.com/ThreeDotsLabs/watermill/message"
)

// subscribeFillWatcher handles cycle.ioc.fired → sets up WS callback for order fills.
func (o *CycleOrchestrator) subscribeFillWatcher(ctx context.Context) {
	o.watchIOCFills(ctx)
	o.watchTrapFills(ctx)
}

func (o *CycleOrchestrator) watchIOCFills(ctx context.Context) {
	o.consumeTopic(ctx, events.TopicIOCFired, func(msg *message.Message) {
		evt, err := unmarshal[events.IOCFiredEvent](msg.Payload)
		if err != nil {
			o.deps.Log.Error("🔴 Unmarshal IOCFiredEvent failed", "error", err)
			return
		}
		o.setupFillWatcher(evt.OrderID, evt.Side, evt.CloseSide, "ioc")
	})
}

func (o *CycleOrchestrator) watchTrapFills(ctx context.Context) {
	o.consumeTopic(ctx, events.TopicTrapFired, func(msg *message.Message) {
		evt, err := unmarshal[events.TrapFiredEvent](msg.Payload)
		if err != nil {
			o.deps.Log.Error("🔴 Unmarshal TrapFiredEvent failed", "error", err)
			return
		}
		o.setupFillWatcher(evt.OrderID, evt.Side, evt.CloseSide, "trap")
	})
}

func (o *CycleOrchestrator) setupFillWatcher(orderID string, side, closeSide int, phase string) {
	if orderID == "" {
		return
	}

	o.deps.OrderNotifier.OnOrderUpdate(orderID, 5*time.Second, func(deal exchange.WsOrderDeal) {
		if !exchange.IsTerminalOrderState(deal.State) {
			return
		}

		o.deps.OrderNotifier.RemoveOrderCallback(deal.GetOrderID())

		if deal.DealVol <= 0 {
			o.deps.Log.Warn("🟡 No fill", "phase", phase, "orderID", deal.GetOrderID())
			return
		}

		o.deps.Log.Info("📊 Position opened",
			"phase", phase,
			"entry", deal.DealAvgPrice,
			"vol", deal.DealVol,
		)

		o.publishOrLog(events.TopicOrderFilled, events.OrderFilledEvent{
			Symbol:       o.cfg.Symbol,
			OrderID:      deal.GetOrderID(),
			Phase:        phase,
			DealAvgPrice: deal.DealAvgPrice,
			DealVol:      deal.DealVol,
			Side:         side,
			CloseSide:    closeSide,
		})
	})
}
