package watcher_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/watcher"
	"crypto-bot/pkg/eventbus"

	"github.com/stretchr/testify/assert"
)

func TestOrderWatcher_OnPositionUpdate_Callback(t *testing.T) {
	t.Parallel()

	logger := slog.Default()
	bus := eventbus.New(logger)
	defer func() { _ = bus.Close() }()

	w := watcher.NewOrderWatcher(bus, exchange.ExchangeMexc, logger)

	called := make(chan exchange.PersonalPositionUpdate, 1)
	w.OnPositionUpdate(context.Background(), "BTC_USDT", 2*time.Second, func(update exchange.PersonalPositionUpdate) {
		called <- update
	})

	time.Sleep(50 * time.Millisecond)

	w.PublishPosition(exchange.PersonalPositionUpdate{
		Symbol:          "BTC_USDT",
		HoldVolContract: 2,
		HoldAvgPrice:    100,
	})

	select {
	case update := <-called:
		assert.Equal(t, 2.0, update.HoldVolContract)
		assert.Equal(t, 100.0, update.HoldAvgPrice)
	case <-time.After(3 * time.Second):
		assert.Fail(t, "timeout waiting for position callback")
	}
}

func TestOrderWatcher_OnPositionUpdate_Timeout(t *testing.T) {
	t.Parallel()

	logger := slog.Default()
	bus := eventbus.New(logger)
	defer func() { _ = bus.Close() }()

	w := watcher.NewOrderWatcher(bus, exchange.ExchangeMexc, logger)

	called := make(chan struct{}, 1)
	w.OnPositionUpdate(context.Background(), "BTC_USDT", 100*time.Millisecond, func(exchange.PersonalPositionUpdate) {
		called <- struct{}{}
	})

	time.Sleep(200 * time.Millisecond)

	select {
	case <-called:
		assert.Fail(t, "callback should not have been called on timeout")
	default:
	}
}

func TestOrderWatcher_PositionRoutingBySymbol(t *testing.T) {
	t.Parallel()

	logger := slog.Default()
	bus := eventbus.New(logger)
	defer func() { _ = bus.Close() }()

	w := watcher.NewOrderWatcher(bus, exchange.ExchangeMexc, logger)

	called := make(chan exchange.PersonalPositionUpdate, 1)
	w.OnPositionUpdate(context.Background(), "BTC_USDT", 2*time.Second, func(update exchange.PersonalPositionUpdate) {
		called <- update
	})

	time.Sleep(50 * time.Millisecond)

	w.PublishPosition(exchange.PersonalPositionUpdate{Symbol: "ETH_USDT", HoldVolContract: 3})
	w.PublishPosition(exchange.PersonalPositionUpdate{Symbol: "BTC_USDT", HoldVolContract: 1})

	select {
	case update := <-called:
		assert.Equal(t, "BTC_USDT", update.Symbol)
		assert.Equal(t, 1.0, update.HoldVolContract)
	case <-time.After(3 * time.Second):
		assert.Fail(t, "timeout waiting for position callback")
	}
}

func TestOrderWatcher_OnTradeUpdate_Callback(t *testing.T) {
	t.Parallel()

	logger := slog.Default()
	bus := eventbus.New(logger)
	defer func() { _ = bus.Close() }()

	w := watcher.NewOrderWatcher(bus, exchange.ExchangeMexc, logger)

	called := make(chan []domain.PublicTrade, 1)
	w.OnTradeUpdate(context.Background(), "BTC_USDT", 2*time.Second, func(trades []domain.PublicTrade) {
		called <- trades
	})

	time.Sleep(50 * time.Millisecond)

	w.PublishTrades("BTC_USDT", []domain.PublicTrade{
		{Symbol: "BTC_USDT", Price: 65000.5, Volume: 2.0},
	})

	select {
	case trades := <-called:
		assert.Len(t, trades, 1)
		assert.Equal(t, 65000.5, trades[0].Price)
		assert.Equal(t, 2.0, trades[0].Volume)
	case <-time.After(3 * time.Second):
		assert.Fail(t, "timeout waiting for trade callback")
	}
}
