package service_test

import (
	"context"
	"testing"

	"crypto-bot/internal/bots/funding/application/service"
	"crypto-bot/internal/infrastructure/exchange"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeProvider struct {
	exchange.Client
	tickers []exchange.Ticker
	rates   []exchange.FundingRateResult
}

func (f *fakeProvider) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	return f.tickers, nil
}

func (f *fakeProvider) GetFundingRates(ctx context.Context, symbols []string) ([]exchange.FundingRateResult, error) {
	symbolMap := make(map[string]bool)
	for _, sym := range symbols {
		symbolMap[sym] = true
	}
	var res []exchange.FundingRateResult
	for _, r := range f.rates {
		if symbolMap[r.Symbol] {
			res = append(res, r)
		}
	}
	return res, nil
}

func TestFundingService_GetPotentialFundingSymbols(t *testing.T) {
	t.Parallel()

	tickers := []exchange.Ticker{
		{Symbol: "BTC_USDT", AmountUSDT24: 1000000},
		{Symbol: "ETH_USDT", AmountUSDT24: 500000},
		{Symbol: "SOL_USDT", AmountUSDT24: 150000},
		{Symbol: "ADA_USDT", AmountUSDT24: 50000},
		{Symbol: "DOGE_USDT", AmountUSDT24: 10000},
	}

	rates := []exchange.FundingRateResult{
		{Symbol: "BTC_USDT", Rate: 0.0001, SettleTime: 123},
		{Symbol: "ETH_USDT", Rate: 0.0002, SettleTime: 123},
		{Symbol: "SOL_USDT", Rate: 0.0003, SettleTime: 123},
		{Symbol: "ADA_USDT", Rate: 0.0004, SettleTime: 123},
		{Symbol: "DOGE_USDT", Rate: 0.0005, SettleTime: 123},
	}

	provider := &fakeProvider{
		tickers: tickers,
		rates:   rates,
	}

	fundingService := service.NewFundingService(provider)

	t.Run("no filters (whitelist empty, blacklist empty, vol matching)", func(t *testing.T) {
		t.Parallel()
		res, err := fundingService.GetPotentialFundingSymbols(context.Background(), 0, 0, nil, nil)
		require.NoError(t, err)
		assert.Len(t, res, 5)
		assert.Equal(t, "BTC_USDT", res[0].Symbol)
		assert.Equal(t, 1000000.0, res[0].Volume24h)
		assert.Equal(t, 0.0001, res[0].Rate)
	})

	t.Run("filter min volume", func(t *testing.T) {
		t.Parallel()
		res, err := fundingService.GetPotentialFundingSymbols(context.Background(), 100000, 0, nil, nil)
		require.NoError(t, err)
		assert.Len(t, res, 3) // BTC, ETH, SOL
		assert.Equal(t, "BTC_USDT", res[0].Symbol)
		assert.Equal(t, "ETH_USDT", res[1].Symbol)
		assert.Equal(t, "SOL_USDT", res[2].Symbol)
	})

	t.Run("filter min and max volume", func(t *testing.T) {
		t.Parallel()
		res, err := fundingService.GetPotentialFundingSymbols(context.Background(), 100000, 800000, nil, nil)
		require.NoError(t, err)
		assert.Len(t, res, 2) // ETH, SOL
		assert.Equal(t, "ETH_USDT", res[0].Symbol)
		assert.Equal(t, "SOL_USDT", res[1].Symbol)
	})

	t.Run("filter whitelist", func(t *testing.T) {
		t.Parallel()
		res, err := fundingService.GetPotentialFundingSymbols(context.Background(), 0, 0, []string{"BTC_USDT", "SOL_USDT"}, nil)
		require.NoError(t, err)
		assert.Len(t, res, 2)
		assert.Equal(t, "BTC_USDT", res[0].Symbol)
		assert.Equal(t, "SOL_USDT", res[1].Symbol)
	})

	t.Run("filter blacklist", func(t *testing.T) {
		t.Parallel()
		res, err := fundingService.GetPotentialFundingSymbols(context.Background(), 0, 0, nil, []string{"ETH_USDT", "ADA_USDT"})
		require.NoError(t, err)
		assert.Len(t, res, 3) // BTC, SOL, DOGE
		assert.Equal(t, "BTC_USDT", res[0].Symbol)
		assert.Equal(t, "SOL_USDT", res[1].Symbol)
		assert.Equal(t, "DOGE_USDT", res[2].Symbol)
	})

	t.Run("filter combined", func(t *testing.T) {
		t.Parallel()
		res, err := fundingService.GetPotentialFundingSymbols(context.Background(), 10000, 600000, []string{"ETH_USDT", "SOL_USDT", "DOGE_USDT"}, []string{"SOL_USDT"})
		require.NoError(t, err)
		assert.Len(t, res, 2) // ETH, DOGE (SOL blacklisted)
		assert.Equal(t, "ETH_USDT", res[0].Symbol)
		assert.Equal(t, "DOGE_USDT", res[1].Symbol)
	})

	t.Run("empty result case", func(t *testing.T) {
		t.Parallel()
		res, err := fundingService.GetPotentialFundingSymbols(context.Background(), 2000000, 0, nil, nil)
		require.NoError(t, err)
		assert.Nil(t, res)
	})
}
