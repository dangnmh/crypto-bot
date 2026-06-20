package kucoin_test

import (
	"context"
	"encoding/json"
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

	assert.Equal(t, "BABYUSDTM", details[1].Symbol)
	assert.Equal(t, "BABY", details[1].BaseCoin)
	assert.Equal(t, 5, details[1].PriceScale)
	assert.Equal(t, 0.00001, details[1].PriceUnit)
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
	assert.Equal(t, 10.0, positions[0].HoldVol)
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
	assert.Equal(t, 2.0, res.ClosedSize)
	assert.Equal(t, 0.5837, res.GrossPnL)
	assert.Equal(t, 0.03766066, res.Fee)
	assert.Equal(t, -0.03389521, res.FundingFee)
	assert.Equal(t, int64(1735589352069-1735549162120), res.DurationMs)
	assert.Equal(t, 0.51214413, res.NetPnl)
}
