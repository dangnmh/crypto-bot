package store_test

import (
	"context"
	"testing"

	"crypto-bot/internal/infrastructure/store"

	"crypto-bot/internal/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDepthStore_UpdateAndGet(t *testing.T) {
	t.Parallel()
	s := store.NewDepthStore()
	ctx := context.Background()

	ob := &domain.OrderBook{
		Symbol:  "BTC_USDT",
		Version: 1,
		Asks:    []domain.OrderBookEntry{{Price: 101, Volume: 5}},
		Bids:    []domain.OrderBookEntry{{Price: 100, Volume: 10}},
	}

	s.UpdateDepth("BTC_USDT", ob)

	got, err := s.GetDepth(ctx, "BTC_USDT")
	require.NoError(t, err)
	assert.Equal(t, "BTC_USDT", got.Symbol)
	require.Len(t, got.Asks, 1)
	assert.Equal(t, 101.0, got.Asks[0].Price)
}

func TestDepthStore_GetDepth_NotFound(t *testing.T) {
	t.Parallel()
	s := store.NewDepthStore()
	_, err := s.GetDepth(context.Background(), "NONEXISTENT")
	assert.Error(t, err)
}

func TestDepthStore_DeleteDepth(t *testing.T) {
	t.Parallel()
	s := store.NewDepthStore()
	s.UpdateDepth("BTC_USDT", &domain.OrderBook{Symbol: "BTC_USDT"})
	s.DeleteDepth("BTC_USDT")

	_, err := s.GetDepth(context.Background(), "BTC_USDT")
	assert.Error(t, err)
}

func TestKlineBuffer_Add(t *testing.T) {
	t.Parallel()
	buf := store.NewKlineBuffer(3)

	buf.Add(domain.Kline{Timestamp: 1, Open: 1, Close: 2})
	buf.Add(domain.Kline{Timestamp: 2, Open: 2, Close: 3})
	buf.Add(domain.Kline{Timestamp: 3, Open: 3, Close: 4})

	klines := buf.GetKlines()
	require.Len(t, klines, 3)

	// Adding a 4th should evict the oldest.
	buf.Add(domain.Kline{Timestamp: 4, Open: 4, Close: 5})
	klines = buf.GetKlines()
	require.Len(t, klines, 3)
	assert.Equal(t, int64(2), klines[0].Timestamp)
}

func TestKlineBuffer_UpdateSameTimestamp(t *testing.T) {
	t.Parallel()
	buf := store.NewKlineBuffer(5)
	buf.Add(domain.Kline{Timestamp: 1, Close: 100})
	buf.Add(domain.Kline{Timestamp: 1, Close: 200}) // same ts = update

	klines := buf.GetKlines()
	require.Len(t, klines, 1)
	assert.Equal(t, 200.0, klines[0].Close)
}

func TestKlineBuffer_IgnoreOutOfOrder(t *testing.T) {
	t.Parallel()
	buf := store.NewKlineBuffer(5)
	buf.Add(domain.Kline{Timestamp: 5, Close: 100})
	buf.Add(domain.Kline{Timestamp: 3, Close: 50}) // older = ignore

	klines := buf.GetKlines()
	require.Len(t, klines, 1)
}

func TestKlineStore_InitAndGet(t *testing.T) {
	t.Parallel()
	s := store.NewKlineStore()
	ctx := context.Background()
	initial := []domain.Kline{
		{Timestamp: 1, Close: 100},
		{Timestamp: 2, Close: 200},
	}
	s.InitKlines("BTC_USDT", 10, initial)

	klines := s.GetKlines(ctx, "BTC_USDT")
	require.Len(t, klines, 2)
}

func TestKlineStore_AddKline_AutoCreate(t *testing.T) {
	t.Parallel()
	s := store.NewKlineStore()
	s.AddKline("NEW_SYMBOL", domain.Kline{Timestamp: 1, Close: 100})

	klines := s.GetKlines(context.Background(), "NEW_SYMBOL")
	require.Len(t, klines, 1)
}

func TestKlineStore_GetKlines_Missing(t *testing.T) {
	t.Parallel()
	s := store.NewKlineStore()
	klines := s.GetKlines(context.Background(), "NONEXISTENT")
	assert.Nil(t, klines)
}
