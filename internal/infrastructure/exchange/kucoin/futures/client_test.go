package futures_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/kucoin/futures"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFuturesClient_GetDepth(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/level2/snapshot", r.URL.Path)
		assert.Equal(t, "BTC_USDT", r.URL.Query().Get("symbol"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "200000",
			"data": {
				"sequence": 100,
				"symbol": "BTC_USDT",
				"bids": [["50000", "2.0"]],
				"asks": [["50001", "1.5"]]
			}
		}`))
	}))
	defer server.Close()

	client := futures.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	ob, err := client.GetDepth(context.Background(), "BTC_USDT")
	require.NoError(t, err)
	require.NotNil(t, ob)
	assert.Equal(t, "BTC_USDT", ob.Symbol)
	assert.Equal(t, int64(100), ob.Version)
	require.Len(t, ob.Bids, 1)
	require.Len(t, ob.Asks, 1)
}

func TestFuturesClient_GetTopGainer(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/contracts/active" {
			_, _ = w.Write([]byte(`{
				"code": "200000",
				"data": [{
					"symbol": "BTC_USDT",
					"status": "Open",
					"turnoverOf24h": 1000000,
					"lastTradePrice": 50000,
					"priceChgPct": 0.05
				}]
			}`))
			return
		}
		if r.URL.Path == "/api/v1/allTickers" {
			_, _ = w.Write([]byte(`{
				"code": "200000",
				"data": [{
					"symbol": "BTC_USDT",
					"lastPrice": "50000",
					"bestBidPrice": "49999",
					"bestAskPrice": "50001"
				}]
			}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := futures.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	gainers, err := client.GetTopGainer(context.Background(), exchange.TopGainerRequest{})
	require.NoError(t, err)
	require.Len(t, gainers, 1)
	assert.Equal(t, "BTC_USDT", gainers[0].Symbol)
	assert.Equal(t, 5.0, gainers[0].Gain24hPct)
}

func TestFuturesClient_OrderAndPosition(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/orders" && r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{
				"code": "200000",
				"data": {
					"orderId": "ord-123",
					"clientOid": "client-123"
				}
			}`))
			return
		}
		if r.URL.Path == "/api/v1/positions" {
			_, _ = w.Write([]byte(`{
				"code": "200000",
				"data": [{
					"symbol": "BTC_USDT",
					"currentQty": 1.0,
					"avgEntryPrice": 50000.0,
					"realisedPnl": 10.0,
					"realLeverage": 10.0,
					"isOpen": true
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

	client := futures.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	client.SetClock(exchange.RealClock{})

	res, err := client.CreateOrder(context.Background(), exchange.SubmitOrderRequest{
		Symbol: "BTC_USDT",
		Price:  50000,
		Vol:    1,
		Side:   exchange.SideOpenLong,
		Type:   exchange.OrderTypeLimit,
	})
	require.NoError(t, err)
	assert.Equal(t, "ord-123", res.OrderID)

	positions, err := client.GetOpenPositions(context.Background(), "BTC_USDT")
	require.NoError(t, err)
	require.Len(t, positions, 1)
	assert.Equal(t, 1.0, positions[0].HoldVolContract)
	assert.Equal(t, 50000.0, positions[0].HoldAvgPrice)

	st, err := client.GetServerTime(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1670000000000), st)

	err = client.Ping(context.Background())
	require.NoError(t, err)

	warmCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	client.WarmUp(warmCtx, 10*time.Millisecond)
}
