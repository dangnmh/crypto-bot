package eventbus_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"crypto-bot/pkg/eventbus"
)

func TestNew(t *testing.T) {
	t.Parallel()

	bus := eventbus.New(slog.Default())
	require.NotNil(t, bus)
	assert.Empty(t, bus.Timeline())
	require.NoError(t, bus.Close())
}

func TestPublishAndSubscribe(t *testing.T) {
	t.Parallel()

	bus := eventbus.New(slog.Default())
	defer func() { _ = bus.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	type TestEvent struct {
		Value string `json:"value"`
	}

	// Subscribe BEFORE publish (GoChannel requires this)
	msgs, err := bus.Subscribe(ctx, "test.topic")
	require.NoError(t, err)

	// Publish
	err = bus.Publish("test.topic", TestEvent{Value: "hello"})
	require.NoError(t, err)

	// Receive
	select {
	case msg := <-msgs:
		var evt TestEvent
		require.NoError(t, json.Unmarshal(msg.Payload, &evt))
		assert.Equal(t, "hello", evt.Value)
		msg.Ack()
	case <-ctx.Done():
		t.Fatal("timeout waiting for message")
	}
}

func TestTimeline(t *testing.T) {
	t.Parallel()

	bus := eventbus.New(slog.Default())
	defer func() { _ = bus.Close() }()

	require.NoError(t, bus.Publish("topic.a", map[string]string{"k": "1"}))
	require.NoError(t, bus.Publish("topic.b", map[string]string{"k": "2"}))
	require.NoError(t, bus.Publish("topic.a", map[string]string{"k": "3"}))

	timeline := bus.Timeline()
	require.Len(t, timeline, 3)

	assert.Equal(t, "topic.a", timeline[0].Topic)
	assert.Equal(t, "topic.b", timeline[1].Topic)
	assert.Equal(t, "topic.a", timeline[2].Topic)

	// Verify timeline is a copy (mutations don't affect bus)
	timeline[0].Topic = "mutated"
	assert.Equal(t, "topic.a", bus.Timeline()[0].Topic)
}

func TestFanOut(t *testing.T) {
	t.Parallel()

	bus := eventbus.New(slog.Default())
	defer func() { _ = bus.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Two subscribers on same topic
	sub1, err := bus.Subscribe(ctx, "shared.topic")
	require.NoError(t, err)

	sub2, err := bus.Subscribe(ctx, "shared.topic")
	require.NoError(t, err)

	require.NoError(t, bus.Publish("shared.topic", "payload"))

	// Both should receive the message
	for _, sub := range []<-chan *message.Message{sub1, sub2} {
		select {
		case msg := <-sub:
			msg.Ack()
		case <-ctx.Done():
			t.Fatal("timeout: subscriber did not receive message")
		}
	}
}

func TestDumpTimeline(t *testing.T) {
	t.Parallel()

	bus := eventbus.New(slog.Default())
	defer func() { _ = bus.Close() }()

	// Should not panic on empty timeline
	bus.DumpTimeline(slog.Default())

	require.NoError(t, bus.Publish("test.event", "data"))
	bus.DumpTimeline(slog.Default())

	assert.Len(t, bus.Timeline(), 1)
}

func TestPublish_MarshalError(t *testing.T) {
	t.Parallel()

	bus := eventbus.New(slog.Default())
	defer func() { _ = bus.Close() }()

	// Trigger marshal error by passing an un-marshalable channel
	err := bus.Publish("topic", make(chan int))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "marshal payload")
}

func TestPublish_OnClosedBus(t *testing.T) {
	t.Parallel()

	bus := eventbus.New(slog.Default())
	require.NoError(t, bus.Close())

	err := bus.Publish("topic", "data")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "publish to")
}

type mockDeduplicatable struct {
	Key string `json:"key"`
	Val string `json:"val"`
}

func (m mockDeduplicatable) DeduplicateKey() string {
	return m.Key
}

func TestPublish_Deduplication(t *testing.T) {
	t.Parallel()

	bus := eventbus.New(slog.Default())
	defer func() { _ = bus.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	msgs, err := bus.Subscribe(ctx, "test.dedup")
	require.NoError(t, err)

	// Publish first event
	evt1 := mockDeduplicatable{Key: "key-1", Val: "first"}
	err = bus.Publish("test.dedup", evt1)
	require.NoError(t, err)

	// Publish second event with same key (should be deduplicated/dropped)
	evt2 := mockDeduplicatable{Key: "key-1", Val: "second"}
	err = bus.Publish("test.dedup", evt2)
	require.NoError(t, err)

	// Publish third event with different key (should succeed)
	evt3 := mockDeduplicatable{Key: "key-2", Val: "third"}
	err = bus.Publish("test.dedup", evt3)
	require.NoError(t, err)

	// We should receive exactly 2 messages (evt1 and evt3, in any order)
	var received []string
	for range 2 {
		select {
		case msg := <-msgs:
			var rec mockDeduplicatable
			require.NoError(t, json.Unmarshal(msg.Payload, &rec))
			received = append(received, rec.Val)
			msg.Ack()
		case <-ctx.Done():
			t.Fatal("timeout waiting for message")
		}
	}

	assert.Contains(t, received, "first")
	assert.Contains(t, received, "third")
	assert.NotContains(t, received, "second")

	// Verify timeline only has 2 entries (since the duplicate one is dropped before log/publish)
	timeline := bus.Timeline()
	assert.Len(t, timeline, 2)
}
