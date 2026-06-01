package kucoin_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/kucoin"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_GetServerTime(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v1/timestamp", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "200000",
			"msg": "success",
			"data": 1695812285073
		}`))
	}))
	defer server.Close()

	client := kucoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	ts, err := client.GetServerTime(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1695812285073), ts)
}

func TestClient_GetContractDetails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v1/contracts/active", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "200000",
			"msg": "success",
			"data": [
				{
					"symbol": "XBTUSDTM",
					"baseCurrency": "XBT",
					"quoteCurrency": "USDT",
					"settleCurrency": "USDT",
					"lotSize": 1,
					"tickSize": 0.1,
					"multiplier": 1,
					"status": "Open"
				}
			]
		}`))
	}))
	defer server.Close()

	client := kucoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	details, err := client.GetContractDetails(context.Background())
	require.NoError(t, err)
	require.Len(t, details, 1)
	assert.Equal(t, "XBTUSDTM", details[0].Symbol)
	assert.Equal(t, "XBT", details[0].BaseCoin)
	assert.Equal(t, 1, details[0].PriceScale)
	assert.Equal(t, 0.1, details[0].PriceUnit)
}

func TestClient_GetTickers(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/allTickers":
			_, _ = w.Write([]byte(`{
				"code": "200000",
				"msg": "success",
				"data": [
					{
						"symbol": "XBTUSDTM",
						"bestBidPrice": "50000.0",
						"bestAskPrice": "50001.0",
						"price": "50000.5",
						"volume": "1000",
						"ts": 1695812285073
					}
				]
			}`))
		case "/api/v1/contracts/active":
			_, _ = w.Write([]byte(`{
				"code": "200000",
				"msg": "success",
				"data": [
					{
						"symbol": "XBTUSDTM",
						"fundingFeeRate": 0.0001,
						"nextFundingRateDateTime": 1695841085073
					}
				]
			}`))
		}
	}))
	defer server.Close()

	client := kucoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	tickers, err := client.GetTickers(context.Background(), "")
	require.NoError(t, err)
	require.Len(t, tickers, 1)
	assert.Equal(t, "XBTUSDTM", tickers[0].Symbol)
	assert.Equal(t, 50000.5, tickers[0].LastPrice)
	assert.Equal(t, 50000.0, tickers[0].Bid1)
	assert.Equal(t, 50001.0, tickers[0].Ask1)
}

func TestClient_CreateOrder(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/v1/orders", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "200000",
			"msg": "success",
			"data": {
				"orderId": "123456",
				"clientOid": "external_123"
			}
		}`))
	}))
	defer server.Close()

	client := kucoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	res, err := client.CreateOrder(context.Background(), exchange.SubmitOrderRequest{
		Symbol:      "XBTUSDTM",
		Vol:         0.5,
		Side:        exchange.SideOpenLong,
		Type:        exchange.OrderTypeLimit,
		Price:       50000.0,
		ExternalOID: "external_123",
	})
	require.NoError(t, err)
	assert.Equal(t, "123456", res.OrderID)
}

func TestClient_CancelOrder(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DELETE", r.Method)
		assert.Equal(t, "/api/v1/orders/123456", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "200000",
			"msg": "success"
		}`))
	}))
	defer server.Close()

	client := kucoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	err := client.CancelOrder(context.Background(), "XBTUSDTM", "123456")
	require.NoError(t, err)
}

func TestClient_GetOrder(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v1/orders/123456", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "200000",
			"msg": "success",
			"data": {
				"orderId": "123456",
				"symbol": "XBTUSDTM",
				"side": "buy",
				"type": "limit",
				"size": 10,
				"price": "50000.0",
				"status": "done",
				"dealSize": 10,
				"isActive": false,
				"filledValue": "500000.0"
			}
		}`))
	}))
	defer server.Close()

	client := kucoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	info, err := client.GetOrder(context.Background(), "XBTUSDTM", "123456")
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "123456", info.OrderID)
	assert.Equal(t, 50000.0, info.Price)
}

func TestClient_GetOpenOrders(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v1/orders", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "200000",
			"msg": "success",
			"data": {
				"items": [
					{
						"orderId": "123456",
						"symbol": "XBTUSDTM",
						"side": "sell",
						"type": "limit",
						"size": 10,
						"price": "50000.0",
						"status": "active",
						"dealSize": 0,
						"isActive": true
					}
				]
			}
		}`))
	}))
	defer server.Close()

	client := kucoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	orders, err := client.GetOpenOrders(context.Background(), "XBTUSDTM")
	require.NoError(t, err)
	require.Len(t, orders, 1)
	assert.Equal(t, "123456", orders[0].OrderID)
}

func TestClient_GetAssets(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v1/account-overview", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "200000",
			"msg": "success",
			"data": {
				"currency": "USDT",
				"accountEquity": "1000.0",
				"availableBalance": "950.0",
				"unrealisedPNL": "50.0"
			}
		}`))
	}))
	defer server.Close()

	client := kucoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	assets, err := client.GetAssets(context.Background())
	require.NoError(t, err)
	require.Len(t, assets, 1)
	assert.Equal(t, "USDT", assets[0].Currency)
	assert.Equal(t, 1000.0, assets[0].CashBalance)

	asset, err := client.GetAssetByCurrency(context.Background(), "USDT")
	require.NoError(t, err)
	assert.Equal(t, "USDT", asset.Currency)
}

func TestClient_GetOpenPositions(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v1/positions", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "200000",
			"msg": "success",
			"data": [
				{
					"symbol": "XBTUSDTM",
					"currentQty": "-10.0",
					"avgEntryPrice": "50000.0",
					"realisedPNL": "10.0",
					"unrealisedPNL": "5.0",
					"leverage": "10",
					"liquidationPrice": "45000.0"
				}
			]
		}`))
	}))
	defer server.Close()

	client := kucoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	positions, err := client.GetOpenPositions(context.Background(), "XBTUSDTM")
	require.NoError(t, err)
	require.Len(t, positions, 1)
	assert.Equal(t, "XBTUSDTM", positions[0].Symbol)
	assert.Equal(t, 10.0, positions[0].HoldVol)
}

