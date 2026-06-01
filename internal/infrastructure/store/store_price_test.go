package store_test

import (
	"context"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"log/slog"
)

func TestPriceStore_UpdateAndGetPrice(t *testing.T) {
	t.Parallel()

	s := store.NewPriceStore(slog.Default())
	ctx := context.Background()

	pd := &store.PriceData{
		Symbol:    "BTC_USDT",
		LastPrice: 65000,
		BestBid:   64999,
		BestAsk:   65001,
		UpdatedAt: time.Now(),
	}
	s.UpdatePrice("BTC_USDT", pd)

	got, err := s.GetPrice(ctx, "BTC_USDT", 5*time.Second)
	require.NoError(t, err)
	assert.Equal(t, 65000.0, got.LastPrice)
}

func TestPriceStore_ReturnsSnapshot(t *testing.T) {
	t.Parallel()

	s := store.NewPriceStore(slog.Default())
	ctx := context.Background()
	input := &store.PriceData{
		Symbol:    "BTC_USDT",
		LastPrice: 65000,
		UpdatedAt: time.Now(),
	}
	s.UpdatePrice("BTC_USDT", input)

	input.LastPrice = 1
	got, err := s.GetPrice(ctx, "BTC_USDT", 5*time.Second)
	require.NoError(t, err)
	assert.Equal(t, 65000.0, got.LastPrice)

	got.LastPrice = 2
	gotAgain, err := s.GetPrice(ctx, "BTC_USDT", 5*time.Second)
	require.NoError(t, err)
	assert.Equal(t, 65000.0, gotAgain.LastPrice)
}

func TestPriceStore_GetPrice_Missing(t *testing.T) {
	t.Parallel()

	s := store.NewPriceStore(slog.Default())
	_, err := s.GetPrice(context.Background(), "NONEXISTENT", 5*time.Second)
	assert.Error(t, err)
}

func TestPriceStore_GetPrice_Stale(t *testing.T) {
	t.Parallel()

	s := store.NewPriceStore(slog.Default())
	pd := &store.PriceData{
		Symbol:    "BTC_USDT",
		UpdatedAt: time.Now().Add(-10 * time.Second),
	}
	s.UpdatePrice("BTC_USDT", pd)

	_, err := s.GetPrice(context.Background(), "BTC_USDT", 5*time.Second)
	assert.Error(t, err)
}

func TestPriceStore_GetBestBidAsk(t *testing.T) {
	t.Parallel()

	s := store.NewPriceStore(slog.Default())
	s.UpdatePrice("BTC_USDT", &store.PriceData{BestBid: 100, BestAsk: 101})

	bid, ask, err := s.GetBestBidAsk(context.Background(), "BTC_USDT")
	require.NoError(t, err)
	assert.Equal(t, 100.0, bid)
	assert.Equal(t, 101.0, ask)
}

func TestPriceStore_GetBestBidAsk_Missing(t *testing.T) {
	t.Parallel()

	s := store.NewPriceStore(slog.Default())
	_, _, err := s.GetBestBidAsk(context.Background(), "NONEXISTENT")
	assert.Error(t, err)
}

func TestPriceStore_PriceAge(t *testing.T) {
	t.Parallel()

	s := store.NewPriceStore(slog.Default())

	// Missing symbol returns very large age.
	age := s.PriceAge("NONEXISTENT")
	assert.GreaterOrEqual(t, age, 24*time.Hour)

	// Recent update returns small age.
	s.UpdatePrice("BTC_USDT", &store.PriceData{UpdatedAt: time.Now()})
	age = s.PriceAge("BTC_USDT")
	assert.LessOrEqual(t, age, time.Second)
}

func TestPriceStore_SubscribePrice(t *testing.T) {
	t.Parallel()

	s := store.NewPriceStore(slog.Default())
	ctx := t.Context()

	btcUpdates := s.SubscribePrice(ctx, "BTC_USDT")
	ethUpdates := s.SubscribePrice(ctx, "ETH_USDT")

	s.UpdatePrice("BTC_USDT", &store.PriceData{LastPrice: 100})

	got := <-btcUpdates
	assert.Equal(t, "BTC_USDT", got.Symbol)
	assert.Equal(t, 100.0, got.LastPrice)
	assert.False(t, got.UpdatedAt.IsZero())

	select {
	case got := <-ethUpdates:
		t.Fatalf("unexpected ETH update: %+v", got)
	default:
	}
}

func TestPriceStore_SubscribePrice_ContextCancelClosesChannel(t *testing.T) {
	t.Parallel()

	s := store.NewPriceStore(slog.Default())
	ctx, cancel := context.WithCancel(context.Background())
	updates := s.SubscribePrice(ctx, "BTC_USDT")

	cancel()

	select {
	case _, ok := <-updates:
		assert.False(t, ok)
	case <-time.After(time.Second):
		t.Fatal("subscription channel was not closed")
	}
}

func TestPriceStore_SubscribePrice_NonBlockingUpdate(t *testing.T) {
	t.Parallel()

	s := store.NewPriceStore(slog.Default())
	ctx := t.Context()

	_ = s.SubscribePrice(ctx, "BTC_USDT")

	done := make(chan struct{})
	go func() {
		s.UpdatePrice("BTC_USDT", &store.PriceData{LastPrice: 100})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("UpdatePrice blocked on an unbuffered subscriber")
	}
}
