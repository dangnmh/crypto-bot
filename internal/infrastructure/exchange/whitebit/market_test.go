package whitebit_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange/whitebit"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_GetPotentialFundingSymbols(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v4/public/futures", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"message": null,
			"result": [
				{
					"product_type": "perpetual",
					"stock_currency": "BTC",
					"money_currency": "USDT",
					"money_volume": "1000000.0",
					"last_price": "60000.0",
					"funding_rate": "0.0001",
					"next_funding_rate_timestamp": "1783504800000"
				},
				{
					"product_type": "delivery",
					"stock_currency": "ETH",
					"money_currency": "USDT"
				}
			]
		}`))
	}))
	defer server.Close()

	client := whitebit.NewClient(server.Client(), server.URL, config.LoggingConfig{})

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
