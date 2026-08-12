package store_test

import (
	"testing"

	"crypto-bot/internal/infrastructure/store"

	"crypto-bot/internal/infrastructure/exchange"

	"github.com/stretchr/testify/assert"
)

func TestTickerDataFromExchange(t *testing.T) {
	t.Parallel()

	input := &exchange.Ticker{
		Symbol:       "BTC_USDT",
		Volume24:     5000,
		AmountUSDT24: 300000000,
		LastPrice:    65000,
		Bid1:         64999,
		Ask1:         65001,
	}

	td := store.TickerDataFromExchange(input)

	assert.Equal(t, "BTC_USDT", td.Symbol)
	assert.Equal(t, 300000000.0, td.AmountUSDT24)
	assert.Equal(t, 64999.0, td.BestBid)
	assert.Equal(t, 65001.0, td.BestAsk)
	assert.False(t, td.UpdatedAt.IsZero())
}

func TestTickerDataFromExchange_Amount24Fallback(t *testing.T) {
	t.Parallel()

	input := &exchange.Ticker{
		Symbol:       "KAITO_USDT",
		Volume24:     1000000,
		AmountUSDT24: 0, // 0 in response
		LastPrice:    0.70,
		Bid1:         0.699,
		Ask1:         0.701,
	}

	td := store.TickerDataFromExchange(input)

	assert.Equal(t, "KAITO_USDT", td.Symbol)
	assert.Equal(t, 700000.0, td.AmountUSDT24)
}

func TestContractDataFromExchange(t *testing.T) {
	t.Parallel()

	input := &exchange.ContractDetail{
		Symbol:       "ETH_USDT",
		PriceUnit:    0.01,
		VolUnit:      1,
		MinVol:       1,
		MaxVol:       5000,
		PriceScale:   2,
		VolScale:     0,
		ContractSize: 0.1,
		TakerFeeRate: 0.0006,
		MakerFeeRate: 0.0002,
		MaxLeverage:  50,
	}

	cd := store.ContractDataFromExchange(input)

	assert.Equal(t, "ETH_USDT", cd.Symbol)
	assert.Equal(t, 0.0006, cd.TakerFeeRate)
	assert.Equal(t, 50, cd.MaxLeverage)
}

func TestGetMaxVolForLeverage(t *testing.T) {
	t.Parallel()

	cd := &store.ContractData{
		Symbol:       "KAITO-SWAP-USDT",
		ContractSize: 0.1,
		MaxVol:       100000,
		RiskLimits: []exchange.RiskLimitTier{
			{Level: 1, MaxLeverage: 75, MaxNotional: 34276},
			{Level: 2, MaxLeverage: 20, MaxNotional: 408050},
		},
	}

	// At 20x leverage, price 0.6119: MaxNotional = 408050 -> notionalMaxVol = 408050 / 0.06119 = 6,668,573
	// Capped by single order MaxVol = 100,000
	maxVol20 := cd.GetMaxVolForLeverage(20, 0.6119)
	assert.Equal(t, 100000, maxVol20)

	// At 75x leverage, price 0.6119: MaxNotional = 34276 -> notionalMaxVol = 34276 / 0.06119 = 560,156
	// Capped by single order MaxVol = 100,000
	maxVol75 := cd.GetMaxVolForLeverage(75, 0.6119)
	assert.Equal(t, 100000, maxVol75)

	// With smaller MaxVol single order limit = 50,000
	cdSmall := &store.ContractData{
		Symbol:       "KAITO-SWAP-USDT",
		ContractSize: 0.1,
		MaxVol:       50000,
		RiskLimits: []exchange.RiskLimitTier{
			{Level: 1, MaxLeverage: 75, MaxNotional: 3427}, // 3427 / 0.06119 = 55,997
		},
	}
	assert.Equal(t, 50000, cdSmall.GetMaxVolForLeverage(75, 0.6119))
}
