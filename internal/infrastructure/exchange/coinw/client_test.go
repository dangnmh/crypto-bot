package coinw_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange/coinw"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_GetTickers(t *testing.T) {
	t.Parallel()

	t.Run("success all symbols", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/v1/perpumPublic/tickers", r.URL.Path)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"code": 0,
				"data": [
					{
						"fair_price": 64195.1,
						"max_leverage": 125,
						"total_volume": 0.045,
						"price_coin": "btc",
						"contract_id": 1,
						"base_coin": "btc",
						"high": 64565.7,
						"rise_fall_rate": 0.007604,
						"low": 63150,
						"name": "BTCUSDT",
						"contract_size": 0.001,
						"quote_coin": "usdt",
						"last_price": 64191,
						"ts": 1782035864184
					}
				],
				"msg": ""
			}`))
		}))
		defer server.Close()

		client := coinw.NewClient(server.Client(), server.URL, config.LoggingConfig{})
		tickers, err := client.GetTickers(context.Background(), "")
		require.NoError(t, err)
		require.Len(t, tickers, 1)

		assert.Equal(t, "BTCUSDT", tickers[0].Symbol)
		assert.Equal(t, 64191.0, tickers[0].LastPrice)
		assert.Equal(t, 0.045, tickers[0].Volume24)
		assert.Equal(t, 0.045*64191.0, tickers[0].AmountUSDT24)
		assert.Equal(t, int64(1782035864184), tickers[0].Timestamp)
	})

	t.Run("success filtered symbol", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/v1/perpumPublic/tickers", r.URL.Path)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"code": 0,
				"data": [
					{
						"fair_price": 64195.1,
						"max_leverage": 125,
						"total_volume": 0.045,
						"price_coin": "btc",
						"contract_id": 1,
						"base_coin": "btc",
						"high": 64565.7,
						"rise_fall_rate": 0.007604,
						"low": 63150,
						"name": "BTCUSDT",
						"contract_size": 0.001,
						"quote_coin": "usdt",
						"last_price": 64191,
						"ts": 1782035864184
					},
					{
						"fair_price": 1732.16,
						"max_leverage": 100,
						"total_volume": 0.18,
						"price_coin": "eth",
						"contract_id": 2,
						"base_coin": "eth",
						"high": 1749,
						"rise_fall_rate": 0.002599,
						"low": 1707.07,
						"name": "ETHUSDT",
						"contract_size": 0.01,
						"quote_coin": "usdt",
						"last_price": 1732.16,
						"ts": 1782035864183
					}
				],
				"msg": ""
			}`))
		}))
		defer server.Close()

		client := coinw.NewClient(server.Client(), server.URL, config.LoggingConfig{})
		tickers, err := client.GetTickers(context.Background(), "BTCUSDT")
		require.NoError(t, err)
		require.Len(t, tickers, 1)
		assert.Equal(t, "BTCUSDT", tickers[0].Symbol)
	})
}

func TestClient_GetFundingRates(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/v1/perpum/instruments", r.URL.Path)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"code": 0,
				"data": [
					{
						"base": "btc",
						"quote": "usdt",
						"name": "BTC",
						"settledAt": 1740124800000,
						"settlementRate": 0.0004,
						"status": "online"
					},
					{
						"base": "eth",
						"quote": "usdt",
						"name": "ETH",
						"settledAt": 1740124800000,
						"settlementRate": 0.0002,
						"status": "offline"
					}
				],
				"msg": ""
			}`))
		}))
		defer server.Close()

		client := coinw.NewClient(server.Client(), server.URL, config.LoggingConfig{})
		rates, err := client.GetFundingRates(context.Background(), []string{"BTCUSDT", "ETHUSDT"})
		require.NoError(t, err)
		require.Len(t, rates, 1)

		assert.Equal(t, "BTCUSDT", rates[0].Symbol)
		assert.Equal(t, 0.0004, rates[0].Rate)
		assert.Equal(t, int64(1740124800000), rates[0].SettleTime)
	})

	t.Run("empty symbols", func(t *testing.T) {
		t.Parallel()

		client := coinw.NewClient(nil, "https://api.coinw.com", config.LoggingConfig{})
		rates, err := client.GetFundingRates(context.Background(), nil)
		require.NoError(t, err)
		assert.Nil(t, rates)
	})
}
