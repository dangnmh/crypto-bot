package kucoin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/domain"
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
					"status": "Open",
					"maxLeverage": 20
				},
				{
					"symbol": "BABYUSDTM",
					"baseCurrency": "BABY",
					"quoteCurrency": "USDT",
					"settleCurrency": "USDT",
					"lotSize": 1,
					"tickSize": 0.00001,
					"multiplier": 10,
					"status": "Open"
				}
			]
		}`))
	}))
	defer server.Close()

	client := kucoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	details, err := client.GetContractDetails(context.Background())
	require.NoError(t, err)
	require.Len(t, details, 2)
	assert.Equal(t, "XBTUSDTM", details[0].Symbol)
	assert.Equal(t, "XBT", details[0].BaseCoin)
	assert.Equal(t, 1, details[0].PriceScale)
	assert.Equal(t, 0.1, details[0].PriceUnit)
	assert.Equal(t, 20, details[0].MaxLeverage)

	assert.Equal(t, "BABYUSDTM", details[1].Symbol)
	assert.Equal(t, "BABY", details[1].BaseCoin)
	assert.Equal(t, 5, details[1].PriceScale)
	assert.Equal(t, 0.00001, details[1].PriceUnit)
	assert.Equal(t, 100, details[1].MaxLeverage) // Fallback default
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

func TestClient_CreateOrder_HedgeAndOptions(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/v1/orders", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "200000",
			"msg": "success",
			"data": {
				"orderId": "1234567",
				"clientOid": "external_456"
			}
		}`))
	}))
	defer server.Close()

	client := kucoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	res, err := client.CreateOrder(context.Background(), exchange.SubmitOrderRequest{
		Symbol:       "XBTUSDTM",
		Vol:          2.0,
		Side:         exchange.SideCloseShort,
		Type:         exchange.OrderTypeMarket,
		Leverage:     5,
		OpenType:     exchange.OpenTypeCross,
		PositionMode: 1, // Hedge Mode
		ExternalOID:  "external_456",
	})
	require.NoError(t, err)
	assert.Equal(t, "1234567", res.OrderID)
}

func TestClient_CreateOrder_WithTPSL(t *testing.T) {
	t.Parallel()

	var receivedBody map[string]any
	var receivedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		receivedPath = r.URL.Path

		w.Header().Set("Content-Type", "application/json")

		var reqBody map[string]any
		dec := json.NewDecoder(r.Body)
		if err := dec.Decode(&reqBody); err == nil {
			receivedBody = reqBody
		}

		_, _ = w.Write([]byte(`{
			"code": "200000",
			"msg": "success",
			"data": {
				"orderId": "tpsl_order_123",
				"clientOid": "external_tpsl"
			}
		}`))
	}))
	defer server.Close()

	client := kucoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})

	// 1. Long position TP/SL
	res, err := client.CreateOrder(context.Background(), exchange.SubmitOrderRequest{
		Symbol:          "XBTUSDTM",
		Vol:             1.0,
		Side:            exchange.SideOpenLong,
		Type:            exchange.OrderTypeMarket,
		TakeProfitPrice: 55000.0,
		StopLossPrice:   45000.0,
		ExternalOID:     "external_tpsl",
	})
	require.NoError(t, err)
	assert.True(t, res.TPSLSubmitted)
	assert.Equal(t, "tpsl_order_123", res.OrderID)
	assert.Equal(t, "/api/v1/st-orders", receivedPath)
	assert.Equal(t, "55000", receivedBody["triggerStopUpPrice"])
	assert.Equal(t, "45000", receivedBody["triggerStopDownPrice"])
	assert.Equal(t, "TP", receivedBody["stopPriceType"])

	// Reset received variables
	receivedBody = nil
	receivedPath = ""

	// 2. Short position TP/SL
	res, err = client.CreateOrder(context.Background(), exchange.SubmitOrderRequest{
		Symbol:          "XBTUSDTM",
		Vol:             1.0,
		Side:            exchange.SideOpenShort,
		Type:            exchange.OrderTypeMarket,
		TakeProfitPrice: 45000.0,
		StopLossPrice:   55000.0,
		ExternalOID:     "external_tpsl",
	})
	require.NoError(t, err)
	assert.True(t, res.TPSLSubmitted)
	assert.Equal(t, "tpsl_order_123", res.OrderID)
	assert.Equal(t, "/api/v1/st-orders", receivedPath)
	assert.Equal(t, "55000", receivedBody["triggerStopUpPrice"])
	assert.Equal(t, "45000", receivedBody["triggerStopDownPrice"])
	assert.Equal(t, "TP", receivedBody["stopPriceType"])
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
				"id": "123456",
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
						"id": "123456",
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
	assert.Equal(t, 10.0, positions[0].HoldVolContract)
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
							"id": "123456",
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

	client := kucoin.NewClient(nil, "", "key", "secret", "pass", config.LoggingConfig{})

	// 2. CancelOrders unimplemented
	err := client.CancelOrders(context.Background(), []string{"1"})
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

