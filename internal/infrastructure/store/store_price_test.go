package store_test

import (
	"context"
	"testing"

	"crypto-bot/internal/infrastructure/store"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPriceStore_UpdateAndGetPrice(t *testing.T) {
	t.Parallel()

	s := store.NewPriceStore()
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

func TestPriceStore_GetPrice_Missing(t *testing.T) {
	t.Parallel()

	s := store.NewPriceStore()
	_, err := s.GetPrice(context.Background(), "NONEXISTENT", 5*time.Second)
	assert.Error(t, err)
}

func TestPriceStore_GetPrice_Stale(t *testing.T) {
	t.Parallel()

	s := store.NewPriceStore()
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

	s := store.NewPriceStore()
	s.UpdatePrice("BTC_USDT", &store.PriceData{BestBid: 100, BestAsk: 101})

	bid, ask, err := s.GetBestBidAsk(context.Background(), "BTC_USDT")
	require.NoError(t, err)
	assert.Equal(t, 100.0, bid)
	assert.Equal(t, 101.0, ask)
}

func TestPriceStore_GetBestBidAsk_Missing(t *testing.T) {
	t.Parallel()

	s := store.NewPriceStore()
	_, _, err := s.GetBestBidAsk(context.Background(), "NONEXISTENT")
	assert.Error(t, err)
}

func TestPriceStore_PriceAge(t *testing.T) {
	t.Parallel()

	s := store.NewPriceStore()

	// Missing symbol returns very large age.
	age := s.PriceAge("NONEXISTENT")
	assert.GreaterOrEqual(t, age, 24*time.Hour)

	// Recent update returns small age.
	s.UpdatePrice("BTC_USDT", &store.PriceData{UpdatedAt: time.Now()})
	age = s.PriceAge("BTC_USDT")
	assert.LessOrEqual(t, age, time.Second)
}
