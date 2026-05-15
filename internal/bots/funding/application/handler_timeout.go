package application

import (
	"context"
	"log/slog"
	"time"

	"crypto-bot/internal/bots/funding/application/events"

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
		o.deps.Log.Error("🔴 Force close failed", slog.Any("error", err))
	}

	o.publishOrLog(events.TopicReversionTimeout, events.CycleTimeoutEvent{
		Flow:    events.FlowReversion,
		Symbol:  symbol,
		Timeout: timeout,
	})
}