func TestClient_PlaceTPSL(t *testing.T) {
	t.Parallel()

	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/v1/st-orders", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")

		// Decode request body for verification
		var req map[string]any
		dec := json.NewDecoder(r.Body)
		if err := dec.Decode(&req); err == nil {
			receivedBody = req
		}

		_, _ = w.Write([]byte(`{
			"code": "200000",
			"msg": "success",
			"data": {
				"orderId": "tpsl_123456"
			}
		}`))
	}))
	defer server.Close()

	client := kucoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})

	// 1. Test Long position TP/SL (Hedge Mode)
	err := client.PlaceTPSL(context.Background(), exchange.TPSLRequest{
		Symbol:          "XBTUSDTM",
		PositionMode:    1, // Hedge
		Side:            exchange.SideOpenLong,
		TakeProfitPrice: 55000.0,
		StopLossPrice:   45000.0,
		Volume:          10.0,
	})
	require.NoError(t, err)
	assert.Equal(t, "LONG", receivedBody["positionSide"])
	assert.Equal(t, "sell", receivedBody["side"])
	assert.NotContains(t, receivedBody, "closeOrder")
	assert.Equal(t, "55000", receivedBody["triggerStopUpPrice"])
	assert.Equal(t, "45000", receivedBody["triggerStopDownPrice"])

	// 2. Test Short position TP/SL (Hedge Mode)
	err = client.PlaceTPSL(context.Background(), exchange.TPSLRequest{
		Symbol:          "XBTUSDTM",
		PositionMode:    1, // Hedge
		Side:            exchange.SideOpenShort,
		TakeProfitPrice: 45000.0,
		StopLossPrice:   55000.0,
		Volume:          10.0,
	})
	require.NoError(t, err)
	assert.Equal(t, "SHORT", receivedBody["positionSide"])
	assert.Equal(t, "buy", receivedBody["side"])
	assert.Equal(t, "55000", receivedBody["triggerStopUpPrice"])
	assert.Equal(t, "45000", receivedBody["triggerStopDownPrice"])

	// 3. Test One-Way Mode
	err = client.PlaceTPSL(context.Background(), exchange.TPSLRequest{
		Symbol:          "XBTUSDTM",
		PositionMode:    2, // OneWay
		Side:            exchange.SideOpenLong,
		TakeProfitPrice: 55000.0,
		StopLossPrice:   45000.0,
		Volume:          10.0,
	})
	require.NoError(t, err)
	assert.Equal(t, "BOTH", receivedBody["positionSide"])
	assert.Equal(t, "55000", receivedBody["triggerStopUpPrice"])
	assert.Equal(t, "45000", receivedBody["triggerStopDownPrice"])
}

