package spot_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/toobit/spot"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpotClient_GetDepth(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/quote/v1/depth", r.URL.Path)
		assert.Equal(t, "BTCUSDT", r.URL.Query().Get("symbol"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"time": 1670000000000,
			"bids": [["50000", "2.0"]],
			"asks": [["50001", "1.5"]]
		}`))
	}))
	defer server.Close()

	client := spot.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	ob, err := client.GetDepth(context.Background(), "BTCUSDT")
	require.NoError(t, err)
	require.NotNil(t, ob)
	assert.Equal(t, "BTCUSDT", ob.Symbol)
	assert.Equal(t, int64(1670000000000), ob.Version)
	require.Len(t, ob.Bids, 1)
	require.Len(t, ob.Asks, 1)
}

func TestSpotClient_GetTopGainer(t *testing.T) {
	t.Parallel()
	now := time.Now().UnixMilli()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/quote/v1/ticker/24hr", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `[{
			"s": "BTCUSDT",
			"c": "50000",
			"b": "49999",
			"a": "50001",
			"qv": "1000000",
			"pcp": "5.0",
			"t": %d
		}]`, now)
	}))
	defer server.Close()

	client := spot.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	gainers, err := client.GetTopGainer(context.Background(), exchange.TopGainerRequest{})
	require.NoError(t, err)
	require.Len(t, gainers, 1)
	assert.Equal(t, "BTCUSDT", gainers[0].Symbol)
	assert.Equal(t, 5.0, gainers[0].Gain24hPct)
}

func TestSpotClient_GetContractDetails_And_System(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/exchangeInfo" {
			_, _ = w.Write([]byte(`{
				"symbols": [{
					"symbol": "BTCUSDT",
					"baseAsset": "BTC",
					"quoteAsset": "USDT",
					"filters": [{
						"filterType": "PRICE_FILTER",
						"tickSize": "0.01"
					}]
				}]
			}`))
			return
		}
		if r.URL.Path == "/api/v1/time" {
			_, _ = w.Write([]byte(`{"serverTime": 1670000000000}`))
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
	assert.Equal(t, 1, details[0].State)

	st, err := client.GetServerTime(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1670000000000), st)

	err = client.Ping(context.Background())
	require.NoError(t, err)
}
