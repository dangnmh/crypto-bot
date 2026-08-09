package xt_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/xt"
	pkgws "crypto-bot/pkg/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_Signature(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "my-api-key", r.Header.Get("validate-appkey"))
		assert.NotEmpty(t, r.Header.Get("validate-timestamp"))
		assert.NotEmpty(t, r.Header.Get("validate-signature"))
		assert.Equal(t, "HmacSHA256", r.Header.Get("validate-algorithms"))

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"returnCode":0,"msgInfo":"success","result":"12345678"}`))
	}))
	defer server.Close()

	client := xt.NewClient(server.Client(), server.URL, "my-api-key", "my-api-secret", config.LoggingConfig{})

	res, err := client.CreateOrder(context.Background(), exchange.SubmitOrderRequest{
		Symbol: "BTCUSDT",
		Price:  60000,
		Vol:    0.1,
		Side:   domain.SideOpenLong,
		Type:   domain.OrderTypeLimit,
	})
	require.NoError(t, err)
	assert.Equal(t, "12345678", res.OrderID)
}

func runXTMockServer(routes map[string]string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if res, ok := routes[r.URL.Path]; ok {
			_, _ = w.Write([]byte(res))
		}
	}))
}

func TestClient_GetOrderPNL(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/future/trade/v1/order/detail" {
			// Mock order detail
			_, _ = w.Write([]byte(`{
				"returnCode": 0,
				"msgInfo": "success",
				"result": {
					"orderId": "o999",
					"clientOrderId": "ext999",
					"symbol": "btc_usdt",
					"orderSide": "SELL",
					"positionSide": "LONG",
					"orderType": "LIMIT",
					"price": "60000.0",
					"origQty": "1.5",
					"executedQty": "1.5",
					"avgPrice": "61000.0",
					"state": "FILLED",
					"createdTime": 1782570000000,
					"updateTime": 1782570005000
				}
			}`))
			return
		}

		if r.URL.Path == "/future/trade/v1/position/list-history" {
			// Mock position list history
			_, _ = w.Write([]byte(`{
				"returnCode": 0,
				"msgInfo": "success",
				"result": {
					"hasNext": false,
					"hasPrev": false,
					"items": [
						{
							"id": "1987654321098765432",
							"positionSide": "LONG",
							"contractType": "PERPETUAL",
							"symbol": "BTCUSDT",
							"positionType": 2,
							"closeProfit": "1500.0",
							"closePositionSize": "1.5",
							"closeOpenPrice": "60666.67",
							"closePrice": "61000.0",
							"maxPositionSize": "1.5",
							"openTime": 1782570000000,
							"closeTime": 1782570005000,
							"startLeverage": 5,
							"endLeverage": 5,
							"working": false,
							"force": false,
							"forceMarkPrice": null,
							"totalFee": "4.5",
							"totalFundFee": "-2.5",
							"welfareAccount": false
						}
					]
				}
			}`))
			return
		}

		if r.URL.Path == "/future/user/v1/balance/bills" {
			assert.Equal(t, "1782570000000", r.URL.Query().Get("startTime"))
			assert.Equal(t, "1782570060000", r.URL.Query().Get("endTime"))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"returnCode": 0,
				"msgInfo": "success",
				"result": {
					"hasNext": false,
					"hasPrev": false,
					"items": [
						{
							"afterAmount": 12.6249,
							"amount": -0.2722,
							"coin": "usdt",
							"createdTime": 1782570010000,
							"id": 1234567,
							"side": "SUB",
							"symbol": "btc_usdt",
							"type": "FUND"
						}
					]
				}
			}`))
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := xt.NewClient(server.Client(), server.URL, "my-api-key", "my-api-secret", config.LoggingConfig{})

	pnl, err := client.GetOrderPNL(context.Background(), "BTCUSDT", "o999")
	require.NoError(t, err)

	assert.Equal(t, "xt", pnl.Exchange)
	assert.Equal(t, "BTCUSDT", pnl.Symbol)
	// Entry price weighted: ((60000 * 0.5) + (61000 * 1.0)) / 1.5 = 60666.666...
	assert.InDelta(t, 60666.67, pnl.EntryPrice, 0.01)
	assert.Equal(t, 61000.0, pnl.ExitPrice)
	assert.Equal(t, 1.5, *pnl.ClosedSizeContract)
	assert.Equal(t, 1500.0, pnl.GrossPnL)
	assert.Equal(t, 4.5, pnl.Fee)
	assert.Equal(t, -0.2722, pnl.FundingFee)
	// netPnL = grossPnL + fee + fundingFee = 1500 + 4.5 + (-0.2722) = 1504.2278
	assert.Equal(t, 1504.2278, pnl.NetPnl)
}