func TestClient_GetOrderPNL(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/v1/orders/close_order_id_123":
			_, _ = w.Write([]byte(`{
				"code": "200000",
				"msg": "success",
				"data": {
					"id": "close_order_id_123",
					"clientOid": "external_close_oid",
					"symbol": "XBTUSDTM",
					"status": "done",
					"statusVal": "done",
					"isActive": false
				}
			}`))
		case "/api/v1/fills":
			assert.Equal(t, "close_order_id_123", r.URL.Query().Get("orderId"))
			_, _ = w.Write([]byte(`{
				"code": "200000",
				"msg": "success",
				"data": {
					"items": [
						{
							"tradeId": "trade_1",
							"orderId": "close_order_id_123",
							"symbol": "XBTUSDTM",
							"side": "sell",
							"price": "94443.5",
							"size": 2,
							"fee": "0.03766066",
							"createdAt": 1735589352069
						}
					]
				}
			}`))
		case "/api/v1/history-positions":
			assert.Equal(t, "XBTUSDTM", r.URL.Query().Get("symbol"))
			_, _ = w.Write([]byte(`{
				"code": "200000",
				"msg": "success",
				"data": {
					"items": [
						{
							"closeId": "500000000036305465",
							"userId": "633559791e1cbc0001f319bc",
							"symbol": "XBTUSDTM",
							"settleCurrency": "USDT",
							"leverage": "1.0",
							"type": "CLOSE_LONG",
							"pnl": "0.51214413",
							"realisedGrossCost": "-0.5837",
							"withdrawPnl": "0.0",
							"tradeFee": "0.03766066",
							"fundingFee": "-0.03389521",
							"openTime": 1735549162120,
							"closeTime": 1735589352069,
							"openPrice": "93859.8",
							"closePrice": "94443.5",
							"marginMode": "CROSS",
							"positionSide": "BOTH",
							"side": "LONG"
						}
					]
				}
			}`))
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := kucoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	res, err := client.GetOrderPNL(context.Background(), "XBTUSDTM", "close_order_id_123")
	require.NoError(t, err)

	assert.Equal(t, "XBTUSDTM", res.Symbol)
	assert.Equal(t, 93859.8, res.EntryPrice)
	assert.Equal(t, 94443.5, res.ExitPrice)
	assert.Equal(t, 2.0, *res.ClosedSizeContract)
	assert.Equal(t, 0.5837, res.GrossPnL)
	assert.Equal(t, 0.03766066, res.Fee)
	assert.Equal(t, -0.03389521, res.FundingFee)
	assert.Equal(t, int64(1735589352069-1735549162120), res.DurationMs)
	assert.Equal(t, 0.51214413, res.NetPnl)
}

