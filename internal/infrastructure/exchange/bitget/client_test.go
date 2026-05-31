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
	res, err := client.CreateOrder(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "order123", res.OrderID)
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
	assert.Equal(t, 48000.0, positions[0].HoldAvgPrice)
	assert.Equal(t, 1, positions[0].PositionType) // long
}

func TestClient_GetAssets(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v2/mix/account/accounts", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "00000",
			"msg": "success",
			"data": [
				{
					"marginCoin": "USDT",
					"locked": "10.0",
					"available": "990.0",
					"accountEquity": "1000.0",
					"unrealizedPL": "50.0"
				}
			]
		}`))
	}))
	defer server.Close()

	client := bitget.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	assets, err := client.GetAssets(context.Background())
	require.NoError(t, err)
	require.Len(t, assets, 1)
	assert.Equal(t, "USDT", assets[0].Currency)
	assert.Equal(t, 1000.0, assets[0].CashBalance)

	asset, err := client.GetAssetByCurrency(context.Background(), "USDT")
	require.NoError(t, err)
	assert.Equal(t, "USDT", asset.Currency)
}

func TestClient_GetOrder_and_GetOpenOrders(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/v2/mix/order/detail":
			_, _ = w.Write([]byte(`{
				"code": "00000",
				"msg": "success",
				"data": {
					"orderId": "order123",
					"clientOid": "client123",
					"symbol": "BTCUSDT",
					"size": "1.0",
					"price": "50000.0",
					"priceAvg": "50000.0",
					"baseVolume": "1.0",
					"state": "filled",
					"side": "buy",
					"posSide": "long",
					"leverage": "20",
					"cTime": "1695812285073",
					"uTime": "1695812285073"
				}
			}`))
		case "/api/v2/mix/order/click-detail":
			_, _ = w.Write([]byte(`{
				"code": "00000",
				"msg": "success",
				"data": {}
			}`))
		case "/api/v2/mix/order/orders-history":
			_, _ = w.Write([]byte(`{
				"code": "00000",
				"msg": "success",
				"data": [
					{
						"orderId": "order123",
						"clientOid": "client123",
						"symbol": "BTCUSDT",
						"size": "1.0",
						"price": "50000.0",
						"priceAvg": "50000.0",
						"baseVolume": "1.0",
						"state": "filled",
						"side": "buy",
						"posSide": "long",
						"leverage": "20",
						"cTime": "1695812285073",
						"uTime": "1695812285073"
					}
				]
			}`))
		case "/api/v2/mix/order/orders-pending":
			_, _ = w.Write([]byte(`{
				"code": "00000",
				"msg": "success",
				"data": [
					{
						"orderId": "order123",
						"clientOid": "client123",
						"symbol": "BTCUSDT",
						"size": "1.0",
						"price": "50000.0",
						"priceAvg": "50000.0",
						"baseVolume": "1.0",
						"state": "filled",
						"side": "buy",
						"posSide": "long",
						"leverage": "20",
						"cTime": "1695812285073",
						"uTime": "1695812285073"
					}
				]
			}`))
		}
	}))
	defer server.Close()

	client := bitget.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	info, err := client.GetOrder(context.Background(), "BTCUSDT", "order123")
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "order123", info.OrderID)

	orders, err := client.GetOpenOrders(context.Background(), "BTCUSDT")
	require.NoError(t, err)
	require.Len(t, orders, 1)
	assert.Equal(t, "order123", orders[0].OrderID)
}

func TestClient_ChangeLeverage(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/v2/mix/account/set-leverage", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "00000",
			"msg": "success",
			"data": {}
		}`))
	}))
	defer server.Close()

	client := bitget.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	err := client.ChangeLeverage(context.Background(), exchange.ChangeLeverageRequest{
		Symbol:   "BTCUSDT",
		Leverage: 10,
	})
	require.NoError(t, err)
}

func TestClient_CancelAllOpenOrders_and_CloseAll(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v2/mix/order/orders-pending":
			_, _ = w.Write([]byte(`{
				"code": "00000",
				"msg": "success",
				"data": [
					{
						"orderId": "order123",
						"clientOid": "client123",
						"symbol": "BTCUSDT",
						"size": "1.0",
						"price": "50000.0",
						"priceAvg": "50000.0",
						"baseVolume": "1.0",
						"state": "filled",
						"side": "buy",
						"posSide": "long",
						"leverage": "20",
						"cTime": "1695812285073",
						"uTime": "1695812285073"
					}
				]
			}`))
		case "POST /api/v2/mix/order/cancel-order":
			_, _ = w.Write([]byte(`{
				"code": "00000",
				"msg": "success",
				"data": {}
			}`))
		case "GET /api/v2/mix/position/all-position":
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
		case "POST /api/v2/mix/order/place-order":
			_, _ = w.Write([]byte(`{
				"code": "00000",
				"msg": "success",
				"data": {
					"orderId": "order1234"
				}
			}`))
		}
	}))
	defer server.Close()

	client := bitget.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})

	err := client.CancelAllOpenOrders(context.Background(), "BTCUSDT")
	require.NoError(t, err)

	err = client.CloseAllPositions(context.Background(), "BTCUSDT")
	require.NoError(t, err)
}

func TestClient_ErrorPaths(t *testing.T) {
	t.Parallel()

	// 1. CreateTrackOrder unimplemented
	client := bitget.NewClient(nil, "", "key", "secret", "pass", config.LoggingConfig{})
	_, err := client.CreateTrackOrder(context.Background(), exchange.SubmitTrackOrderRequest{})
	assert.ErrorContains(t, err, "CreateTrackOrder not implemented")
}

func TestClient_GetKlines(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Contains(t, r.URL.Path, "/api/v2/mix/market/candles")

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "00000",
			"msg": "success",
			"data": [
				["1672531200000", "50000.0", "50001.0", "49999.0", "50000.5", "10", "500000"]
			]
		}`))
	}))
	defer server.Close()

	client := bitget.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	klines, err := client.GetKlines(context.Background(), "BTCUSDT", "1m", 0, 0)
	require.NoError(t, err)
	require.Len(t, klines, 1)
	assert.Equal(t, int64(1672531200000), klines[0].Timestamp)
	assert.Equal(t, 50000.0, klines[0].Open)
	assert.Equal(t, 50000.5, klines[0].Close)
}

func TestClient_GetDepthSnapshot(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Contains(t, r.URL.Path, "/api/v2/mix/market/merge-depth")

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "00000",
			"msg": "success",
			"data": {
				"asks": [[50001.0, 1.5]],
				"bids": [[50000.0, 2.0]],
				"ts": "1672531200000"
			}
		}`))
	}))
	defer server.Close()

	client := bitget.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	ob, err := client.GetDepthSnapshot(context.Background(), "BTCUSDT", 5)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", ob.Symbol)
	require.Len(t, ob.Asks, 1)
	require.Len(t, ob.Bids, 1)
	assert.Equal(t, 50001.0, ob.Asks[0].Price)
	assert.Equal(t, 1.5, ob.Asks[0].Volume)
}

func TestClient_GetDepthCommits(t *testing.T) {
	t.Parallel()

	client := bitget.NewClient(nil, "", "key", "secret", "pass", config.LoggingConfig{})
	_, err := client.GetDepthCommits(context.Background(), "BTCUSDT", 5)
	assert.ErrorContains(t, err, "GetDepthCommits not supported")
}
