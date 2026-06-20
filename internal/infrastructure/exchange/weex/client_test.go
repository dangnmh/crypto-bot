package weex_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange/weex"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_GetTickers(t *testing.T) {
	t.Parallel()

	t.Run("success all symbols", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/capi/v3/market/ticker/24hr", r.URL.Path)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[
				{
					"symbol": "BTCUSDT",
					"priceChange": "150.5",
					"priceChangePercent": "0.22",
					"lastPrice": "69350.5",
					"openPrice": "69200.0",
					"highPrice": "69980.0",
					"lowPrice": "68888.0",
					"volume": "1234.567",
					"quoteVolume": "85679012.45",
					"markPrice": "69348.7",
					"indexPrice": "69347.9",
					"openTime": 1764419370000,
					"closeTime": 1764505770000
				}
			]`))
		}))
		defer server.Close()

		client := weex.NewClient(server.Client(), server.URL, config.LoggingConfig{})
		tickers, err := client.GetTickers(context.Background(), "")
		require.NoError(t, err)
		require.Len(t, tickers, 1)

		assert.Equal(t, "BTCUSDT", tickers[0].Symbol)
		assert.Equal(t, 69350.5, tickers[0].LastPrice)
		assert.Equal(t, 1234.567, tickers[0].Volume24)
		assert.Equal(t, 85679012.45, tickers[0].AmountUSDT24)
		assert.Equal(t, int64(1764505770000), tickers[0].Timestamp)
	})

	t.Run("success single symbol", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/capi/v3/market/ticker/24hr", r.URL.Path)
			assert.Equal(t, "BTCUSDT", r.URL.Query().Get("symbol"))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[
				{
					"symbol": "BTCUSDT",
					"priceChange": "150.5",
					"priceChangePercent": "0.22",
					"lastPrice": "69350.5",
					"openPrice": "69200.0",
					"highPrice": "69980.0",
					"lowPrice": "68888.0",
					"volume": "1234.567",
					"quoteVolume": "85679012.45",
					"markPrice": "69348.7",
					"indexPrice": "69347.9",
					"openTime": 1764419370000,
					"closeTime": 1764505770000
				}
			]`))
		}))
		defer server.Close()

		client := weex.NewClient(server.Client(), server.URL, config.LoggingConfig{})
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
			assert.Equal(t, "/capi/v3/market/premiumIndex", r.URL.Path)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[
				{
					"symbol": "BTCUSDT",
					"markPrice": "69348.6",
					"indexPrice": "69347.9",
					"lastFundingRate": "0.00025",
					"forecastFundingRate": "0.00025",
					"interestRate": "0.0001",
					"nextFundingTime": 1764510000000,
					"time": 1764505777345,
					"collectCycle": 480
				}
			]`))
		}))
		defer server.Close()

		client := weex.NewClient(server.Client(), server.URL, config.LoggingConfig{})
		rates, err := client.GetFundingRates(context.Background(), []string{"BTCUSDT"})
		require.NoError(t, err)
		require.Len(t, rates, 1)

		assert.Equal(t, "BTCUSDT", rates[0].Symbol)
		assert.Equal(t, 0.00025, rates[0].Rate)
		assert.Equal(t, int64(1764510000000), rates[0].SettleTime)
	})

	t.Run("empty symbols", func(t *testing.T) {
		t.Parallel()

		client := weex.NewClient(nil, "https://api-contract.weex.com", config.LoggingConfig{})
		rates, err := client.GetFundingRates(context.Background(), nil)
		require.NoError(t, err)
		assert.Nil(t, rates)
	})
}
