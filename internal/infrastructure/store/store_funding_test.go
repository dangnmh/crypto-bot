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

func TestFundingStore_GetFunding(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)

	client := mocks.NewMockClient(ctrl)
	client.EXPECT().GetFundingRates(gomock.Any(), gomock.Any()).Return([]exchange.FundingRateResult{
		{
			Symbol:     "BTC_USDT",
			Rate:       0.005,
			SettleTime: time.Now().Add(time.Hour).UnixMilli(),
		},
	}, nil).AnyTimes()

	wg := &sync.WaitGroup{}
	fs := store.NewFundingStore(wg, slog.Default())

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go fs.StartFundingSync(ctx, client, []string{"BTC_USDT"}, time.Hour)
	wg.Wait()

	fd, err := fs.GetFunding(context.Background(), "BTC_USDT")
	require.NoError(t, err)
	assert.Equal(t, 0.005, fd.FundingRate)
}

func TestFundingStore_GetFunding_Missing(t *testing.T) {
	t.Parallel()

	wg := &sync.WaitGroup{}
	fs := store.NewFundingStore(wg, slog.Default())
	wg.Done()

	_, err := fs.GetFunding(context.Background(), "NONEXISTENT")
	assert.Error(t, err)
}

func TestFundingStore_GetSettleTime(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)

	settleMs := time.Now().Add(time.Hour).UnixMilli()
	client := mocks.NewMockClient(ctrl)
	client.EXPECT().GetFundingRates(gomock.Any(), gomock.Any()).Return([]exchange.FundingRateResult{
		{
			Symbol:     "BTC_USDT",
			Rate:       0.001,
			SettleTime: settleMs,
		},
	}, nil).AnyTimes()

	wg := &sync.WaitGroup{}
	fs := store.NewFundingStore(wg, slog.Default())

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go fs.StartFundingSync(ctx, client, []string{"BTC_USDT"}, time.Hour)
	wg.Wait()

	st, err := fs.GetSettleTime(context.Background(), "BTC_USDT")
	require.NoError(t, err)
	assert.Equal(t, settleMs, st.UnixMilli())
}

func TestFundingStore_GetSettleTime_Missing(t *testing.T) {
	t.Parallel()

	wg := &sync.WaitGroup{}
	fs := store.NewFundingStore(wg, slog.Default())
	wg.Done()

	_, err := fs.GetSettleTime(context.Background(), "NONEXISTENT")
	assert.Error(t, err)
}
