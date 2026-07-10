package lbank_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange/lbank"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_GetPotentialFundingSymbols(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/cfd/openApi/v1/pub/marketData", r.URL.Path)
		assert.Equal(t, "SwapU", r.URL.Query().Get("productGroup"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"error_code": 0,
			"msg": "success",
			"data": [
				{
					"symbol": "BTCUSDT",
					"lastPrice": "60000.0",
					"fundingRate": "0.0001",
					"nextFeeTime": 1783504800000,
					"turnover": "1000000.0"
				},
				{
					"symbol": "ETHUSDT",
					"lastPrice": "3000.0",
					"fundingRate": "0.0002",
					"nextFeeTime": 1783504800000,
					"turnover": "500000.0"
				},
				{
					"symbol": "INVALID",
					"lastPrice": "0",
					"fundingRate": "0",
					"nextFeeTime": 0,
					"turnover": "0"
				}
			]
		}`))
	}))
	defer server.Close()

	client := lbank.NewClient(server.Client(), server.URL, config.LoggingConfig{})

	results, err := client.GetPotentialFundingSymbols(
		context.Background(),
		1000,
		0,
		nil,
		nil,
	)
	require.NoError(t, err)
	require.Len(t, results, 2)

	assert.Equal(t, "BTCUSDT", results[0].Symbol)
	assert.Equal(t, 60000.0, results[0].Price)
	assert.Equal(t, 0.0001, results[0].Rate)
	assert.Equal(t, int64(1783504800000), results[0].SettleTime)
	assert.Equal(t, 1000000.0, results[0].Volume24h)
}

func TestClient_GetPotentialFundingSymbols_Error(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": false,
			"error_code": 10002,
			"msg": "invalid group"
		}`))
	}))
	defer server.Close()

	client := lbank.NewClient(server.Client(), server.URL, config.LoggingConfig{})

	_, err := client.GetPotentialFundingSymbols(
		context.Background(),
		1000,
		0,
		nil,
		nil,
	)
	assert.Error(t, err)
}
