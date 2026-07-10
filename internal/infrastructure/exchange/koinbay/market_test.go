package koinbay_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange/koinbay"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_GetPotentialFundingSymbols(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/fapi/v1/contracts":
			_, _ = w.Write([]byte(`[
				{
					"symbol": "E-BTC-USDT",
					"status": 1
				},
				{
					"symbol": "E-ETH-USDT",
					"status": 0
				}
			]`))
		case "/fapi/v1/ticker":
			assert.Equal(t, "E-BTC-USDT", r.URL.Query().Get("contractName"))
			_, _ = w.Write([]byte(`{
				"vol": "1000000.0",
				"last": "60000.0"
			}`))
		case "/fapi/v1/index":
			assert.Equal(t, "E-BTC-USDT", r.URL.Query().Get("contractName"))
			_, _ = w.Write([]byte(`{
				"currentFundRate": "0.0001"
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := koinbay.NewClient(server.Client(), server.URL, config.LoggingConfig{})

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
	assert.Equal(t, 1000000.0, results[0].Volume24h)
}

func TestClient_GetPotentialFundingSymbols_Error(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := koinbay.NewClient(server.Client(), server.URL, config.LoggingConfig{})

	_, err := client.GetPotentialFundingSymbols(
		context.Background(),
		1000,
		0,
		nil,
		nil,
	)
	assert.Error(t, err)
}