func TestClient_GetBalanceBills(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/future/user/v1/balance/bills", r.URL.Path)
		assert.Equal(t, "btc_usdt", r.URL.Query().Get("symbol"))

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"returnCode":0,"msgInfo":"success","result":{"hasNext":false,"hasPrev":false,"items":[]}}`))
	}))
	defer server.Close()

	client := xt.NewClient(server.Client(), server.URL, "my-api-key", "my-api-secret", config.LoggingConfig{})

	res, err := client.GetBalanceBillsRaw(context.Background(), map[string]string{"symbol": "btc_usdt"})
	require.NoError(t, err)
	assert.Contains(t, string(res), `"msgInfo":"success"`)
}

func TestClient_MarketMethods(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/future/market/v1/public/cg/contracts":
			_, _ = w.Write([]byte(`[
				{
					"id": 1,
					"symbol": "btc_usdt",
					"ticker_id": "BTC_USDT",
					"base_currency": "btc",
					"target_currency": "USDT",
					"last_price": "64000",
					"target_volume": "100.5",
					"bid": "63999",
					"ask": "64001",
					"product_type": "PERPETUAL",
					"funding_rate": "0.001",
					"next_funding_rate_timestamp": 1700000000
				}
			]`))
		case "/future/market/v1/public/symbol/list":
			_, _ = w.Write([]byte(`{
				"returnCode": 0,
				"msgInfo": "success",
				"result": [
					{
						"symbol": "btc_usdt",
						"baseCoin": "btc",
						"quoteCoin": "usdt",
						"contractSize": "0.001",
						"pricePrecision": 2,
						"quantityPrecision": 4,
						"minQty": "1",
						"state": 0
					}
				]
			}`))
		}
	}))
	defer server.Close()

	client := xt.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})

	// GetTickers
	tickers, err := client.GetTickers(context.Background(), "")
	require.NoError(t, err)
	require.Len(t, tickers, 1)
	assert.Equal(t, "BTCUSDT", tickers[0].Symbol)

	// GetContractDetails
	details, err := client.GetContractDetails(context.Background())
	require.NoError(t, err)
	require.Len(t, details, 1)
	assert.Equal(t, "BTCUSDT", details[0].Symbol)

	// GetFundingRates
	rates, err := client.GetFundingRates(context.Background(), []string{"BTCUSDT"})
	require.NoError(t, err)
	require.Len(t, rates, 1)
	assert.Equal(t, 0.001, rates[0].Rate)
}

func TestClient_OrderCancellationAndPositions(t *testing.T) {
	t.Parallel()

	server := runXTMockServer(map[string]string{
		"/future/trade/v1/order/cancel":     `{"returnCode":0,"msgInfo":"success"}`,
		"/future/trade/v1/order/cancel-all": `{"returnCode":0,"msgInfo":"success"}`,
		"/future/user/v1/position": `{
				"returnCode": 0,
				"msgInfo": "success",
				"result": [
					{
						"symbol": "btc_usdt",
						"positionSide": "LONG",
						"positionSize": "0.5",
						"entryPrice": "64000",
						"leverage": 10
					}
				]
			}`,
		"/future/trade/v1/position/close-all":      `{"returnCode":0,"msgInfo":"success"}`,
		"/future/user/v1/position/change-type":     `{"returnCode":0,"msgInfo":"success"}`,
		"/future/user/v1/position/adjust-leverage": `{"returnCode":0,"msgInfo":"success"}`,
	})
	defer server.Close()

	client := xt.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})

	// CancelOrder
	err := client.CancelOrder(context.Background(), "BTCUSDT", "123")
	require.NoError(t, err)

	// CancelAllOpenOrders
	err = client.CancelAllOpenOrders(context.Background(), "BTCUSDT")
	require.NoError(t, err)

	// GetOpenPositions
	positions, err := client.GetOpenPositions(context.Background(), "BTCUSDT")
	require.NoError(t, err)
	require.Len(t, positions, 1)
	assert.Equal(t, "BTCUSDT", positions[0].Symbol)

	// CloseAllPositions
	err = client.CloseAllPositions(context.Background(), "BTCUSDT")
	require.NoError(t, err)

	// SwitchMarginMode
	err = client.SwitchMarginMode(context.Background(), "BTCUSDT", domain.MarginModeIsolated, 10, domain.SideOpenLong)
	require.NoError(t, err)

	// ChangeLeverage
	err = client.ChangeLeverage(context.Background(), exchange.ChangeLeverageRequest{Symbol: "BTCUSDT", Leverage: 5})
	require.NoError(t, err)
}

func TestClient_GetPotentialFundingSymbols(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/future/market/v1/public/cg/contracts" {
			_, _ = w.Write([]byte(`[
				{
					"id": 1,
					"symbol": "btc_usdt",
					"ticker_id": "BTC_USDT",
					"base_currency": "btc",
					"target_currency": "USDT",
					"last_price": "64000",
					"target_volume": "15000000",
					"bid": "63999",
					"ask": "64001",
					"product_type": "PERPETUAL",
					"funding_rate": "0.001",
					"next_funding_rate_timestamp": 1700000000
				}
			]`))
		}
	}))
	defer server.Close()

	client := xt.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	res, err := client.GetPotentialFundingSymbols(context.Background(), 10000000, 0, nil, nil)
	require.NoError(t, err)
	require.Len(t, res, 1)
	assert.Equal(t, "BTCUSDT", res[0].Symbol)
	assert.Equal(t, 0.001, res[0].Rate)

	// Call with whitelist containing "BTCUSDT" -> should pass
	res, err = client.GetPotentialFundingSymbols(context.Background(), 10000000, 0, []string{"BTCUSDT"}, nil)
	require.NoError(t, err)
	require.Len(t, res, 1)

	// Call with whitelist NOT containing "BTCUSDT" -> should filter out
	res, err = client.GetPotentialFundingSymbols(context.Background(), 10000000, 0, []string{"ETHUSDT"}, nil)
	require.NoError(t, err)
	require.Len(t, res, 0)

	// Call with blacklist containing "BTCUSDT" -> should filter out
	res, err = client.GetPotentialFundingSymbols(context.Background(), 10000000, 0, nil, []string{"BTCUSDT"})
	require.NoError(t, err)
	require.Len(t, res, 0)

	// Call with maxVol24h (which is less than target_volume 15000000) -> should filter out
	res, err = client.GetPotentialFundingSymbols(context.Background(), 10000000, 5000000, nil, nil)
	require.NoError(t, err)
	require.Len(t, res, 0)
}

func TestClient_SystemAndRemainingOrderMethods(t *testing.T) {
	t.Parallel()

	server := runXTMockServer(map[string]string{
		"/future/market/v1/public/time": `{"returnCode":0,"msgInfo":"success","result":1700000000}`,
		"/future/trade/v1/order/list": `{
				"returnCode": 0,
				"msgInfo": "success",
				"result": [
					{
						"orderId": "o123",
						"clientOrderId": "ext123",
						"symbol": "btc_usdt",
						"orderSide": "BUY",
						"positionSide": "LONG",
						"orderType": "LIMIT",
						"price": "64000",
						"origQty": "0.5",
						"executedQty": "0.0",
						"avgPrice": "0.0",
						"state": "NEW",
						"createdTime": 1700000000000,
						"updateTime": 1700000000000
					}
				]
			}`,
		"/future/trade/v1/order/list-history": `{
				"returnCode": 0,
				"msgInfo": "success",
				"result": {
					"hasNext": false,
					"hasPrev": false,
					"items": [
						{
							"orderId": "o456",
							"clientOrderId": "ext456",
							"symbol": "btc_usdt",
							"orderSide": "SELL",
							"positionSide": "LONG",
							"orderType": "LIMIT",
							"price": "65000",
							"origQty": "0.5",
							"executedQty": "0.5",
							"avgPrice": "65000",
							"state": "FILLED",
							"createdTime": 1700000010000,
							"updateTime": 1700000010000
						}
					]
				}
			}`,
		"/future/market/v1/public/cg/contracts": `[]`,
		"/future/user/v1/position":              `{"returnCode":0,"result":[]}`,
		"/future/trade/v1/order/detail":         `{"returnCode":0,"result":{}}`,
	})
	defer server.Close()

	client := xt.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})

	// GetServerTime
	ts, err := client.GetServerTime(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1700000000), ts)

	// SupportLeverageOnOrder
	assert.False(t, client.SupportLeverageOnOrder())

	// GetOpenOrders
	orders, err := client.GetOpenOrders(context.Background(), "BTCUSDT")
	require.NoError(t, err)
	require.Len(t, orders, 1)
	assert.Equal(t, "o123", orders[0].OrderID)

	// GetHistoryOrders
	histOrders, err := client.GetHistoryOrders(context.Background(), "BTCUSDT", 10)
	require.NoError(t, err)
	require.Len(t, histOrders, 1)
	assert.Equal(t, "o456", histOrders[0].OrderID)

	// GetOrderByExternalID
	order, err := client.GetOrderByExternalID(context.Background(), "BTCUSDT", "ext123")
	require.NoError(t, err)
	assert.Equal(t, "o123", order.OrderID)

	order, err = client.GetOrderByExternalID(context.Background(), "BTCUSDT", "ext456")
	require.NoError(t, err)
	assert.Equal(t, "o456", order.OrderID)

	// Cover raw methods to hit client.go lines
	_, _ = client.GetFundingRateRaw(context.Background(), nil)
	_, _ = client.GetTickersRaw(context.Background(), nil)
	_, _ = client.GetOpenPositionsRaw(context.Background(), nil)
	_, _ = client.GetOrderDetailRaw(context.Background(), "o123", nil)
	_, _ = client.GetHistoryOrdersRaw(context.Background(), nil)
	_, _ = client.GetOrderPNLRaw(context.Background(), nil)

	// WarmUp
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	client.WarmUp(ctx, 10*time.Millisecond)
}

func TestClient_ErrorPaths(t *testing.T) {
	t.Parallel()

	// 1. HTTP Error Status Code
	serverErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer serverErr.Close()

	clientErr := xt.NewClient(serverErr.Client(), serverErr.URL, "key", "secret", config.LoggingConfig{})

	_, err := clientErr.GetTickers(context.Background(), "")
	assert.Error(t, err)

	_, err = clientErr.GetContractDetails(context.Background())
	assert.Error(t, err)

	_, err = clientErr.GetOpenPositions(context.Background(), "BTCUSDT")
	assert.Error(t, err)

	err = clientErr.CancelOrder(context.Background(), "BTCUSDT", "123")
	assert.Error(t, err)

	// 2. API Error Return Code
	serverApiErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"returnCode":1001,"msgInfo":"some api error"}`))
	}))
	defer serverApiErr.Close()

	clientApiErr := xt.NewClient(serverApiErr.Client(), serverApiErr.URL, "key", "secret", config.LoggingConfig{})

	err = clientApiErr.CancelOrder(context.Background(), "BTCUSDT", "123")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "some api error")

	err = clientApiErr.CancelAllOpenOrders(context.Background(), "BTCUSDT")
	assert.Error(t, err)

	_, err = clientApiErr.GetOpenPositions(context.Background(), "BTCUSDT")
	assert.Error(t, err)

	err = clientApiErr.SwitchMarginMode(context.Background(), "BTCUSDT", domain.MarginModeIsolated, 10, domain.SideOpenLong)
	assert.Error(t, err)

	// 3. Invalid JSON Response
	serverInvalidJson := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{invalid-json`))
	}))
	defer serverInvalidJson.Close()

	clientInvalidJson := xt.NewClient(serverInvalidJson.Client(), serverInvalidJson.URL, "key", "secret", config.LoggingConfig{})

	_, err = clientInvalidJson.GetTickers(context.Background(), "")
	assert.Error(t, err)

	_, err = clientInvalidJson.GetContractDetails(context.Background())
	assert.Error(t, err)

	_, err = clientInvalidJson.GetOpenPositions(context.Background(), "BTCUSDT")
	assert.Error(t, err)
}

func TestClient_XTRemainingMethods(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/future/user/v1/user/listen-key":
			_, _ = w.Write([]byte(`{
				"returnCode": 0,
				"msgInfo": "success",
				"result": "test-listen-key"
			}`))
		case "/future/trade/v1/order/list":
			_, _ = w.Write([]byte(`{
				"returnCode": 0,
				"msgInfo": "success",
				"result": [
					{
						"orderId": "order123",
						"symbol": "btc_usdt"
					}
				]
			}`))
		case "/future/trade/v1/order/cancel":
			_, _ = w.Write([]byte(`{
				"returnCode": 0,
				"msgInfo": "success",
				"result": {}
			}`))
		}
	}))
	defer server.Close()

	client := xt.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})

	// 1. GetListenKey
	lk, err := client.GetListenKey(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "test-listen-key", lk)

	// 2. CancelOrders
	err = client.CancelOrders(context.Background(), []string{"order123"})
	require.NoError(t, err)

	// 3. SubscribePersonal on WsAdapter
	adapter := xt.NewWsAdapter()
	pool := pkgws.NewPool("ws://dummy", 30, slog.Default())
	adapter.SetPool(pool)
	adapter.SetClient(client)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = adapter.SubscribePersonal(ctx)
	assert.Error(t, err) // cancelled context should fail subscription

	// Test SubscribePersonal with valid context
	err = adapter.SubscribePersonal(context.Background())
	require.NoError(t, err)

	// Test GetListenKey with map result format
	serverMap := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"returnCode": 0,
			"msgInfo": "success",
			"result": {
				"listenKey": "test-listen-key-map"
			}
		}`))
	}))
	defer serverMap.Close()

	clientMap := xt.NewClient(serverMap.Client(), serverMap.URL, "key", "secret", config.LoggingConfig{})
	lkMap, err := clientMap.GetListenKey(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "test-listen-key-map", lkMap)
}
