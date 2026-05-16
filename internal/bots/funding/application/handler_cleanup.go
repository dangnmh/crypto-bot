package application

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"crypto-bot/internal/bots/funding/application/events"
	"crypto-bot/internal/bots/funding/domain"

	"github.com/ThreeDotsLabs/watermill/message"
)

// subscribeCleanup handles terminal events → unsubscribe WS, signal done.
func (o *CycleOrchestrator) subscribeCleanup(ctx context.Context, done chan struct{}) {
	closedTopics := []string{
		events.TopicReversionPositionClosed,
		events.TopicTrapPositionClosed,
	}
	timeoutTopics := []string{
		events.TopicReversionTimeout,
		events.TopicTrapTimeout,
	}
	abortTopics := []string{
		events.TopicReversionAbort,
		events.TopicTrapAbort,
	}

	cleanup := o.makeCleanupFn(ctx, done)

	for _, topic := range closedTopics {
		o.watchTerminalEvent(ctx, topic, func(msg *message.Message) {
			evt, parseErr := unmarshal[events.PositionClosedEvent](msg.Payload)
			flow := terminalFlow(topic)
			if parseErr == nil {
				flow = evt.Flow
				cleanup(topic, flow, evt.Reason)
			} else {
				cleanup(topic, flow, "position_closed")
			}
		})
	}

	for _, topic := range timeoutTopics {
		o.watchTerminalEvent(ctx, topic, func(msg *message.Message) {
			flow := terminalFlow(topic)
			if evt, parseErr := unmarshal[events.CycleTimeoutEvent](msg.Payload); parseErr == nil {
				flow = evt.Flow
			}
			cleanup(topic, flow, "timeout")
		})
	}

	for _, topic := range abortTopics {
		o.watchTerminalEvent(ctx, topic, func(msg *message.Message) {
			evt, parseErr := unmarshal[events.CycleAbortEvent](msg.Payload)
			flow := terminalFlow(topic)
			if parseErr == nil {
				flow = evt.Flow
				o.recorder.Mutate(func(b *domain.CycleRecordBuilder) {
					b.AbortReason = evt.Reason
					b.AbortFlow = flow
					b.AbortTopic = topic
				})
			}
			cleanup(topic, flow, "abort")
		})
	}
}

func (o *CycleOrchestrator) watchTerminalEvent(
	ctx context.Context,
	topic string,
	handler func(*message.Message),
) {
	msgs, err := o.bus.Subscribe(ctx, topic)
	if err != nil {
		o.deps.Log.Error("subscribe failed", slog.String("topic", topic), slog.Any("error", err))
		return
	}
	o.watchTerminalTopic(ctx, msgs, handler)
}

// makeCleanupFn returns a closure that handles cycle cleanup and recording.
func (o *CycleOrchestrator) makeCleanupFn(ctx context.Context, done chan struct{}) func(string, string, string) {
	var once sync.Once
	return func(topic, flow, reason string) {
		once.Do(func() {
			startedAt := time.Now()
			o.deps.Log.Info("🧹 Cleanup", slog.String("reason", reason), slog.String("topic", topic), slog.String("flow", flow))
			o.subs.UnsubscribeAll(ctx)
			unsubscribed := true
			if o.excursionCancel != nil {
				o.excursionCancel()
				o.excursionCancel = nil
			}

			// Capture exit data for cycle record.
			o.recorder.Mutate(func(b *domain.CycleRecordBuilder) {
				b.ExitReason = reason
				b.ExitTime = time.Now()
			})

			// Final MFE/MAE update from latest price.
			excursionFinalized := false
			if o.recorder.Excursion != nil {
				if pd, err := o.deps.PriceStore.GetPrice(ctx, o.cfg.Symbol, 2*time.Second); err == nil {
					o.recorder.Excursion.Update(pd.LastPrice, time.Now())
					excursionFinalized = true
				}
			}
			completedAt := time.Now()
			o.recorder.Mutate(func(b *domain.CycleRecordBuilder) {
				b.Cleanup = domain.CleanupSnapshot{
					TerminalFlow:       flow,
					TerminalTopic:      topic,
					Reason:             reason,
					StartedAt:          startedAt,
					CompletedAt:        completedAt,
					Unsubscribed:       unsubscribed,
					ExcursionFinalized: excursionFinalized,
				}
			})

			// Signal cycle completion (non-blocking in case already done).
			select {
			case done <- struct{}{}:
			default:
			}
		})
	}
}

func terminalFlow(topic string) string {
	switch topic {
	case events.TopicTrapPositionClosed, events.TopicTrapTimeout, events.TopicTrapAbort:
		return events.FlowTrap
	case events.TopicReversionPositionClosed, events.TopicReversionTimeout, events.TopicReversionAbort:
		return events.FlowReversion
	default:
		return ""
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
		events.TopicScanStart,
		events.TopicScanCandidateFound,
		events.TopicScanAbort,
		events.TopicReversionCandidate,
		events.TopicReversionArmed,
		events.TopicReversionWaitComplete,
		events.TopicReversionConfirmed,
		events.TopicReversionIOCFired,
		events.TopicReversionOrderFilled,
		events.TopicReversionTrailingPlaced,
		events.TopicReversionPositionClosed,
		events.TopicReversionTimeout,
		events.TopicReversionAbort,
		events.TopicReversionError,
		events.TopicTrapCandidate,
		events.TopicTrapOBWallFound,
		events.TopicTrapOrderPlaced,
		events.TopicTrapOrderFilled,
		events.TopicTrapTrailingPlaced,
		events.TopicTrapPositionClosed,
		events.TopicTrapTimeout,
		events.TopicTrapAbort,
		events.TopicTrapError,
	}

	for _, topic := range topics {
		o.consumeTopic(ctx, topic, func(msg *message.Message) {
			o.deps.Log.Info("📋 Event", slog.String("topic", topic), slog.String("msg_id", msg.UUID))
		})
	}
}
