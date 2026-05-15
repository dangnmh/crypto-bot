package application

import (
	"context"
	"log/slog"
	"time"

	"crypto-bot/internal/bots/funding/application/events"
	"crypto-bot/internal/bots/funding/domain"

	"github.com/ThreeDotsLabs/watermill/message"
)

// subscribeTimeoutGuard handles funding.reversion.ioc_fired → starts safety timer.
// If the timer expires before a PositionClosed event, it force-closes all positions.
func (o *CycleOrchestrator) subscribeTimeoutGuard(ctx context.Context) {
	o.consumeTopic(ctx, events.TopicReversionIOCFired, func(msg *message.Message) {
		evt, err := unmarshal[events.IOCFiredEvent](msg.Payload)
		if err != nil {
			o.deps.Log.Error("🔴 Unmarshal IOCFiredEvent failed", slog.Any("error", err))
			return
		}
		go o.handleTimeout(ctx, evt.Symbol)
	})
}

func (o *CycleOrchestrator) subscribeTrapOrderTimeoutGuard(ctx context.Context) {
	o.consumeTopic(ctx, events.TopicTrapOrderPlaced, func(msg *message.Message) {
		evt, err := unmarshal[events.TrapFiredEvent](msg.Payload)
		if err != nil {
			o.deps.Log.Error("🔴 Unmarshal TrapFiredEvent failed", slog.Any("error", err))
			return
		}
		go o.handleTrapOrderTimeout(ctx, evt)
	})
}

func (o *CycleOrchestrator) handleTimeout(ctx context.Context, symbol string) {
	timeout := time.Duration(o.cfg.FundingReversion.PostSettleTimeout)
	if timeout <= 0 {
		timeout = 60 * time.Second // Fallback default
	}

	o.deps.Log.Info("⏱️ Timeout guard started", slog.Duration("timeout", timeout))

	if err := o.deps.Clock.Sleep(ctx, timeout); err != nil {
		// Context cancelled (position already closed or cycle done)
		return
	}
	o.deps.Log.Warn("🔴 TIMEOUT — force closing all positions", slog.Duration("timeout", timeout))

	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	if err := o.deps.Client.CloseAllPositions(closeCtx, symbol); err != nil {
		reason := "critical_timeout_close_failed: " + err.Error()
		o.recorder.Mutate(func(b *domain.CycleRecordBuilder) {
			b.AbortReason = reason
			b.AbortPhase = domain.PhaseFire
		})
		o.deps.Log.Error("🔴 CRITICAL force close failed after timeout", slog.Any("error", err))
		o.publishOrLog(events.TopicReversionError, events.CycleErrorEvent{
			Flow:   events.FlowReversion,
			Symbol: symbol,
			Error:  reason,
			Phase:  domain.PhaseFire,
		})
		o.publishOrLog(events.TopicReversionAbort, events.CycleAbortEvent{
			Flow:   events.FlowReversion,
			Symbol: symbol,
			Reason: reason,
			Phase:  domain.PhaseFire,
		})
		return
	}

	o.publishOrLog(events.TopicReversionTimeout, events.CycleTimeoutEvent{
		Flow:    events.FlowReversion,
		Symbol:  symbol,
		Timeout: timeout,
	})
}

func (o *CycleOrchestrator) handleTrapOrderTimeout(ctx context.Context, evt events.TrapFiredEvent) {
	timeout := time.Duration(o.cfg.FundingTrap.PostSettleTimeout)
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	o.deps.Log.Info("⏱️ Trap order timeout guard started",
		slog.String("orderID", evt.OrderID),
		slog.Duration("timeout", timeout),
	)

	if err := o.deps.Clock.Sleep(ctx, timeout); err != nil {
		return
	}

	trapFilled := false
	o.recorder.Mutate(func(b *domain.CycleRecordBuilder) {
		trapFilled = b.TrapFilled && b.TrapOrderID == evt.OrderID
	})
	if trapFilled {
		return
	}

	cancelCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	if err := o.deps.Client.CancelOrder(cancelCtx, evt.Symbol, evt.OrderID); err != nil {
		o.deps.Log.Error("🔴 Trap order cancel failed - canceling all open orders", slog.Any("error", err))
		if allErr := o.deps.Client.CancelAllOpenOrders(cancelCtx, evt.Symbol); allErr != nil {
			reason := "critical_trap_cancel_failed: " + allErr.Error()
			o.recorder.Mutate(func(b *domain.CycleRecordBuilder) {
				b.AbortReason = reason
				b.AbortPhase = domain.PhaseTrap
			})
			o.publishOrLog(events.TopicTrapError, events.CycleErrorEvent{
				Flow:   events.FlowTrap,
				Symbol: evt.Symbol,
				Error:  reason,
				Phase:  domain.PhaseTrap,
			})
			o.publishOrLog(events.TopicReversionAbort, events.CycleAbortEvent{
				Flow:   events.FlowTrap,
				Symbol: evt.Symbol,
				Reason: reason,
				Phase:  domain.PhaseTrap,
			})
			return
		}
	}

	o.publishOrLog(events.TopicTrapTimeout, events.CycleTimeoutEvent{
		Flow:    events.FlowTrap,
		Symbol:  evt.Symbol,
		Timeout: timeout,
	})
}
