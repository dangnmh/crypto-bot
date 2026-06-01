package store_test

import (
	"context"
	"sync"
	"testing"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/internal/testutil/mocks"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"log/slog"
)

func TestTickerStore_GetTicker(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)

	client := mocks.NewMockClient(ctrl)
	client.EXPECT().GetTickers(gomock.Any(), "").Return([]exchange.Ticker{
		{Symbol: "BTC_USDT", FundingRate: 0.001, LastPrice: 65000, Bid1: 64999, Ask1: 65001},
	}, nil).AnyTimes()

	wg := &sync.WaitGroup{}
	ts := store.NewTickerStore(wg, slog.Default())

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go ts.StartTickerSync(ctx, client, time.Hour)
	wg.Wait()

	td, err := ts.GetTicker(context.Background(), "BTC_USDT")
	require.NoError(t, err)
	assert.Equal(t, 0.001, td.FundingRate)
	assert.Equal(t, 65000.0, td.LastPrice)
}

func TestTickerStore_GetTicker_Missing(t *testing.T) {
	t.Parallel()

	wg := &sync.WaitGroup{}
	ts := store.NewTickerStore(wg, slog.Default())
	wg.Done()

	_, err := ts.GetTicker(context.Background(), "NONEXISTENT")
	assert.Error(t, err)
}

func TestTickerStore_GetAllTickers(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)

	client := mocks.NewMockClient(ctrl)
	client.EXPECT().GetTickers(gomock.Any(), "").Return([]exchange.Ticker{
		{Symbol: "BTC_USDT"},
		{Symbol: "ETH_USDT"},
	}, nil).AnyTimes()

	wg := &sync.WaitGroup{}
	ts := store.NewTickerStore(wg, slog.Default())

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go ts.StartTickerSync(ctx, client, time.Hour)
	wg.Wait()

	all := ts.GetAllTickers(context.Background())
	assert.Len(t, all, 2)
}