func TestClient_RemainingMethods(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/contracts/active":
			_, _ = w.Write([]byte(`{
				"code": "200000",
				"data": [
					{
						"symbol": "XBTUSDTM",
						"status": "Open",
						"fundingFeeRate": 0.001,
						"nextFundingRateDateTime": 1700000000,
						"turnoverOf24h": 15000000
					}
				]
			}`))
		case "/api/v1/allTickers":
			_, _ = w.Write([]byte(`{
				"code": "200000",
				"data": [
					{
						"symbol": "XBTUSDTM",
						"lastPrice": "64000",
						"price": "64000"
					}
				]
			}`))
		case "/api/v1/orders/byClientOid":
			_, _ = w.Write([]byte(`{
				"code": "200000",
				"data": {
					"id": "ord123",
					"clientOid": "client-order-id",
					"symbol": "XBTUSDTM"
				}
			}`))
		case "/api/v2/position/changeMarginMode":
			_, _ = w.Write([]byte(`{"code":"200000","data":{}}`))
		case "/api/v1/position/leverage":
			_, _ = w.Write([]byte(`{"code":"200000","data":{}}`))
		case "/api/v1/timestamp":
			_, _ = w.Write([]byte(`{"code":"200000","data":1700000000}`))
		case "/api/v1/ticker":
			_, _ = w.Write([]byte(`{"code":"200000","data":{"symbol":"XBTUSDTM","price":64000.0}}`))
		}
	}))
	defer server.Close()

	client := kucoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})

	// 1. GetPotentialFundingSymbols
	res, err := client.GetPotentialFundingSymbols(context.Background(), 10000000, 0, nil, nil)
	require.NoError(t, err)
	require.Len(t, res, 1)
	assert.Equal(t, "XBTUSDTM", res[0].Symbol)
	assert.Equal(t, 0.001, res[0].Rate)

	// 2. GetOrderByExternalID
	order, err := client.GetOrderByExternalID(context.Background(), "XBTUSDTM", "client-order-id")
	require.NoError(t, err)
	assert.Equal(t, "ord123", order.OrderID)

	// 3. WarmUp
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client.WarmUp(ctx, 10*time.Millisecond)

	// 4. SupportLeverageOnOrder
	assert.True(t, client.SupportLeverageOnOrder())

	// 5. ChangeLeverage & SwitchMarginMode
	err = client.ChangeLeverage(context.Background(), exchange.ChangeLeverageRequest{Symbol: "XBTUSDTM", Leverage: 10})
	assert.ErrorContains(t, err, "not implemented")

	err = client.SwitchMarginMode(context.Background(), "XBTUSDTM", domain.MarginModeIsolated, 10, domain.SideOpenLong)
	require.NoError(t, err)

	// 6. GetServerTime & Latency
	ts, err := client.GetServerTime(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1700000000), ts)

	_, err = client.Latency(context.Background())
	require.NoError(t, err)

	// 7. Cover Raw methods to hit client.go lines
	_, _ = client.GetFundingRateRaw(context.Background(), nil)
	_, _ = client.GetHistoryOrdersRaw(context.Background(), nil)
	_, _ = client.GetClosedPnLRaw(context.Background(), nil)
	_, _ = client.GetOrderPNLRaw(context.Background(), nil)

	// 8. GetTickers with single symbol
	_, _ = client.GetTickers(context.Background(), "XBTUSDTM")
}

