package bitget_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/bitget"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_GetServerTime(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v2/public/time", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "00000",
			"msg": "success",
			"data": "1695812285073"
		}`))
	}))
	defer server.Close()

	client := bitget.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	ts, err := client.GetServerTime(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1695812285073), ts)
}

func TestClient_GetContractDetails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v2/mix/market/contracts", r.URL.Path)
		assert.Equal(t, "USDT-FUTURES", r.URL.Query().Get("productType"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "00000",
			"msg": "success",
			"data": [
				{
					"symbol": "BTCUSDT",
					"baseCoin": "BTC",
					"quoteCoin": "USDT",
					"settleCoin": "USDT",
					"symbolStatus": "online",
					"pricePlace": "1",
					"volumePlace": "3",
					"minTradeNum": "0.001",
					"priceEndStep": "0.1"
				}
			]
		}`))
	}))
	defer server.Close()

	client := bitget.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	details, err := client.GetContractDetails(context.Background())
	require.NoError(t, err)
	require.Len(t, details, 1)
	assert.Equal(t, "BTCUSDT", details[0].Symbol)
	assert.Equal(t, "BTC", details[0].BaseCoin)
	assert.Equal(t, 1, details[0].PriceScale)
	assert.Equal(t, 3, details[0].VolScale)
	assert.Equal(t, 0.1, details[0].PriceUnit)
}

func TestClient_GetTickers(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v2/mix/market/tickers", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "00000",
			"msg": "success",
			"data": [
				{
					"symbol": "BTCUSDT",
					"lastPr": "50000.5",
					"bidPr": "50000.0",
					"askPr": "50001.0",
					"baseVolume": "1000",
					"quoteVolume": "50000000",
					"ts": "1695812285073",
					"fundingRate": "0.0001"
				}
			]
		}`))
	}))
	defer server.Close()

	client := bitget.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	tickers, err := client.GetTickers(context.Background(), "")
	require.NoError(t, err)
	require.Len(t, tickers, 1)
	assert.Equal(t, "BTCUSDT", tickers[0].Symbol)
	assert.Equal(t, 50000.5, tickers[0].LastPrice)
	assert.Equal(t, 0.0001, tickers[0].FundingRate)
}

func TestClient_GetFundingRate(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v2/mix/market/current-fund-rate", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "00000",
			"msg": "success",
			"data": [
				{
					"symbol": "BTCUSDT",
					"fundingRate": "0.00015",
					"nextUpdate": "1695862400000"
				}
			]
		}`))
	}))
	defer server.Close()

	client := bitget.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	fr, err := client.GetFundingRate(context.Background(), "BTCUSDT")
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", fr.Symbol)
	assert.Equal(t, 0.00015, fr.FundingRate)
	assert.Equal(t, int64(1695862400000), fr.NextSettleTime)
}

func TestClient_CreateOrder(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/v2/mix/order/place-order", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "00000",
			"msg": "success",
			"data": {
				"orderId": "order123",
				"clientOid": "client123"
			}
		}`))
	}))
	defer server.Close()

	client := bitget.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	req := exchange.SubmitOrderRequest{
		Symbol:       "BTCUSDT",
		Vol:          1.0,
		Price:        50000.0,
		Side:         exchange.SideOpenLong,
		Type:         exchange.OrderTypeLimit,
		PositionMode: 1,
	}
	orderID, err := client.CreateOrder(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "order123", orderID)
}

func TestClient_CancelOrder(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/v2/mix/order/cancel-order", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "00000",
			"msg": "success",
			"data": {}
		}`))
	}))
	defer server.Close()

	client := bitget.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	err := client.CancelOrder(context.Background(), "BTCUSDT", "order123")
	require.NoError(t, err)
}

func TestClient_GetOpenPositions(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v2/mix/position/all-position", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "00000",
			"msg": "success",
			"data": [
				{
					"symbol": "BTCUSDT",
					"holdSide": "long",
					"marginMode": "crossed",
					"leverage": "20",
					"total": "0.5",
					"openPriceAvg": "48000.0",
					"liquidationPrice": "38000.0",
					"achievedProfits": "150.0",
					"marginSize": "1200.0"
				}
			]
		}`))
	}))
	defer server.Close()

	client := bitget.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	positions, err := client.GetOpenPositions(context.Background(), "BTCUSDT")
	require.NoError(t, err)
	require.Len(t, positions, 1)
	assert.Equal(t, "BTCUSDT", positions[0].Symbol)
	assert.Equal(t, 0.5, positions[0].HoldVol)
	assert.Equal(t, 20, positions[0].Leverage)
	assert.Equal(t, 48000.0, positions[0].HoldAvgPrice)
	assert.Equal(t, 2, positions[0].OpenType)     // crossed
	assert.Equal(t, 1, positions[0].PositionType) // long
}
