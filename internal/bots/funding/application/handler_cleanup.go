package application

import (
	"context"
	"log/slog"
	"time"

	"crypto-bot/internal/bots/funding/application/events"

	"github.com/ThreeDotsLabs/watermill/message"
)

// subscribeCleanup handles terminal events → unsubscribe WS, signal done.
func (o *CycleOrchestrator) subscribeCleanup(ctx context.Context, done chan struct{}) {
	// Subscribe to all terminal event types.
	closedMsgs, err := o.bus.Subscribe(ctx, events.TopicPositionClosed)
	if err != nil {
		o.deps.Log.Error("subscribe failed", slog.String("topic", events.TopicPositionClosed), slog.Any("error", err))
	}
	timeoutMsgs, err := o.bus.Subscribe(ctx, events.TopicCycleTimeout)
	if err != nil {
		o.deps.Log.Error("subscribe failed", slog.String("topic", events.TopicCycleTimeout), slog.Any("error", err))
	}
	abortMsgs, err := o.bus.Subscribe(ctx, events.TopicCycleAbort)
	if err != nil {
		o.deps.Log.Error("subscribe failed", slog.String("topic", events.TopicCycleAbort), slog.Any("error", err))
	}

	cleanup := o.makeCleanupFn(ctx, done)

	o.watchTerminalTopic(ctx, closedMsgs, func(msg *message.Message) {
		evt, parseErr := unmarshal[events.PositionClosedEvent](msg.Payload)
		if parseErr == nil {
			cleanup(evt.Reason)
		} else {
			cleanup("position_closed")
		}
	})

	o.watchTerminalTopic(ctx, timeoutMsgs, func(_ *message.Message) {
		cleanup("timeout")
	})

	o.watchTerminalTopic(ctx, abortMsgs, func(msg *message.Message) {
		evt, parseErr := unmarshal[events.CycleAbortEvent](msg.Payload)
		if parseErr == nil {
			o.recorder.AbortReason = evt.Reason
			o.recorder.AbortPhase = evt.Phase
		}
		cleanup("abort")
	})
}

// makeCleanupFn returns a closure that handles cycle cleanup and recording.
func (o *CycleOrchestrator) makeCleanupFn(ctx context.Context, done chan struct{}) func(string) {
	return func(reason string) {
		o.deps.Log.Info("🧹 Cleanup", slog.String("reason", reason))
		o.subs.UnsubscribeAll(ctx)

		// Capture exit data for cycle record.
		o.recorder.ExitReason = reason
		o.recorder.ExitTime = time.Now()

		// Final MFE/MAE update from latest price.
		if o.recorder.Excursion != nil {
			if pd, err := o.deps.PriceStore.GetPrice(ctx, o.cfg.Symbol, 2*time.Second); err == nil {
				o.recorder.Excursion.Update(pd.LastPrice, time.Now())
			}
		}

		// Signal cycle completion (non-blocking in case already done).
		select {
		case done <- struct{}{}:
		default:
		}
	}
}

// watchTerminalTopic spawns a goroutine that waits for one message on the given
// channel and invokes the handler, or returns on context cancellation.
func (o *CycleOrchestrator) watchTerminalTopic(
	ctx context.Context,
	msgs <-chan *message.Message,
	handler func(*message.Message),
) {
	go func() {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-msgs:
			if !ok {
				return
			}
			msg.Ack()
			handler(msg)
		}
	}()
}

// subscribeEventLog subscribes to key topics and logs them for audit.
// This is a passive observer — it does not affect the event chain.
func (o *CycleOrchestrator) subscribeEventLog(ctx context.Context) {
	topics := []string{
		events.TopicCycleStart,
		events.TopicCandidateFound,
		events.TopicArmed,
		events.TopicWaitComplete,
		events.TopicConfirmed,
		events.TopicIOCFired,
		events.TopicTrapFired,
		events.TopicOrderFilled,
		events.TopicTrailingPlaced,
		events.TopicOBWallFound,
		events.TopicPositionClosed,
		events.TopicCycleTimeout,
		events.TopicCycleAbort,
		events.TopicCycleError,
	}

	for _, topic := range topics {
		o.consumeTopic(ctx, topic, func(msg *message.Message) {
			o.deps.Log.Info("📋 Event", slog.String("topic", topic), slog.String("msg_id", msg.UUID))
		})
	}
}