func TestClient_GetDepth(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v1/level2/snapshot", r.URL.Path)
		assert.Equal(t, "XBTUSDTM", r.URL.Query().Get("symbol"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code": "200000",
			"data": {
				"sequence": 16,
				"symbol": "XBTUSDTM",
				"bids": [
					["3988.51", 56],
					["3988.50", 15]
				],
				"asks": [
					["3988.59", 3],
					["3988.60", 47]
				]
			}
		}`))
	}))
	defer server.Close()

	client := kucoin.NewClient(
		server.Client(),
		server.URL,
		"key",
		"secret",
		"passphrase",
		config.LoggingConfig{},
	)

	ob, err := client.GetDepth(context.Background(), "XBTUSDTM")
	require.NoError(t, err)
	require.NotNil(t, ob)
	assert.Equal(t, "XBTUSDTM", ob.Symbol)
	assert.Equal(t, int64(16), ob.Version)
	require.Len(t, ob.Bids, 2)
	assert.Equal(t, 3988.51, ob.Bids[0].Price)
	assert.Equal(t, 56.0, ob.Bids[0].Volume)
	require.Len(t, ob.Asks, 2)
	assert.Equal(t, 3988.59, ob.Asks[0].Price)
	assert.Equal(t, 3.0, ob.Asks[0].Volume)
}

func TestClient_GetDepthCommits(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "XBTUSDTM", r.URL.Query().Get("symbol"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		if r.URL.Path == "/api/v1/level2/depth20" {
			_, _ = w.Write([]byte(`{
				"code": "200000",
				"data": {
					"sequence": 17,
					"symbol": "XBTUSDTM",
					"bids": [["3988.55", 10]],
					"asks": [["3988.58", 5]]
				}
			}`))
			return
		}

		assert.Equal(t, "/api/v1/level2/depth100", r.URL.Path)
		_, _ = w.Write([]byte(`{
			"code": "200000",
			"data": {
				"sequence": 18,
				"symbol": "XBTUSDTM",
				"bids": [["3988.50", 20]],
				"asks": [["3988.60", 15]]
			}
		}`))
	}))
	defer server.Close()

	client := kucoin.NewClient(
		server.Client(),
		server.URL,
		"key",
		"secret",
		"passphrase",
		config.LoggingConfig{},
	)

	// 1. limit <= 20 routes to depth20
	commits, err := client.GetDepthCommits(context.Background(), "XBTUSDTM", 20)
	require.NoError(t, err)
	require.Len(t, commits, 1)
	assert.Equal(t, int64(17), commits[0].Version)
	require.Len(t, commits[0].Bids, 1)
	assert.Equal(t, 3988.55, commits[0].Bids[0].Price)

	// 2. limit > 20 routes to depth100
	commits, err = client.GetDepthCommits(context.Background(), "XBTUSDTM", 100)
	require.NoError(t, err)
	require.Len(t, commits, 1)
	assert.Equal(t, int64(18), commits[0].Version)
	require.Len(t, commits[0].Bids, 1)
	assert.Equal(t, 3988.50, commits[0].Bids[0].Price)
}

func TestClient_GetTopGainer(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/v1/contracts/active":
			_, _ = w.Write([]byte(`{
				"code": "200000",
				"data": [
					{
						"symbol": "BTCUSDTM",
						"status": "Open",
						"turnoverOf24h": 5000000,
						"volumeOf24h": 100,
						"lastTradePrice": 50000,
						"priceChgPct": 0.05
					},
					{
						"symbol": "ETHUSDTM",
						"status": "Open",
						"turnoverOf24h": 2000000,
						"volumeOf24h": 1000,
						"lastTradePrice": 3000,
						"priceChgPct": 0.10
					},
					{
						"symbol": "CLOSED_M",
						"status": "Closed",
						"turnoverOf24h": 100,
						"lastTradePrice": 10,
						"priceChgPct": 0.50
					}
				]
			}`))
		case "/api/v1/allTickers":
			_, _ = w.Write([]byte(`{
				"code": "200000",
				"data": [
					{
						"symbol": "BTCUSDTM",
						"bestBidPrice": "49990",
						"bestAskPrice": "50010",
						"lastPrice": "50000",
						"ts": 1700000000000
					},
					{
						"symbol": "ETHUSDTM",
						"bestBidPrice": "2995",
						"bestAskPrice": "3005",
						"lastPrice": "3000",
						"ts": 1700000000000
					}
				]
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := kucoin.NewClient(
		server.Client(),
		server.URL,
		"key",
		"secret",
		"passphrase",
		config.LoggingConfig{},
	)

	// Test full list with sorting
	results, err := client.GetTopGainer(context.Background(), exchange.TopGainerRequest{})
	require.NoError(t, err)
	require.Len(t, results, 2) // CLOSED_M should be filtered out

	// ETHUSDTM has 10% gain, BTCUSDTM has 5% gain
	assert.Equal(t, "ETHUSDTM", results[0].Symbol)
	assert.Equal(t, 10.0, results[0].Gain24hPct)
	assert.Equal(t, 3000.0, results[0].LastPrice)
	assert.Equal(t, 2995.0, results[0].Bid1)
	assert.Equal(t, 3005.0, results[0].Ask1)
	assert.Equal(t, 2000000.0, results[0].Volume24hUSDT)
	assert.InDelta(t, ((3005.0-2995.0)/2995.0)*100, results[0].SpreadPct, 0.001)

	assert.Equal(t, "BTCUSDTM", results[1].Symbol)
	assert.Equal(t, 5.0, results[1].Gain24hPct)

	// Test with Limit
	limited, err := client.GetTopGainer(context.Background(), exchange.TopGainerRequest{Limit: 1})
	require.NoError(t, err)
	require.Len(t, limited, 1)
	assert.Equal(t, "ETHUSDTM", limited[0].Symbol)
}
