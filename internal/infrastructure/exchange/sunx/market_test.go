package sunx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange/sunx"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSunX_GetPotentialFundingSymbols(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/sapi/v1/public/batch_funding_rate" {
			_, _ = w.Write([]byte(`{
				"status": "ok",
				"data": [
					{
						"funding_rate": "0.0001",
						"contract_code": "BTC-USDT",
						"symbol": "BTC",
						"fee_asset": "USDT",
						"funding_time": "1672531200000"
					},
					{
						"funding_rate": "0.0002",
						"contract_code": "ETH-USDT",
						"symbol": "ETH",
						"fee_asset": "USDT",
						"funding_time": "1672531200000"
					}
				]
			}`))
			return
		}
		if r.URL.Path == "/sapi/v1/market/detail/batch_merged" {
			_, _ = w.Write([]byte(`{
				"status": "ok",
				"ticks": [
					{
						"contract_code": "BTC-USDT",
						"close": "50000",
						"trade_turnover": "1000000"
					},
					{
						"contract_code": "ETH-USDT",
						"close": "3000",
						"trade_turnover": "500000"
					}
				]
			}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := sunx.NewClient(server.Client(), server.URL, config.LoggingConfig{})
	res, err := client.GetPotentialFundingSymbols(context.Background(), 0, 0, nil, nil)
	require.NoError(t, err)
	assert.Len(t, res, 2)

	assert.Equal(t, "BTCUSDT", res[0].Symbol)
	assert.Equal(t, 50000.0, res[0].Price)
	assert.Equal(t, 1000000.0, res[0].Volume24h)
	assert.Equal(t, 0.0001, res[0].Rate)
	assert.Equal(t, int64(1672531200000), res[0].SettleTime)

	resFiltered, err := client.GetPotentialFundingSymbols(context.Background(), 600000, 0, nil, nil)
	require.NoError(t, err)
	assert.Len(t, resFiltered, 1)
	assert.Equal(t, "BTCUSDT", resFiltered[0].Symbol)
}
