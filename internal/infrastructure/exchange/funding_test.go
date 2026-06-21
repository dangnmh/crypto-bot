package exchange_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange/binance"
	"crypto-bot/internal/infrastructure/exchange/bybit"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBybit_GetPotentialFundingSymbols(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		assert.Contains(t, r.URL.Path, "/v5/market/tickers")

		// Return sample tickers with funding rates and volume
		_, _ = w.Write([]byte(`{
			"retCode": 0,
			"retMsg": "OK",
			"result": {
				"category": "linear",
				"list": [
					{
						"symbol": "BTCUSDT",
						"lastPrice": "50000",
						"volume24h": "1000",
						"turnover24h": "50000000",
						"fundingRate": "0.0001",
						"nextFundingTime": "1672531200000"
					},
					{
						"symbol": "ETHUSDT",
						"lastPrice": "3000",
						"volume24h": "5000",
						"turnover24h": "15000000",
						"fundingRate": "0.0002",
						"nextFundingTime": "1672531200000"
					},
					{
						"symbol": "SOLUSDT",
						"lastPrice": "100",
						"volume24h": "20000",
						"turnover24h": "2000000",
						"fundingRate": "0.0003",
						"nextFundingTime": "1672531200000"
					},
					{
						"symbol": "ADAUSDT",
						"lastPrice": "0.5",
						"volume24h": "100000",
						"turnover24h": "50000",
						"fundingRate": "0.0004",
						"nextFundingTime": "1672531200000"
					}
				]
			}
		}`))
	}))
	t.Cleanup(server.Close)

	client := bybit.NewClient(server.Client(), server.URL, "api_key", "api_secret", "standard", config.LoggingConfig{})

	t.Run("no filters", func(t *testing.T) {
		t.Parallel()
		res, err := client.GetPotentialFundingSymbols(context.Background(), 0, 0, nil, nil)
		require.NoError(t, err)
		assert.Len(t, res, 4)
		assert.Equal(t, "BTCUSDT", res[0].Symbol)
		assert.Equal(t, 50000000.0, res[0].Volume24h)
		assert.Equal(t, 0.0001, res[0].Rate)
		assert.Equal(t, int64(1672531200000), res[0].SettleTime)
	})

	t.Run("filter min volume", func(t *testing.T) {
		t.Parallel()
		res, err := client.GetPotentialFundingSymbols(context.Background(), 10000000, 0, nil, nil)
		require.NoError(t, err)
		assert.Len(t, res, 2) // BTC, ETH
		assert.Equal(t, "BTCUSDT", res[0].Symbol)
		assert.Equal(t, "ETHUSDT", res[1].Symbol)
	})

	t.Run("filter max volume", func(t *testing.T) {
		t.Parallel()
		res, err := client.GetPotentialFundingSymbols(context.Background(), 0, 10000000, nil, nil)
		require.NoError(t, err)
		assert.Len(t, res, 2) // SOL, ADA
		assert.Equal(t, "SOLUSDT", res[0].Symbol)
		assert.Equal(t, "ADAUSDT", res[1].Symbol)
	})

	t.Run("filter whitelist", func(t *testing.T) {
		t.Parallel()
		res, err := client.GetPotentialFundingSymbols(context.Background(), 0, 0, []string{"BTCUSDT", "SOLUSDT"}, nil)
		require.NoError(t, err)
		assert.Len(t, res, 2)
		assert.Equal(t, "BTCUSDT", res[0].Symbol)
		assert.Equal(t, "SOLUSDT", res[1].Symbol)
	})

	t.Run("filter blacklist", func(t *testing.T) {
		t.Parallel()
		res, err := client.GetPotentialFundingSymbols(context.Background(), 0, 0, nil, []string{"ETHUSDT", "ADAUSDT"})
		require.NoError(t, err)
		assert.Len(t, res, 2) // BTC, SOL
		assert.Equal(t, "BTCUSDT", res[0].Symbol)
		assert.Equal(t, "SOLUSDT", res[1].Symbol)
	})
}

func TestBinance_GetPotentialFundingSymbols(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/fapi/v1/ticker/24hr":
			_, _ = w.Write([]byte(`[
				{"symbol": "BTCUSDT", "quoteVolume": "50000000"},
				{"symbol": "ETHUSDT", "quoteVolume": "15000000"},
				{"symbol": "SOLUSDT", "quoteVolume": "2000000"},
				{"symbol": "ADAUSDT", "quoteVolume": "50000"}
			]`))
		case "/fapi/v1/premiumIndex":
			_, _ = w.Write([]byte(`[
				{"symbol": "BTCUSDT", "lastFundingRate": "0.0001", "nextFundingTime": 1672531200000},
				{"symbol": "ETHUSDT", "lastFundingRate": "0.0002", "nextFundingTime": 1672531200000},
				{"symbol": "SOLUSDT", "lastFundingRate": "0.0003", "nextFundingTime": 1672531200000},
				{"symbol": "ADAUSDT", "lastFundingRate": "0.0004", "nextFundingTime": 1672531200000}
			]`))
		}
	}))
	t.Cleanup(server.Close)

	client := binance.NewClient(server.Client(), server.URL, "api_key", "api_secret", config.LoggingConfig{})

	t.Run("no filters", func(t *testing.T) {
		t.Parallel()
		res, err := client.GetPotentialFundingSymbols(context.Background(), 0, 0, nil, nil)
		require.NoError(t, err)
		assert.Len(t, res, 4)
		assert.Equal(t, "BTCUSDT", res[0].Symbol)
		assert.Equal(t, 50000000.0, res[0].Volume24h)
		assert.Equal(t, 0.0001, res[0].Rate)
	})

	t.Run("filter combined", func(t *testing.T) {
		t.Parallel()
		res, err := client.GetPotentialFundingSymbols(context.Background(), 1000000, 20000000, []string{"ETHUSDT", "SOLUSDT", "ADAUSDT"}, []string{"SOLUSDT"})
		require.NoError(t, err)
		assert.Len(t, res, 1) // Only ETHUSDT matches volume limits and is not blacklisted
		assert.Equal(t, "ETHUSDT", res[0].Symbol)
		assert.Equal(t, 15000000.0, res[0].Volume24h)
	})
}
