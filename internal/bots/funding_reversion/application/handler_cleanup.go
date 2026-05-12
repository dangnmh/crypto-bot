package application

import (
	"context"

	"crypto-bot/internal/bots/funding_reversion/application/events"

	"github.com/ThreeDotsLabs/watermill/message"
)

// subscribeCleanup handles terminal events → unsubscribe WS, signal done.
func (o *CycleOrchestrator) subscribeCleanup(ctx context.Context, done chan struct{}) {
	// Subscribe to all terminal event types
	closedMsgs, err := o.bus.Subscribe(ctx, events.TopicPositionClosed)
	if err != nil {
		o.deps.Log.Error("subscribe failed", "topic", events.TopicPositionClosed, "error", err)
	}
	timeoutMsgs, err := o.bus.Subscribe(ctx, events.TopicCycleTimeout)
	if err != nil {
		o.deps.Log.Error("subscribe failed", "topic", events.TopicCycleTimeout, "error", err)
	}
	abortMsgs, err := o.bus.Subscribe(ctx, events.TopicCycleAbort)
	if err != nil {
		o.deps.Log.Error("subscribe failed", "topic", events.TopicCycleAbort, "error", err)
	}

	cleanup := func(reason string) {
		o.deps.Log.Info("🧹 Cleanup", "reason", reason)
		o.subs.UnsubscribeAll()

		// Signal cycle completion (non-blocking in case already done)
		select {
		case done <- struct{}{}:
		default:
		}
	}

	go func() {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-closedMsgs:
			if !ok {
				return
			}
			msg.Ack()
			cleanup("position_closed")
		}
	}()

	go func() {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-timeoutMsgs:
			if !ok {
				return
			}
			msg.Ack()
			cleanup("timeout")
		}
	}()

	go func() {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-abortMsgs:
			if !ok {
				return
			}
			msg.Ack()
			cleanup("abort")
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
		t := topic // capture
		o.consumeTopic(ctx, t, func(msg *message.Message) {
			o.deps.Log.Info("📋 Event", "topic", t, "msg_id", msg.UUID)
		})
	}
}
