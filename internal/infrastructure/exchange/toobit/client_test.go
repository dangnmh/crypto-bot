package toobit_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange/toobit"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_GetTickers(t *testing.T) {
	t.Parallel()

	t.Run("success all symbols", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/quote/v1/contract/ticker/24hr", r.URL.Path)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[
				{
					"t": 1538725500422,
					"a": "1.1",
					"b": "1.0",
					"s": "BTC-SWAP-USDT",
					"c": "4.0",
					"o": "99.0",
					"h": "100.0",
					"l": "0.1",
					"v": "8913.3",
					"qv": "15.3",
					"pc": "1.0",
					"pcp": "2.0"
				}
			]`))
		}))
		defer server.Close()

		client := toobit.NewClient(server.Client(), server.URL, config.LoggingConfig{})
		tickers, err := client.GetTickers(context.Background(), "")
		require.NoError(t, err)
		require.Len(t, tickers, 1)

		assert.Equal(t, "BTCUSDT", tickers[0].Symbol)
		assert.Equal(t, 4.0, tickers[0].LastPrice)
		assert.Equal(t, 1.0, tickers[0].Bid1)
		assert.Equal(t, 1.1, tickers[0].Ask1)
		assert.Equal(t, 8913.3, tickers[0].Volume24)
		assert.Equal(t, 15.3, tickers[0].AmountUSDT24)
		assert.Equal(t, int64(1538725500422), tickers[0].Timestamp)
	})

	t.Run("success single symbol", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/quote/v1/contract/ticker/24hr", r.URL.Path)
			assert.Equal(t, "BTC-SWAP-USDT", r.URL.Query().Get("symbol"))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[
				{
					"t": 1538725500422,
					"a": "1.1",
					"b": "1.0",
					"s": "BTC-SWAP-USDT",
					"c": "4.0",
					"o": "99.0",
					"h": "100.0",
					"l": "0.1",
					"v": "8913.3",
					"qv": "15.3",
					"pc": "1.0",
					"pcp": "2.0"
				}
			]`))
		}))
		defer server.Close()

		client := toobit.NewClient(server.Client(), server.URL, config.LoggingConfig{})
		tickers, err := client.GetTickers(context.Background(), "BTCUSDT")
		require.NoError(t, err)
		require.Len(t, tickers, 1)
		assert.Equal(t, "BTCUSDT", tickers[0].Symbol)
	})

	t.Run("http error status", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`bad request details`))
		}))
		defer server.Close()

		client := toobit.NewClient(server.Client(), server.URL, config.LoggingConfig{})
		_, err := client.GetTickers(context.Background(), "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "HTTP 400: bad request details")
	})

	t.Run("unmarshal error", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{invalid json`))
		}))
		defer server.Close()

		client := toobit.NewClient(server.Client(), server.URL, config.LoggingConfig{})
		_, err := client.GetTickers(context.Background(), "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unmarshal tickers")
	})
}

func TestClient_GetFundingRates(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/v1/futures/fundingRate", r.URL.Path)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[
				{
					"symbol": "BTC-SWAP-USDT",
					"rate": "0.00180991",
					"period": "8H",
					"nextFundingTime": 1668427200000,
					"interest": "0.0001",
					"fundingRateCap": "0.003",
					"fundingRateFloor": "-0.003"
				},
				{
					"symbol": "ETH-SWAP-USDT",
					"rate": "-0.0005",
					"period": "8H",
					"nextFundingTime": 1668427200000,
					"interest": "0.0001",
					"fundingRateCap": "0.003",
					"fundingRateFloor": "-0.003"
				}
			]`))
		}))
		defer server.Close()

		client := toobit.NewClient(server.Client(), server.URL, config.LoggingConfig{})
		rates, err := client.GetFundingRates(context.Background(), []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"})
		require.NoError(t, err)
		require.Len(t, rates, 2)

		assert.Equal(t, "BTCUSDT", rates[0].Symbol)
		assert.Equal(t, 0.00180991, rates[0].Rate)
		assert.Equal(t, int64(1668427200000), rates[0].SettleTime)

		assert.Equal(t, "ETHUSDT", rates[1].Symbol)
		assert.Equal(t, -0.0005, rates[1].Rate)
		assert.Equal(t, int64(1668427200000), rates[1].SettleTime)
	})

	t.Run("empty symbols", func(t *testing.T) {
		t.Parallel()

		client := toobit.NewClient(nil, "https://api.toobit.com", config.LoggingConfig{})
		rates, err := client.GetFundingRates(context.Background(), nil)
		require.NoError(t, err)
		assert.Nil(t, rates)
	})

	t.Run("unmarshal error", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{invalid json`))
		}))
		defer server.Close()

		client := toobit.NewClient(server.Client(), server.URL, config.LoggingConfig{})
		_, err := client.GetFundingRates(context.Background(), []string{"BTCUSDT"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unmarshal funding rates")
	})
}
