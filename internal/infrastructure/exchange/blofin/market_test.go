package blofin_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-bot/internal/infrastructure/exchange/blofin"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_GetPotentialFundingSymbols(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/v1/market/instruments":
			_, _ = w.Write([]byte(`{
				"code": "0",
				"msg": "success",
				"data": [
					{
						"instId": "BTC-USDT",
						"instType": "SWAP"
					},
					{
						"instId": "ETH-USDT",
						"instType": "SPOT"
					}
				]
			}`))
		case "/api/v1/market/tickers":
			_, _ = w.Write([]byte(`{
				"code": "0",
				"msg": "success",
				"data": [
					{
						"instId": "BTC-USDT",
						"last": "60000.0",
						"volCurrency24h": "10.0"
					}
				]
			}`))
		case "/api/v1/market/funding-rate":
			_, _ = w.Write([]byte(`{
				"code": "0",
				"msg": "success",
				"data": [
					{
						"instId": "BTC-USDT",
						"fundingRate": "0.0001",
						"fundingTime": "1783681200000"
					}
				]
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := blofin.NewClient(server.Client(), server.URL, slog.Default())

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
	assert.Equal(t, int64(1783681200000), results[0].SettleTime)
	assert.Equal(t, 600000.0, results[0].Volume24h)
}
