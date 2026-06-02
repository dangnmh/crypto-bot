// Package eventbus provides a lightweight, in-memory event bus built on top of
// Watermill's GoChannel. It is designed for intra-process, cycle-scoped event
// orchestration with built-in event logging for debugging and audit trails.
//
// The bus is NOT persistent — events live only for the lifetime of the Bus
// instance. This is intentional for short-lived trading cycles.
package eventbus

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
)

// Deduplicatable represents an event that can provide a key to prevent duplicate publishing.
type Deduplicatable interface {
	DeduplicateKey() string
}

// Bus wraps a Watermill GoChannel with event logging and lifecycle management.
// Create one Bus per trading cycle — it is NOT safe for reuse across cycles.
type Bus struct {
	pubsub       *gochannel.GoChannel
	deduplicator *middleware.Deduplicator
	logger       *slog.Logger

	mu  sync.RWMutex
	log []LogEntry
}

// New creates a new in-memory event bus.
// The bus fans out messages to all subscribers (each subscriber gets every message).
func New(logger *slog.Logger) *Bus {
	wmLogger := watermill.NewSlogLogger(logger)
	ps := gochannel.NewGoChannel(gochannel.Config{
		// Watermill fans out cycle events to multiple subscribers; this buffer absorbs
		// short intra-cycle bursts without blocking order lifecycle handlers.
		OutputChannelBuffer:            64,
		Persistent:                     false, // No persistence needed for cycle-scoped bus
		BlockPublishUntilSubscriberAck: false, // Non-blocking publish for performance
	}, wmLogger)

	repo, err := middleware.NewMapExpiringKeyRepository(24 * time.Hour)
	if err != nil {
		panic(fmt.Errorf("eventbus: failed to create expiring key repository: %w", err))
	}

	dedup := &middleware.Deduplicator{
		KeyFactory: func(msg *message.Message) (string, error) {
			key := msg.Metadata.Get("dedup_key")
			if key == "" {
				return "uuid:" + msg.UUID, nil
			}
			return key, nil
		},
		Repository: repo,
		Timeout:    5 * time.Second,
	}

	return &Bus{
		pubsub:       ps,
		deduplicator: dedup,
		logger:       logger,
		log:          make([]LogEntry, 0, 32),
	}
}

// Publish serializes the payload as JSON and publishes it to the given topic.
// The event is also appended to the in-memory event log.
func (b *Bus) Publish(topic string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("eventbus: marshal payload for topic %q: %w", topic, err)
	}

	msg := message.NewMessage(watermill.NewUUID(), data)

	// Extract deduplication key if payload implements Deduplicatable
	if dedup, ok := payload.(Deduplicatable); ok {
		key := dedup.DeduplicateKey()
		if key != "" {
			msg.Metadata.Set("dedup_key", key)

			// Check duplicate using Watermill Deduplicator
			isDup, err := b.deduplicator.IsDuplicate(msg)
			if err != nil {
				return fmt.Errorf("eventbus: check duplicate: %w", err)
			}
			if isDup {
				b.logger.Warn("eventbus: duplicate event detected, skipping publication",
					slog.String("key", key),
					slog.String("topic", topic),
				)
				return nil
			}
		}
	}

	// Record in event log
	b.mu.Lock()
	b.log = append(b.log, LogEntry{
		Time:    time.Now(),
		Topic:   topic,
		MsgID:   msg.UUID,
		Payload: data,
	})
	b.mu.Unlock()

	if err := b.pubsub.Publish(topic, msg); err != nil {
		return fmt.Errorf("eventbus: publish to %q: %w", topic, err)
	}

	return nil
}

// Subscribe returns a channel of messages for the given topic.
// Each subscriber receives ALL messages published to the topic (fan-out).
// The caller must Ack() each message after processing.
func (b *Bus) Subscribe(ctx context.Context, topic string) (<-chan *message.Message, error) {
	return b.pubsub.Subscribe(ctx, topic)
}

// Close shuts down the underlying GoChannel, closing all subscriber channels.
func (b *Bus) Close() error {
	return b.pubsub.Close()
}

// Timeline returns a copy of all recorded events in chronological order.
// Use this at the end of a cycle to dump the full event timeline for debugging.
func (b *Bus) Timeline() []LogEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()

	out := make([]LogEntry, len(b.log))
	copy(out, b.log)
	return out
}

// DumpTimeline logs all recorded events to the provided logger.
// Typically called at the end of a cycle for audit/debugging.
func (b *Bus) DumpTimeline(logger *slog.Logger) {
	entries := b.Timeline()
	if len(entries) == 0 {
		logger.Info("📋 Event timeline: (empty)")
		return
	}

	for idx, v := range entries {
		logger.Info("📋 Event timeline", slog.Int("idx", idx), slog.Any("msg", v))
	}
}
