package watcher_test

import (
	"context"
	"log/slog"
	"testing"

	"crypto-bot/internal/infrastructure/watcher"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/eventbus"

	"github.com/stretchr/testify/assert"
)

func TestOrderWatcher_OnOrderUpdate_Callback(t *testing.T) {
	t.Parallel()

	logger := slog.Default()
	bus := eventbus.New(logger)
	defer func() { _ = bus.Close() }()

	w := watcher.NewOrderWatcher(bus, logger)

	called := make(chan exchange.WsOrderDeal, 1)
	w.OnOrderUpdate(context.Background(), "order_456", 2*time.Second, func(deal exchange.WsOrderDeal) {
		called <- deal
	})

	// Give the goroutine time to subscribe.
	time.Sleep(50 * time.Millisecond)

	deal := exchange.WsOrderDeal{
		OrderID: "order_456",
		State:   3,
		DealVol: 5,
	}
	w.Publish(deal)

	select {
	case d := <-called:
		assert.Equal(t, 5.0, d.DealVol)
	case <-time.After(3 * time.Second):
		assert.Fail(t, "timeout waiting for callback")
	}
}

func TestOrderWatcher_OnOrderUpdate_Timeout(t *testing.T) {
	t.Parallel()

	logger := slog.Default()
	bus := eventbus.New(logger)
	defer func() { _ = bus.Close() }()

	w := watcher.NewOrderWatcher(bus, logger)

	called := make(chan struct{}, 1)
	w.OnOrderUpdate(context.Background(), "order_timeout", 100*time.Millisecond, func(_ exchange.WsOrderDeal) {
		called <- struct{}{}
	})

	// Don't publish anything — should timeout without calling.
	time.Sleep(200 * time.Millisecond)

	select {
	case <-called:
		assert.Fail(t, "callback should not have been called on timeout")
	default:
		// Expected — no callback.
	}
}

func TestOrderWatcher_RemoveOrderCallback(t *testing.T) {
	t.Parallel()

	logger := slog.Default()
	bus := eventbus.New(logger)
	defer func() { _ = bus.Close() }()

	w := watcher.NewOrderWatcher(bus, logger)

	// RemoveOrderCallback is a no-op, but should not panic.
	assert.NotPanics(t, func() {
		w.RemoveOrderCallback("any_order")
	})
}