func TestClient_ChangeLeverage(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/v1/position/leverage", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "200000",
			"msg": "success"
		}`))
	}))
	defer server.Close()

	client := kucoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	err := client.ChangeLeverage(context.Background(), exchange.ChangeLeverageRequest{
		Symbol:   "XBTUSDTM",
		Leverage: 10,
	})
	require.NoError(t, err)
}

func TestClient_CancelAllOpenOrders_and_CloseAll(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/orders":
			_, _ = w.Write([]byte(`{
				"code": "200000",
				"msg": "success",
				"data": {
					"items": [
						{
							"orderId": "123456",
							"symbol": "XBTUSDTM",
							"side": "buy",
							"type": "limit",
							"size": 10,
							"price": "50000.0",
							"status": "active",
							"dealSize": 0,
							"isActive": true
						}
					]
				}
			}`))
		case "DELETE /api/v1/orders/123456":
			_, _ = w.Write([]byte(`{
				"code": "200000",
				"msg": "success"
			}`))
		case "GET /api/v1/positions":
			_, _ = w.Write([]byte(`{
				"code": "200000",
				"msg": "success",
				"data": [
					{
						"symbol": "XBTUSDTM",
						"currentQty": "10.0",
						"avgEntryPrice": "50000.0",
						"realisedPNL": "10.0",
						"unrealisedPNL": "5.0",
						"leverage": "10",
						"liquidationPrice": "45000.0"
					}
				]
			}`))
		case "POST /api/v1/orders":
			_, _ = w.Write([]byte(`{
				"code": "200000",
				"msg": "success",
				"data": {
					"orderId": "1234567"
				}
			}`))
		}
	}))
	defer server.Close()

	client := kucoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})

	err := client.CancelAllOpenOrders(context.Background(), "XBTUSDTM")
	require.NoError(t, err)

	err = client.CloseAllPositions(context.Background(), "XBTUSDTM")
	require.NoError(t, err)
}

func TestClient_ErrorPaths(t *testing.T) {
	t.Parallel()

	// 1. CreateTrackOrder unimplemented
	client := kucoin.NewClient(nil, "", "key", "secret", "pass", config.LoggingConfig{})
	_, err := client.CreateTrackOrder(context.Background(), exchange.SubmitTrackOrderRequest{})
	assert.ErrorContains(t, err, "CreateTrackOrder not implemented")

	// 2. CancelOrders unimplemented
	err = client.CancelOrders(context.Background(), []string{"1"})
	assert.ErrorContains(t, err, "batch CancelOrders not implemented")
}

func TestClient_GetFundingRates(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v1/contracts/active", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "200000",
			"data": [
				{
					"symbol": "XBTUSDTM",
					"fundingFeeRate": 0.0001,
					"nextFundingRateDateTime": 1672531200000
				}
			]
		}`))
	}))
	defer server.Close()

	client := kucoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	frs, err := client.GetFundingRates(context.Background(), []string{"XBTUSDTM"})
	require.NoError(t, err)
	require.Len(t, frs, 1)
	assert.Equal(t, "XBTUSDTM", frs[0].Symbol)
	assert.Equal(t, 0.0001, frs[0].Rate)
	assert.Equal(t, int64(1672531200000), frs[0].SettleTime)
}

func TestClient_GetKlines(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Contains(t, r.URL.Path, "/api/v1/kline/query")

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "200000",
			"data": [
				[1672531200000, 50000.0, 50001.0, 49999.0, 50000.5, 10.5]
			]
		}`))
	}))
	defer server.Close()

	client := kucoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	klines, err := client.GetKlines(context.Background(), "XBTUSDTM", "1m", 0, 0)
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
		assert.Contains(t, r.URL.Path, "/api/v1/level2/snapshot")

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "200000",
			"data": {
				"asks": [[50001.0, 1.5]],
				"bids": [[50000.0, 2.0]],
				"ts": 1672531200000
			}
		}`))
	}))
	defer server.Close()

	client := kucoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	ob, err := client.GetDepthSnapshot(context.Background(), "XBTUSDTM", 5)
	require.NoError(t, err)
	assert.Equal(t, "XBTUSDTM", ob.Symbol)
	require.Len(t, ob.Asks, 1)
	require.Len(t, ob.Bids, 1)
	assert.Equal(t, 50001.0, ob.Asks[0].Price)
	assert.Equal(t, 1.5, ob.Asks[0].Volume)
}

func TestClient_GetDepthCommits(t *testing.T) {
	t.Parallel()

	client := kucoin.NewClient(nil, "", "key", "secret", "pass", config.LoggingConfig{})
	_, err := client.GetDepthCommits(context.Background(), "XBTUSDTM", 5)
	assert.ErrorContains(t, err, "GetDepthCommits not supported")
}
