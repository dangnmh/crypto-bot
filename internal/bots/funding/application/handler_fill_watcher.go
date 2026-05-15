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

// subscribeFillWatcher handles flow-scoped order placement events → sets up WS callbacks for fills.
func (o *CycleOrchestrator) subscribeFillWatcher(ctx context.Context) {
	o.watchIOCFills(ctx)
	o.watchTrapFills(ctx)
}

func (o *CycleOrchestrator) watchIOCFills(ctx context.Context) {
	o.consumeTopic(ctx, events.TopicReversionIOCFired, func(msg *message.Message) {
		evt, err := unmarshal[events.IOCFiredEvent](msg.Payload)
		if err != nil {
			o.deps.Log.Error("🔴 Unmarshal IOCFiredEvent failed", slog.Any("error", err))
			return
		}
		o.setupFillWatcher(ctx, evt.OrderID, evt.Side, evt.CloseSide, domain.PhaseIOC)
	})
}

func (o *CycleOrchestrator) watchTrapFills(ctx context.Context) {
	o.consumeTopic(ctx, events.TopicTrapOrderPlaced, func(msg *message.Message) {
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
				b.IOCExcursion = domain.NewExcursionTracker(side, deal.DealAvgPrice)
				b.Excursion = b.IOCExcursion
			} else {
				b.TrapFilled = true
				b.TrapFillPrice = deal.DealAvgPrice
				b.TrapFillVol = deal.DealVol
				b.TrapExcursion = domain.NewExcursionTracker(side, deal.DealAvgPrice)
			}
		})
		o.startExcursionPriceStream(ctx)

		topic := events.TopicReversionOrderFilled
		flow := events.FlowReversion
		if phase == domain.PhaseTrap {
			topic = events.TopicTrapOrderFilled
			flow = events.FlowTrap
		}

		o.publishOrLog(topic, events.OrderFilledEvent{
			Flow:         flow,
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

func (o *CycleOrchestrator) startExcursionPriceStream(ctx context.Context) {
	if o.deps.PriceStore == nil {
		return
	}

	if o.excursionCancel != nil {
		return
	}

	streamCtx, cancel := context.WithCancel(ctx)
	o.excursionCancel = cancel
	updates := o.deps.PriceStore.SubscribePrice(streamCtx, o.cfg.Symbol, 32)

	go func() {
		for pd := range updates {
			if pd == nil || pd.LastPrice <= 0 {
				continue
			}
			o.recorder.Mutate(func(b *domain.CycleRecordBuilder) {
				if b.IOCExcursion != nil {
					b.IOCExcursion.Update(pd.LastPrice, pd.UpdatedAt)
					b.Excursion = b.IOCExcursion
				}
				if b.TrapExcursion != nil {
					b.TrapExcursion.Update(pd.LastPrice, pd.UpdatedAt)
				}
			})
		}
	}()
}
