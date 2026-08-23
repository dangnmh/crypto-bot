package futures_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/toobit/futures"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFuturesClient_GetDepth(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/quote/v1/depth", r.URL.Path)
		assert.Equal(t, "BTC-SWAP-USDT", r.URL.Query().Get("symbol"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"time": 1670000000000,
			"bids": [["50000", "2.0"]],
			"asks": [["50001", "1.5"]]
		}`))
	}))
	defer server.Close()

	client := futures.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	ob, err := client.GetDepth(context.Background(), "BTC-SWAP-USDT")
	require.NoError(t, err)
	require.NotNil(t, ob)
	assert.Equal(t, "BTC-SWAP-USDT", ob.Symbol)
	assert.Equal(t, int64(1670000000000), ob.Version)
	require.Len(t, ob.Bids, 1)
	require.Len(t, ob.Asks, 1)
}

func TestFuturesClient_GetTopGainer(t *testing.T) {
	t.Parallel()
	now := time.Now().UnixMilli()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/quote/v1/contract/ticker/24hr", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `[{
			"s": "BTC-SWAP-USDT",
			"c": "50000",
			"b": "49999",
			"a": "50001",
			"qv": "1000000",
			"pcp": "5.0",
			"t": %d
		}]`, now)
	}))
	defer server.Close()

	client := futures.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	gainers, err := client.GetTopGainer(context.Background(), exchange.TopGainerRequest{})
	require.NoError(t, err)
	require.Len(t, gainers, 1)
	assert.Equal(t, "BTC-SWAP-USDT", gainers[0].Symbol)
	assert.Equal(t, 5.0, gainers[0].Gain24hPct)
}

func TestFuturesClient_OrderAndPosition(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v2/futures/order" && r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{
				"code": "200",
				"data": {
					"orderId": "toobit-123",
					"clientOrderId": "client-123"
				}
			}`))
			return
		}
		if r.URL.Path == "/api/v1/futures/positions" {
			_, _ = w.Write([]byte(`{
				"code": "200",
				"data": [{
					"symbol": "BTC-SWAP-USDT",
					"side": "LONG",
					"position": "1.0",
					"avgPrice": "50000.0",
					"unrealizedPnl": "10.0",
					"leverage": "10"
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

	client := futures.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	client.SetClock(exchange.RealClock{})

	res, err := client.CreateOrder(context.Background(), exchange.SubmitOrderRequest{
		Symbol: "BTC-SWAP-USDT",
		Price:  50000,
		Vol:    1,
		Side:   exchange.SideOpenLong,
		Type:   exchange.OrderTypeLimit,
	})
	require.NoError(t, err)
	assert.Equal(t, "toobit-123", res.OrderID)

	positions, err := client.GetOpenPositions(context.Background(), "BTC-SWAP-USDT")
	require.NoError(t, err)
	require.Len(t, positions, 1)
	assert.Equal(t, 1.0, positions[0].HoldVolContract)
	assert.Equal(t, 50000.0, positions[0].HoldAvgPrice)

	st, err := client.GetServerTime(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1670000000000), st)

	err = client.Ping(context.Background())
	require.NoError(t, err)

	client.WarmUp(context.Background(), 10*time.Millisecond)
}
