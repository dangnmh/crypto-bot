package spot_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/kucoin/spot"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpotClient_GetDepth(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/market/orderbook/level2_100", r.URL.Path)
		assert.Equal(t, "BTC-USDT", r.URL.Query().Get("symbol"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "200000",
			"data": {
				"sequence": "100",
				"time": 1670000000000,
				"bids": [["50000", "2.0"]],
				"asks": [["50001", "1.5"]]
			}
		}`))
	}))
	defer server.Close()

	client := spot.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	ob, err := client.GetDepth(context.Background(), "BTC-USDT")
	require.NoError(t, err)
	require.NotNil(t, ob)
	assert.Equal(t, "BTC-USDT", ob.Symbol)
	assert.Equal(t, int64(100), ob.Version)
	require.Len(t, ob.Bids, 1)
	require.Len(t, ob.Asks, 1)
}

func TestSpotClient_GetTopGainer(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/market/allTickers", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "200000",
			"data": {
				"time": 1670000000000,
				"ticker": [{
					"symbol": "BTC-USDT",
					"symbolName": "BTC-USDT",
					"buy": "49999",
					"sell": "50001",
					"changeRate": "0.05",
					"volValue": "1000000",
					"last": "50000"
				}]
			}
		}`))
	}))
	defer server.Close()

	client := spot.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	gainers, err := client.GetTopGainer(context.Background(), exchange.TopGainerRequest{})
	require.NoError(t, err)
	require.Len(t, gainers, 1)
	assert.Equal(t, "BTC-USDT", gainers[0].Symbol)
	assert.Equal(t, 5.0, gainers[0].Gain24hPct)
}

func TestSpotClient_GetContractDetails_And_System(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v2/symbols" {
			_, _ = w.Write([]byte(`{
				"code": "200000",
				"data": [{
					"symbol": "BTC-USDT",
					"name": "BTC-USDT",
					"baseCurrency": "BTC",
					"quoteCurrency": "USDT",
					"priceIncrement": "0.01",
					"enableTrading": true
				}]
			}`))
			return
		}
		if r.URL.Path == "/api/v1/timestamp" {
			_, _ = w.Write([]byte(`{"code": "200000", "data": 1670000000000}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := spot.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	details, err := client.GetContractDetails(context.Background())
	require.NoError(t, err)
	require.Len(t, details, 1)
	assert.Equal(t, "BTC-USDT", details[0].Symbol)
	assert.Equal(t, 1, details[0].State)

	st, err := client.GetServerTime(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1670000000000), st)

	err = client.Ping(context.Background())
	require.NoError(t, err)
}
