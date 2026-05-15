package application

import (
	"context"
	"log/slog"
	"time"

	"crypto-bot/internal/bots/funding/application/events"
	"crypto-bot/internal/bots/funding/domain"
	shared "crypto-bot/internal/domain"
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
			o.deps.Log.Error("🔴 Unmarshal IOCFiredEvent failed", slog.Any("error", err))
			return
		}
		o.setupFillWatcher(ctx, evt.OrderID, evt.Side, evt.CloseSide, domain.PhaseIOC)
	})
}

func (o *CycleOrchestrator) watchTrapFills(ctx context.Context) {
	o.consumeTopic(ctx, events.TopicTrapFired, func(msg *message.Message) {
		evt, err := unmarshal[events.TrapFiredEvent](msg.Payload)
		if err != nil {
			o.deps.Log.Error("🔴 Unmarshal TrapFiredEvent failed", slog.Any("error", err))
			return
		}
		o.setupFillWatcher(ctx, evt.OrderID, evt.Side, evt.CloseSide, domain.PhaseTrap)
	})
}

func (o *CycleOrchestrator) setupFillWatcher(ctx context.Context, orderID string, side, closeSide shared.Side, phase domain.Phase) {
	if orderID == "" {
		return
	}

	o.deps.OrderNotifier.OnOrderUpdate(ctx, orderID, 5*time.Second, func(deal exchange.WsOrderDeal) {
		if !exchange.IsTerminalOrderState(deal.State) {
			return
		}

		o.deps.OrderNotifier.RemoveOrderCallback(deal.GetOrderID())

		if deal.DealVol <= 0 {
			o.deps.Log.Warn("🟡 No fill", slog.Any("phase", phase), slog.String("orderID", deal.GetOrderID()))
			return
		}

		o.deps.Log.Info("📊 Position opened",
			slog.Any("phase", phase),
			slog.Float64("entry", deal.DealAvgPrice),
			slog.Float64("vol", deal.DealVol),
		)

		// Capture fill data for cycle record.
		o.recorder.Mutate(func(b *domain.CycleRecordBuilder) {
			if phase == domain.PhaseIOC {
				b.IOCFilled = true
				b.IOCFillPrice = deal.DealAvgPrice
				b.IOCFillVol = deal.DealVol
				// Start MFE/MAE tracking from fill price.
				b.Excursion = domain.NewExcursionTracker(b.Side, deal.DealAvgPrice)
			} else {
				b.TrapFilled = true
				b.TrapFillPrice = deal.DealAvgPrice
			}
		})

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
