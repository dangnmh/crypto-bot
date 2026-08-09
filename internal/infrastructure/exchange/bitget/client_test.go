package bitget_test

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
	"crypto-bot/internal/infrastructure/exchange/bitget"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_GetServerTime(t *testing.T) {
	t.Parallel()

	// Test 1: Direct string value (backwards compatibility/some mocks)
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v2/public/time", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "00000",
			"msg": "success",
			"data": "1695812285073"
		}`))
	}))
	defer server1.Close()

	client1 := bitget.NewClient(server1.Client(), server1.URL, "key", "secret", "pass", config.LoggingConfig{})
	ts1, err := client1.GetServerTime(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1695812285073), ts1)

	// Test 2: Actual API object payload ({"serverTime": "..."})
	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v2/public/time", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "00000",
			"msg": "success",
			"data": {
				"serverTime": "1695812285073"
			}
		}`))
	}))
	defer server2.Close()

	client2 := bitget.NewClient(server2.Client(), server2.URL, "key", "secret", "pass", config.LoggingConfig{})
	ts2, err := client2.GetServerTime(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1695812285073), ts2)
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
					"priceEndStep": "0.1",
					"minLever": "2",
					"maxLever": "125"
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
	assert.Equal(t, 2, details[0].MinLeverage)
	assert.Equal(t, 125, details[0].MaxLeverage)
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
}

