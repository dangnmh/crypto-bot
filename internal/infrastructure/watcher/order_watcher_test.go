package watcher_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/watcher"
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

func TestOrderWatcher_OnOrderDealBySymbolSide(t *testing.T) {
	t.Parallel()

	logger := slog.Default()
	bus := eventbus.New(logger)
	defer func() { _ = bus.Close() }()

	w := watcher.NewOrderWatcher(bus, logger)

	bySide := make(chan exchange.PersonalOrderDeal, 1)
	w.OnOrderDealBySymbolSide(context.Background(), "BTC_USDT", exchange.SideStr(exchange.SideCloseLong), 2*time.Second, func(deal exchange.PersonalOrderDeal) {
		bySide <- deal
	})

	time.Sleep(50 * time.Millisecond)

	deal := exchange.PersonalOrderDeal{
		OrderID: "order_789",
		Symbol:  "BTC_USDT",
		Side:    exchange.SideCloseLong,
		Vol:     3,
		Price:   50001,
	}
	w.PublishDeal(deal)

	select {
	case d := <-bySide:
		assert.Equal(t, 50001.0, d.Price)
	case <-time.After(3 * time.Second):
		assert.Fail(t, "timeout waiting for symbol-side deal callback")
	}
}

func TestOrderWatcher_OnTrackAndPositionUpdate(t *testing.T) {
	t.Parallel()

	logger := slog.Default()
	bus := eventbus.New(logger)
	defer func() { _ = bus.Close() }()

	w := watcher.NewOrderWatcher(bus, logger)

	// OnTrackOrderUpdate subscribes by track ID and child order ID, so one
	// publish can legitimately fan out through both subscriptions.
	trackCalled := make(chan exchange.PersonalTrackOrderUpdate, 2)
	positionCalled := make(chan exchange.PersonalPositionUpdate, 1)
	w.OnTrackOrderUpdate(context.Background(), "track_1", "order_1", 2*time.Second, func(update exchange.PersonalTrackOrderUpdate) {
		trackCalled <- update
	})
	w.OnPositionUpdate(context.Background(), "BTC_USDT", 2*time.Second, func(update exchange.PersonalPositionUpdate) {
		positionCalled <- update
	})

	time.Sleep(50 * time.Millisecond)

	w.PublishTrackOrder(exchange.PersonalTrackOrderUpdate{ID: "track_1", OrderID: "order_1", Symbol: "BTC_USDT", State: 1})
	w.PublishPosition(exchange.PersonalPositionUpdate{Symbol: "BTC_USDT", HoldVol: 0})

	select {
	case update := <-trackCalled:
		assert.Equal(t, "track_1", update.GetID())
	case <-time.After(3 * time.Second):
		assert.Fail(t, "timeout waiting for track callback")
	}

	select {
	case update := <-positionCalled:
		assert.Equal(t, 0.0, update.HoldVol)
	case <-time.After(3 * time.Second):
		assert.Fail(t, "timeout waiting for position callback")
	}
}
