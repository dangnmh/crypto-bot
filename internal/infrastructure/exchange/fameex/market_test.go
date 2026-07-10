package fameex_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange/fameex"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_GetPotentialFundingSymbols(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/swap-api/v2/tickers", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{
				"ticker_id": "BTC-USDT",
				"base_currency": "BTC",
				"quote_currency": "USDT",
				"last_price": "60000.0",
				"quote_volume": "1000000.0",
				"product_type": "Perpetual",
				"funding_rate": "0.0001",
				"next_funding_rate_timestam": 1783504800000
			},
			{
				"ticker_id": "ETH-USDT",
				"base_currency": "ETH",
				"quote_currency": "USDT",
				"last_price": "3000.0",
				"quote_volume": "500000.0",
				"product_type": "Spot",
				"funding_rate": "0",
				"next_funding_rate_timestam": 0
			}
		]`))
	}))
	defer server.Close()

	client := fameex.NewClient(server.Client(), server.URL, config.LoggingConfig{})

	results, err := client.GetPotentialFundingSymbols(
		context.Background(),
		1000,
		0,
		nil,
		nil,
	)
	require.NoError(t, err)
	require.Len(t, results, 1)

	assert.Equal(t, "BTCUSDT", results[0].Symbol)
	assert.Equal(t, 60000.0, results[0].Price)
	assert.Equal(t, 0.0001, results[0].Rate)
	assert.Equal(t, int64(1783504800000), results[0].SettleTime)
	assert.Equal(t, 1000000.0, results[0].Volume24h)
}

func TestClient_GetPotentialFundingSymbols_Error(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := fameex.NewClient(server.Client(), server.URL, config.LoggingConfig{})

	_, err := client.GetPotentialFundingSymbols(
		context.Background(),
		1000,
		0,
		nil,
		nil,
	)
	assert.Error(t, err)
}
