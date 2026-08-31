package futures_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/mexc/futures"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFuturesClient_GetDepth(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/contract/depth/BTC_USDT", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"success": true,
			"code": 0,
			"data": {
				"asks": [["50000.5", "1.2"]],
				"bids": [["49999.5", "2.5"]],
				"version": 123456
			}
		}`))
	}))
	defer server.Close()

	client := futures.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	assert.True(t, client.IsFutures())

	depth, err := client.GetDepth(context.Background(), "BTC_USDT")
	require.NoError(t, err)
	require.NotNil(t, depth)
	assert.Equal(t, "BTC_USDT", depth.Symbol)
	assert.Equal(t, int64(123456), depth.Version)
	require.Len(t, depth.Bids, 1)
	assert.Equal(t, 49999.5, depth.Bids[0].Price)
	assert.Equal(t, 2.5, depth.Bids[0].Volume)
}

func TestFuturesClient_GetTopGainer(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/contract/ticker", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"success": true,
			"code": 0,
			"data": [
				{
					"symbol": "BTC_USDT",
					"lastPrice": 50000.0,
					"bid1": 49999.0,
					"ask1": 50001.0,
					"volume24": 100.0,
					"amount24": 5000000.0,
					"riseFallRate": 0.05,
					"timestamp": 1670000000000
				}
			]
		}`))
	}))
	defer server.Close()

	client := futures.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	gainers, err := client.GetTopGainer(context.Background(), exchange.TopGainerRequest{Limit: 10})
	require.NoError(t, err)
	require.Len(t, gainers, 1)
	assert.Equal(t, "BTC_USDT", gainers[0].Symbol)
	assert.Equal(t, 5.0, gainers[0].Gain24hPct)
}

func TestFuturesClient_OrderAndPosition(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/private/order/create" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success": true, "code": 0, "data": {"orderId": "12345", "ts": 1670000000000}}`))
			return
		}
		if r.URL.Path == "/api/v1/private/position/open_positions" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success": true, "code": 0, "data": [{"positionId": 99, "symbol": "BTC_USDT", "holdVol": 1.0, "holdAvgPrice": 50000.0}]}`))
			return
		}
		if r.URL.Path == "/api/v1/contract/ping" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success": true, "code": 0, "data": 1670000000000}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := futures.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	client.SetClock(exchange.RealClock{})

	res, err := client.CreateOrder(context.Background(), exchange.SubmitOrderRequest{
		Symbol: "BTC_USDT",
		Price:  50000,
		Vol:    1,
		Side:   exchange.SideOpenLong,
		Type:   exchange.OrderTypeLimit,
	})
	require.NoError(t, err)
	assert.Equal(t, "12345", res.OrderID)

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

func TestFuturesClient_GetFundingRates(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/contract/funding_rate", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"success": true,
			"code": 0,
			"data": [
				{
					"symbol": "BTC_USDT",
					"fundingRate": 0.0001,
					"nextSettleTime": 1788192000000
				},
				{
					"symbol": "ETH_USDT",
					"fundingRate": -0.0005,
					"nextSettleTime": 1788192000000
				}
			]
		}`))
	}))
	defer server.Close()

	client := futures.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	rates, err := client.GetFundingRates(context.Background(), []string{"BTC_USDT", "ETH_USDT"})
	require.NoError(t, err)
	require.Len(t, rates, 2)
	assert.Equal(t, "BTC_USDT", rates[0].Symbol)
	assert.Equal(t, 0.0001, rates[0].Rate)
	assert.Equal(t, int64(1788192000000), rates[0].SettleTime)
	assert.Equal(t, "ETH_USDT", rates[1].Symbol)
	assert.Equal(t, -0.0005, rates[1].Rate)
	assert.Equal(t, int64(1788192000000), rates[1].SettleTime)
}

func TestFuturesClient_GetPotentialFundingSymbols(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/contract/ticker" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"success": true,
				"code": 0,
				"data": [
					{
						"symbol": "BTC_USDT",
						"lastPrice": 50000.0,
						"amount24": 5000000.0,
						"fundingRate": 0.0001
					},
					{
						"symbol": "ZORA_USDT",
						"lastPrice": 0.01,
						"amount24": 2000000.0,
						"fundingRate": -0.006
					}
				]
			}`))
			return
		}
		if r.URL.Path == "/api/v1/contract/funding_rate" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"success": true,
				"code": 0,
				"data": [
					{
						"symbol": "BTC_USDT",
						"fundingRate": 0.0001,
						"nextSettleTime": 1788192000000
					},
					{
						"symbol": "ZORA_USDT",
						"fundingRate": -0.0061,
						"nextSettleTime": 1788192000000
					}
				]
			}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := futures.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	res, err := client.GetPotentialFundingSymbols(context.Background(), 1000000.0, 0, nil, nil)
	require.NoError(t, err)
	require.Len(t, res, 2)

	assert.Equal(t, "BTC_USDT", res[0].Symbol)
	assert.Equal(t, 0.0001, res[0].Rate)
	assert.Equal(t, int64(1788192000000), res[0].SettleTime)

	assert.Equal(t, "ZORA_USDT", res[1].Symbol)
	assert.Equal(t, -0.0061, res[1].Rate)
	assert.Equal(t, int64(1788192000000), res[1].SettleTime)
}
