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
		Symbol:    "BTC_USDT",
		Volume24:  5000,
		Amount24:  300000000,
		LastPrice: 65000,
		Bid1:      64999,
		Ask1:      65001,
	}

	td := store.TickerDataFromExchange(input)

	assert.Equal(t, "BTC_USDT", td.Symbol)
	assert.Equal(t, 64999.0, td.BestBid)
	assert.Equal(t, 65001.0, td.BestAsk)
	assert.False(t, td.UpdatedAt.IsZero())
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
