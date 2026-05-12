package store_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/internal/testutil/mocks"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// ── TickerStore sync ─────────────────────────────────────────────────.

func TestTickerStore_StartTickerSync(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)

	client := mocks.NewMockClient(ctrl)
	client.EXPECT().GetTickers(gomock.Any(), "").Return([]exchange.Ticker{
		{Symbol: "BTC_USDT", LastPrice: 50000, FundingRate: 0.001},
		{Symbol: "ETH_USDT", LastPrice: 3000, FundingRate: 0.002},
	}, nil).AnyTimes()

	wg := &sync.WaitGroup{}
	ts := store.NewTickerStore(wg)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go ts.StartTickerSync(ctx, client, time.Hour) // long interval, cancelled by ctx

	// WaitReady should return once the first sync completes.
	wg.Wait()

	td, err := ts.GetTicker(ctx, "BTC_USDT")
	require.NoError(t, err)
	assert.Equal(t, "BTC_USDT", td.Symbol)
	assert.Equal(t, 50000.0, td.LastPrice)

	all := ts.GetAllTickers(ctx)
	assert.Len(t, all, 2)
}

func TestTickerStore_StartTickerSync_Error(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)

	client := mocks.NewMockClient(ctrl)
	client.EXPECT().GetTickers(gomock.Any(), "").Return(nil, fmt.Errorf("network error")).AnyTimes()

	wg := &sync.WaitGroup{}
	ts := store.NewTickerStore(wg)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go ts.StartTickerSync(ctx, client, time.Hour)

	// readyWG never fires because sync failed, but StartTickerSync should not panic.
	<-ctx.Done()

	_, err := ts.GetTicker(ctx, "BTC_USDT")
	assert.Error(t, err, "store should be empty after failed sync")
}

// ── ContractStore sync ───────────────────────────────────────────────.

func TestContractStore_StartContractSync(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)

	client := mocks.NewMockClient(ctrl)
	client.EXPECT().GetContractDetails(gomock.Any()).Return([]exchange.ContractDetail{
		{Symbol: "BTC_USDT", PriceScale: 2, VolScale: 0, MinVol: 1},
	}, nil).AnyTimes()

	wg := &sync.WaitGroup{}
	cs := store.NewContractStore(wg)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go cs.StartContractSync(ctx, client, time.Hour)

	wg.Wait()

	cd, err := cs.GetContract(ctx, "BTC_USDT")
	require.NoError(t, err)
	assert.Equal(t, "BTC_USDT", cd.Symbol)
	assert.Equal(t, int(2), cd.PriceScale)
}

func TestContractStore_StartContractSync_Error(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)

	client := mocks.NewMockClient(ctrl)
	client.EXPECT().GetContractDetails(gomock.Any()).Return(nil, fmt.Errorf("api error")).AnyTimes()

	wg := &sync.WaitGroup{}
	cs := store.NewContractStore(wg)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go cs.StartContractSync(ctx, client, time.Hour)

	<-ctx.Done()

	_, err := cs.GetContract(ctx, "BTC_USDT")
	assert.Error(t, err)
}

// ── FundingStore sync ────────────────────────────────────────────────.

func TestFundingStore_StartFundingSync(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)

	client := mocks.NewMockClient(ctrl)
	client.EXPECT().GetFundingRate(gomock.Any(), "BTC_USDT").Return(&exchange.FundingRateDetail{
		Symbol:         "BTC_USDT",
		FundingRate:    0.001,
		NextSettleTime: time.Now().Add(time.Hour).UnixMilli(),
	}, nil).AnyTimes()

	wg := &sync.WaitGroup{}
	fs := store.NewFundingStore(wg)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go fs.StartFundingSync(ctx, client, []string{"BTC_USDT"}, time.Hour)

	wg.Wait()

	fd, err := fs.GetFunding(ctx, "BTC_USDT")
	require.NoError(t, err)
	assert.Equal(t, 0.001, fd.FundingRate)
}

func TestFundingStore_StartFundingSync_Error(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)

	client := mocks.NewMockClient(ctrl)
	client.EXPECT().GetFundingRate(gomock.Any(), "BTC_USDT").Return(nil, fmt.Errorf("funding error")).AnyTimes()

	wg := &sync.WaitGroup{}
	fs := store.NewFundingStore(wg)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go fs.StartFundingSync(ctx, client, []string{"BTC_USDT"}, time.Hour)

	<-ctx.Done()

	_, err := fs.GetFunding(ctx, "BTC_USDT")
	assert.Error(t, err)
}

func TestFundingStore_StartFundingSync_MultipleSymbols(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)

	// Mock only returns data for BTC_USDT, so ETH_USDT will fail silently.
	client := mocks.NewMockClient(ctrl)
	client.EXPECT().GetFundingRate(gomock.Any(), "BTC_USDT").Return(&exchange.FundingRateDetail{
		Symbol:         "BTC_USDT",
		FundingRate:    0.001,
		NextSettleTime: time.Now().Add(time.Hour).UnixMilli(),
	}, nil).AnyTimes()
	client.EXPECT().GetFundingRate(gomock.Any(), "ETH_USDT").Return(nil, fmt.Errorf("no data")).AnyTimes()

	wg := &sync.WaitGroup{}
	fs := store.NewFundingStore(wg)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go fs.StartFundingSync(ctx, client, []string{"BTC_USDT", "ETH_USDT"}, time.Hour)

	wg.Wait()

	// BTC exists, ETH does not.
	_, err := fs.GetFunding(ctx, "BTC_USDT")
	assert.NoError(t, err)

	_, err = fs.GetFunding(ctx, "ETH_USDT")
	assert.Error(t, err)
}
