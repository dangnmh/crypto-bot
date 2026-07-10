package bydfi_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-bot/internal/infrastructure/exchange/bydfi"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_GetPotentialFundingSymbols(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/v1/fapi/market/exchange_info":
			_, _ = w.Write([]byte(`{
				"code": 200,
				"data": [
					{
						"symbol": "BTC-USDT",
						"contractFactor": "0.0001",
						"status": "NORMAL"
					}
				]
			}`))
		case "/api/v1/fapi/market/ticker/24hr":
			_, _ = w.Write([]byte(`{
				"code": 200,
				"data": [
					{
						"symbol": "BTC-USDT",
						"last": "60000.0",
						"vol": "100000.0"
					}
				]
			}`))
		case "/api/v1/fapi/market/funding_rate":
			assert.Equal(t, "BTC-USDT", r.URL.Query().Get("symbol"))
			_, _ = w.Write([]byte(`{
				"code": 200,
				"data": {
					"symbol": "BTC-USDT",
					"lastFundingRate": "0.0001",
					"nextFundingTime": "1783504800000"
				}
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := bydfi.NewClient(server.Client(), server.URL+"/api", slog.Default())

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
	assert.Equal(t, 600000.0, results[0].Volume24h)
}
