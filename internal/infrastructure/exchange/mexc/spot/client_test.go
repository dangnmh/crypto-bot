package spot_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/mexc/spot"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpotClient_GetDepth(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v3/depth", r.URL.Path)
		assert.Equal(t, "BTCUSDT", r.URL.Query().Get("symbol"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"lastUpdateId": 99999,
			"bids": [["50000.00", "1.50"]],
			"asks": [["50001.00", "2.00"]]
		}`))
	}))
	defer server.Close()

	client := spot.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	assert.False(t, client.IsFutures())

	depth, err := client.GetDepth(context.Background(), "BTCUSDT")
	require.NoError(t, err)
	require.NotNil(t, depth)
	assert.Equal(t, "BTCUSDT", depth.Symbol)
	assert.Equal(t, int64(99999), depth.Version)
	require.Len(t, depth.Bids, 1)
	assert.Equal(t, 50000.00, depth.Bids[0].Price)
	assert.Equal(t, 1.50, depth.Bids[0].Volume)
}

func TestSpotClient_GetTopGainer(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v3/ticker/24hr", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[
			{
				"symbol": "BTCUSDT",
				"priceChangePercent": "5.5",
				"lastPrice": "50000.00",
				"bidPrice": "49999.00",
				"askPrice": "50001.00",
				"quoteVolume": "1000000.00",
				"volume": "20.00",
				"closeTime": 1670000000000
			}
		]`))
	}))
	defer server.Close()

	client := spot.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	gainers, err := client.GetTopGainer(context.Background(), exchange.TopGainerRequest{Limit: 10})
	require.NoError(t, err)
	require.Len(t, gainers, 1)
	assert.Equal(t, "BTCUSDT", gainers[0].Symbol)
	assert.Equal(t, 5.5, gainers[0].Gain24hPct)
}

func TestSpotClient_GetContractDetails_And_System(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v3/exchangeInfo" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"symbols": [
					{
						"symbol": "BTCUSDT",
						"status": "1",
						"baseAsset": "BTC",
						"quoteAsset": "USDT",
						"quotePrecision": 2,
						"baseAssetPrecision": 6,
						"filters": [
							{"filterType": "PRICE_FILTER", "tickSize": "0.01"}
						]
					}
				]
			}`))
			return
		}
		if r.URL.Path == "/api/v3/time" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"serverTime": 1670000000000}`))
			return
		}
		if r.URL.Path == "/api/v3/ping" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := spot.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	details, err := client.GetContractDetails(context.Background())
	require.NoError(t, err)
	require.Len(t, details, 1)
	assert.Equal(t, "BTCUSDT", details[0].Symbol)
	assert.Equal(t, 1.0, details[0].ContractSize)

	st, err := client.GetServerTime(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1670000000000), st)

	err = client.Ping(context.Background())
	require.NoError(t, err)
}