func TestClient_GetFundingRates(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v2/mix/market/current-fund-rate", r.URL.Path)
		assert.Equal(t, "USDT-FUTURES", r.URL.Query().Get("productType"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "00000",
			"msg": "success",
			"data": [
				{
					"symbol": "BTCUSDT",
					"fundingRate": "0.00015",
					"nextUpdate": "1743062400000"
				}
			]
		}`))
	}))
	defer server.Close()

	client := bitget.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	frs, err := client.GetFundingRates(context.Background(), []string{"BTCUSDT"})
	require.NoError(t, err)
	require.Len(t, frs, 1)
	assert.Equal(t, "BTCUSDT", frs[0].Symbol)
	assert.Equal(t, 0.00015, frs[0].Rate)
	assert.Equal(t, int64(1743062400000), frs[0].SettleTime)
}

func TestClient_CreateOrder(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/v2/mix/order/place-order", r.URL.Path)

		// Decode and verify the request fields
		var body map[string]any
		err := json.NewDecoder(r.Body).Decode(&body)
		assert.NoError(t, err)

		assert.Equal(t, "limit", body["orderType"])
		assert.Equal(t, "ioc", body["force"])

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
		Type:         exchange.OrderTypeIOC,
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
	assert.Equal(t, 0.5, positions[0].HoldVolContract)
	assert.Equal(t, 48000.0, positions[0].HoldAvgPrice)
	assert.Equal(t, exchange.PositionTypeLong, positions[0].PositionType) // long
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
				"data": {
					"entrustedList": [
						{
							"orderId": "order123",
							"clientOid": "client123",
							"symbol": "BTCUSDT",
							"size": "1.0",
							"price": "50000.0",
							"priceAvg": "50000.0",
							"baseVolume": "1.0",
							"status": "filled",
							"side": "buy",
							"posSide": "long",
							"leverage": "20",
							"cTime": "1695812285073",
							"uTime": "1695812285073"
						}
					]
				}
			}`))
		case "/api/v2/mix/order/orders-pending":
			_, _ = w.Write([]byte(`{
				"code": "00000",
				"msg": "success",
				"data": {
					"entrustedList": [
						{
							"orderId": "order123",
							"clientOid": "client123",
							"symbol": "BTCUSDT",
							"size": "1.0",
							"price": "50000.0",
							"priceAvg": "50000.0",
							"baseVolume": "1.0",
							"status": "filled",
							"side": "buy",
							"posSide": "long",
							"leverage": "20",
							"cTime": "1695812285073",
							"uTime": "1695812285073"
						}
					]
				}
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
				"data": {
					"entrustedList": [
						{
							"orderId": "order123",
							"clientOid": "client123",
							"symbol": "BTCUSDT",
							"size": "1.0",
							"price": "50000.0",
							"priceAvg": "50000.0",
							"baseVolume": "1.0",
							"status": "filled",
							"side": "buy",
							"posSide": "long",
							"leverage": "20",
							"cTime": "1695812285073",
							"uTime": "1695812285073"
						}
					]
				}
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
		case "POST /api/v2/mix/order/close-positions":
			_, _ = w.Write([]byte(`{
				"code": "00000",
				"msg": "success",
				"data": {
					"successList": [
						{"orderId": "order1234", "symbol": "BTCUSDT"}
					],
					"failureList": []
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

func TestClient_GetOrderPNL_Success(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v2/mix/order/detail":
			_, _ = w.Write([]byte(`{
				"code": "00000",
				"msg": "success",
				"data": {
					"orderId": "order123",
					"clientOid": "ext123",
					"symbol": "BTCUSDT",
					"size": "1.0",
					"price": "50000.0",
					"priceAvg": "50000.0",
					"baseVolume": "1.0",
					"state": "filled",
					"side": "sell",
					"posSide": "long",
					"leverage": "20",
					"cTime": "1695812285000",
					"uTime": "1695812295000"
				}
			}`))
		case "/api/v2/mix/position/history-position":
			assert.Equal(t, "BTCUSDT", r.URL.Query().Get("symbol"))
			_, _ = w.Write([]byte(`{
				"code": "00000",
				"msg": "success",
				"data": {
					"list": [
						{
							"positionId": "pos999",
							"marginCoin": "USDT",
							"symbol": "BTCUSDT",
							"holdSide": "long",
							"openAvgPrice": "49000.0",
							"closeAvgPrice": "50000.0",
							"openTotalPos": "1.0",
							"closeTotalPos": "1.0",
							"pnl": "1000.0",
							"netProfit": "998.0",
							"totalFunding": "1.0",
							"openFee": "1.5",
							"closeFee": "1.5",
							"ctime": "1695812285000",
							"utime": "1695812295000"
						}
					],
					"endId": "123456"
				}
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := bitget.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	info, err := client.GetOrderPNL(context.Background(), "BTCUSDT", "order123")
	require.NoError(t, err)
	require.NotNil(t, info)

	assert.Equal(t, "BTCUSDT", info.Symbol)
	assert.Equal(t, 49000.0, info.EntryPrice)
	assert.Equal(t, 50000.0, info.ExitPrice)
	assert.Equal(t, 1.0, *info.ClosedSizeContract)
	assert.Equal(t, 1000.0, info.GrossPnL)
	assert.Equal(t, 3.0, info.Fee)
	assert.Equal(t, 1.0, info.FundingFee)
	assert.Equal(t, int64(10000), info.DurationMs)
	assert.Equal(t, 998.0, info.NetPnl)
	assert.InDelta(t, 2.0408, info.PnLRate, 0.0001)
}

func TestClient_GetOrderPNL_OrderNotFound(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v2/mix/order/detail":
			_, _ = w.Write([]byte(`{
				"code": "40012",
				"msg": "order not found",
				"data": null
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := bitget.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	_, err := client.GetOrderPNL(context.Background(), "BTCUSDT", "order123")
	assert.ErrorContains(t, err, "order not found")
}

func TestClient_RawRequestMethods(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":"00000","msg":"success","data":{"test":"ok"}}`))
	}))
	defer server.Close()

	client := bitget.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	ctx := context.Background()

	res, err := client.RawRequest(ctx, http.MethodGet, "/api/v2/mix/market/tickers", nil, nil)
	require.NoError(t, err)
	assert.Contains(t, string(res), "success")

	res, err = client.GetFundingRateRaw(ctx, nil)
	require.NoError(t, err)
	assert.Contains(t, string(res), "success")

	res, err = client.GetTickersRaw(ctx, nil)
	require.NoError(t, err)
	assert.Contains(t, string(res), "success")

	res, err = client.GetOpenPositionsRaw(ctx, nil)
	require.NoError(t, err)
	assert.Contains(t, string(res), "success")

	res, err = client.GetHistoryPositionsRaw(ctx, nil)
	require.NoError(t, err)
	assert.Contains(t, string(res), "success")

	res, err = client.GetOrderDetailRaw(ctx, "ord123", nil)
	require.NoError(t, err)
	assert.Contains(t, string(res), "success")

	res, err = client.GetHistoryOrdersRaw(ctx, nil)
	require.NoError(t, err)
	assert.Contains(t, string(res), "success")
}

func TestClient_SwitchPositionMode(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v2/mix/account/set-position-mode", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":"00000","msg":"success"}`))
	}))
	defer server.Close()

	client := bitget.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	err := client.SwitchPositionMode(context.Background(), "BTCUSDT", domain.PositionModeHedge)
	require.NoError(t, err)
}

func TestClient_GetPotentialFundingSymbols(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v2/mix/market/tickers":
			_, _ = w.Write([]byte(`{
				"code": "00000",
				"msg": "success",
				"data": [
					{
						"symbol": "BTCUSDT",
						"lastPr": "64000",
						"bidPr": "63999",
						"askPr": "64001",
						"baseVolume": "100.5",
						"quoteVolume": "15000000",
						"ts": "1700000000000"
					}
				]
			}`))
		case "/api/v2/mix/market/current-fund-rate":
			_, _ = w.Write([]byte(`{
				"code": "00000",
				"msg": "success",
				"data": [
					{
						"symbol": "BTCUSDT",
						"fundingRate": "0.001",
						"nextUpdate": "1700000000"
					}
				]
			}`))
		default:
			t.Fatalf("unexpected call to path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := bitget.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	res, err := client.GetPotentialFundingSymbols(context.Background(), 10000000, 0, nil, nil)
	require.NoError(t, err)
	require.Len(t, res, 1)
	assert.Equal(t, "BTCUSDT", res[0].Symbol)
	assert.Equal(t, 0.001, res[0].Rate)
	assert.Equal(t, int64(1700000000), res[0].SettleTime)
}

func TestClient_BitgetRemainingMethods(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v2/mix/account/set-margin-mode":
			_, _ = w.Write([]byte(`{"code":"00000","msg":"success"}`))
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
					"status": "filled",
					"side": "buy",
					"posSide": "long",
					"leverage": "20",
					"cTime": "1695812285073",
					"uTime": "1695812285073"
				}
			}`))
		case "/api/v2/mix/order/place-order":
			_, _ = w.Write([]byte(`{
				"code": "00000",
				"msg": "success",
				"data": {
					"orderId": "order1234"
				}
			}`))
		case "/api/v2/public/time":
			_, _ = w.Write([]byte(`{
				"code": "00000",
				"msg": "success",
				"data": {
					"serverTime": "1700000000000"
				}
			}`))
		}
	}))
	defer server.Close()

	client := bitget.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})

	// 1. SwitchMarginMode
	err := client.SwitchMarginMode(context.Background(), "BTCUSDT", domain.MarginModeIsolated, 10, domain.SideOpenLong)
	require.NoError(t, err)

	// 2. GetOrderByExternalID
	order, err := client.GetOrderByExternalID(context.Background(), "BTCUSDT", "client123")
	require.NoError(t, err)
	assert.Equal(t, "order123", order.OrderID)

	// 3. ClosePosition
	err = client.ClosePosition(context.Background(), "BTCUSDT", domain.SideCloseLong, 1.0, domain.PositionModeHedge, 10)
	require.NoError(t, err)

	// 4. SupportLeverageOnOrder
	assert.False(t, client.SupportLeverageOnOrder())

	// 5. Latency
	_, err = client.Latency(context.Background())
	require.NoError(t, err)

	// 6. WarmUp
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client.WarmUp(ctx, 10*time.Millisecond)

	// 7. GetOrderPNLRaw
	_, _ = client.GetOrderPNLRaw(context.Background(), nil)

	// 8. SetClock
	client.SetClock(exchange.RealClock{})

	// 9. Get and Post
	_, _ = client.Get(context.Background(), "/api/v2/public/time", nil)
	_, _ = client.Post(context.Background(), "/api/v2/mix/account/set-margin-mode", nil)

	// 10. CancelOrders
	_ = client.CancelOrders(context.Background(), []string{"order1234"})
}
