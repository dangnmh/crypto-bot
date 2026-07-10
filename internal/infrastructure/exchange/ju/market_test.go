package ju_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange/ju"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_GetPotentialFundingSymbols(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/v1/future-u/market/public/cg/contracts", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"BTC-PERPETUAL": {
				"ticker_id": "BTC-PERPETUAL",
				"last_price": "60000.0",
				"base_volume": "16.666666",
				"funding_rate": "0.0001",
				"next_funding_rate_timestamp": 1783504800000
			}
		}`))
	}))
	defer server.Close()

	client := ju.NewClient(server.Client(), server.URL, config.LoggingConfig{})

	results, err := client.GetPotentialFundingSymbols(
		context.Background(),
		1000,
		0,
		nil,
		nil,
	)
	require.NoError(t, err)
	require.Len(t, results, 1)

	assert.Equal(t, "BTC", results[0].Symbol)
	assert.Equal(t, 60000.0, results[0].Price)
	assert.Equal(t, 0.0001, results[0].Rate)
	assert.Equal(t, int64(1783504800000), results[0].SettleTime)
	assert.InDelta(t, 1000000.0, results[0].Volume24h, 0.1)
}

func TestClient_GetPotentialFundingSymbols_Error(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := ju.NewClient(server.Client(), server.URL, config.LoggingConfig{})

	_, err := client.GetPotentialFundingSymbols(
		context.Background(),
		1000,
		0,
		nil,
		nil,
	)
	assert.Error(t, err)
}

func TestClient_GetPotentialFundingSymbols_Wrapped(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/v1/future-u/market/public/cg/contracts", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 200,
			"data": {
				"BTC-PERPETUAL": {
					"ticker_id": "BTC-PERPETUAL",
					"last_price": "60000.0",
					"target_volume": "1000000.0",
					"funding_rate": "0.0001",
					"next_funding_rate_timestamp": 1783504800000
				}
			}
		}`))
	}))
	defer server.Close()

	client := ju.NewClient(server.Client(), server.URL, config.LoggingConfig{})

	results, err := client.GetPotentialFundingSymbols(
		context.Background(),
		1000,
		0,
		nil,
		nil,
	)
	require.NoError(t, err)
	require.Len(t, results, 1)

	assert.Equal(t, "BTC", results[0].Symbol)
	assert.Equal(t, 1000000.0, results[0].Volume24h)
}
