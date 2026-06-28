package bitunix_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/bitunix"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_Signature(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "my-api-key", r.Header.Get("api-key"))
		assert.NotEmpty(t, r.Header.Get("nonce"))
		assert.NotEmpty(t, r.Header.Get("timestamp"))
		assert.NotEmpty(t, r.Header.Get("sign"))

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":0,"msg":"success","data":{"orderId":"112233"}}`))
	}))
	defer server.Close()

	client := bitunix.NewClient(server.Client(), server.URL, "my-api-key", "my-api-secret", config.LoggingConfig{})

	res, err := client.CreateOrder(context.Background(), exchange.SubmitOrderRequest{
		Symbol: "BTCUSDT",
		Price:  50000,
		Vol:    0.1,
		Side:   domain.SideOpenLong,
		Type:   domain.OrderTypeLimit,
	})
	require.NoError(t, err)
	assert.Equal(t, "112233", res.OrderID)
}

func TestClient_CreateOrder_TPSL(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/v1/futures/trade/place_order", r.URL.Path)

		var body map[string]any
		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)

		assert.Equal(t, "65000", body["tpPrice"])
		assert.Equal(t, "LAST_PRICE", body["tpStopType"])
		assert.Equal(t, "MARKET", body["tpOrderType"])

		assert.Equal(t, "55000", body["slPrice"])
		assert.Equal(t, "LAST_PRICE", body["slStopType"])
		assert.Equal(t, "MARKET", body["slOrderType"])

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":0,"msg":"success","data":{"orderId":"998877"}}`))
	}))
	defer server.Close()

	client := bitunix.NewClient(server.Client(), server.URL, "my-api-key", "my-api-secret", config.LoggingConfig{})

	res, err := client.CreateOrder(context.Background(), exchange.SubmitOrderRequest{
		Symbol:          "BTCUSDT",
		Price:           60000,
		Vol:             0.1,
		Side:            domain.SideOpenLong,
		Type:            domain.OrderTypeLimit,
		TakeProfitPrice: 65000,
		StopLossPrice:   55000,
	})
	require.NoError(t, err)
	assert.Equal(t, "998877", res.OrderID)
	assert.True(t, res.TPSLSubmitted)
}

func TestClient_GetServerTime(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		headers    map[string]string
		expectedTs int64
	}{
		{
			name: "high precision arrive time",
			headers: map[string]string{
				"req-arrive-time": "1782577741634",
			},
			expectedTs: 1782577741634,
		},
		{
			name: "fallback to date header",
			headers: map[string]string{
				"Date": "Mon, 02 Jan 2006 15:04:05 GMT",
			},
			expectedTs: 1136214245000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/", r.URL.Path)
				for k, v := range tt.headers {
					w.Header().Set(k, v)
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client := bitunix.NewClient(server.Client(), server.URL, "", "", config.LoggingConfig{})
			ts, err := client.GetServerTime(context.Background())
			require.NoError(t, err)
			assert.Equal(t, tt.expectedTs, ts)
		})
	}
}

func TestClient_SupportLeverageOnOrder(t *testing.T) {
	t.Parallel()

	client := bitunix.NewClient(nil, "https://fapi.bitunix.com", "", "", config.LoggingConfig{})
	assert.False(t, client.SupportLeverageOnOrder())
}

func TestClient_GetTickers(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/futures/market/tickers", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code": 0,
			"data": [
				{
					"symbol": "BTCUSDT",
					"markPrice": "60000",
					"lastPrice": "59950",
					"baseVol": "100.5",
					"quoteVol": "6000000",
					"last": "59950"
				}
			]
		}`))
	}))
	defer server.Close()

	client := bitunix.NewClient(server.Client(), server.URL, "", "", config.LoggingConfig{})

	tickers, err := client.GetTickers(context.Background(), "")
	require.NoError(t, err)
	require.Len(t, tickers, 1)
	assert.Equal(t, "BTCUSDT", tickers[0].Symbol)
	assert.Equal(t, 59950.0, tickers[0].LastPrice)
	assert.Equal(t, 100.5, tickers[0].Volume24)
	assert.Equal(t, 6000000.0, tickers[0].AmountUSDT24)
}

func TestClient_GetContractDetails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/futures/market/trading_pairs", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code": 0,
			"data": [
				{
					"symbol": "BTCUSDT",
					"base": "BTC",
					"quote": "USDT",
					"minTradeVolume": "0.0001",
					"basePrecision": 4,
					"quotePrecision": 1,
					"maxLeverage": 200,
					"minLeverage": 1,
					"defaultLeverage": 20,
					"symbolStatus": "OPEN"
				}
			]
		}`))
	}))
	defer server.Close()

	client := bitunix.NewClient(server.Client(), server.URL, "", "", config.LoggingConfig{})

	details, err := client.GetContractDetails(context.Background())
	require.NoError(t, err)
	require.Len(t, details, 1)
	assert.Equal(t, "BTCUSDT", details[0].Symbol)
	assert.Equal(t, "BTC", details[0].BaseCoin)
	assert.Equal(t, "USDT", details[0].QuoteCoin)
	assert.Equal(t, 1, details[0].MinVol)
	assert.Equal(t, 1, details[0].VolUnit)
	assert.Equal(t, 4, details[0].VolScale)
	assert.Equal(t, 1, details[0].PriceScale)
}

func TestClient_GetFundingRates(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/futures/market/funding_rate/batch", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code": 0,
			"data": [
				{
					"symbol": "BTCUSDT",
					"fundingRate": "0.01",
					"nextFundingTime": "1782576000000"
				}
			]
		}`))
	}))
	defer server.Close()

	client := bitunix.NewClient(server.Client(), server.URL, "", "", config.LoggingConfig{})

	rates, err := client.GetFundingRates(context.Background(), []string{"BTCUSDT"})
	require.NoError(t, err)
	require.Len(t, rates, 1)
	assert.Equal(t, "BTCUSDT", rates[0].Symbol)
	assert.Equal(t, 0.0001, rates[0].Rate)
}

func TestClient_GetPotentialFundingSymbols(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/futures/market/tickers" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"code": 0,
				"data": [
					{
						"symbol": "BTCUSDT",
						"quoteVol": "2000000",
						"markPrice": "60000"
					}
				]
			}`))
			return
		}
		if r.URL.Path == "/api/v1/futures/market/funding_rate/batch" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"code": 0,
				"data": [
					{
						"symbol": "BTCUSDT",
						"fundingRate": "0.02",
						"nextFundingTime": "1782576000000"
					}
				]
			}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := bitunix.NewClient(server.Client(), server.URL, "", "", config.LoggingConfig{})

	res, err := client.GetPotentialFundingSymbols(context.Background(), 1000000, 0, nil, nil)
	require.NoError(t, err)
	require.Len(t, res, 1)
	assert.Equal(t, "BTCUSDT", res[0].Symbol)
	assert.Equal(t, 0.0002, res[0].Rate)
	assert.Equal(t, 2000000.0, res[0].Volume24h)
}

func TestClient_OrderManagement(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if r.Method == "POST" && r.URL.Path == "/api/v1/futures/trade/cancel_orders" {
			_, _ = w.Write([]byte(`{"code":0,"msg":"success"}`))
			return
		}
		if r.Method == "GET" && r.URL.Path == "/api/v1/futures/trade/get_pending_orders" {
			_, _ = w.Write([]byte(`{
				"code": 0,
				"data": {
					"orderList": [
						{
							"orderId": "123456",
							"symbol": "BTCUSDT",
							"side": "BUY",
							"tradeSide": "OPEN",
							"price": "60000",
							"qty": "0.5",
							"status": "NEW",
							"createTime": 1782576000000
						}
					]
				}
			}`))
			return
		}
		if r.Method == "GET" && r.URL.Path == "/api/v1/futures/trade/get_order_detail" {
			_, _ = w.Write([]byte(`{
				"code": 0,
				"data": {
					"orderId": "123456",
					"clientId": "ext-123",
					"symbol": "BTCUSDT",
					"side": "BUY",
					"tradeSide": "OPEN",
					"price": "60000",
					"qty": "0.5",
					"status": "NEW",
					"createTime": 1782576000000
				}
			}`))
			return
		}
		if r.Method == "GET" && r.URL.Path == "/api/v1/futures/trade/get_history_orders" {
			_, _ = w.Write([]byte(`{
				"code": 0,
				"data": {
					"orderList": [
						{
							"orderId": "123456",
							"clientId": "ext-123",
							"symbol": "BTCUSDT",
							"side": "BUY",
							"tradeSide": "OPEN",
							"price": "60000",
							"qty": "0.5",
							"status": "FILLED",
							"createTime": 1782576000000
						}
					]
				}
			}`))
			return
		}
	}))
	defer server.Close()

	client := bitunix.NewClient(server.Client(), server.URL, "my-key", "my-secret", config.LoggingConfig{})

	// Test CancelOrder
	err := client.CancelOrder(context.Background(), "BTCUSDT", "123456")
	require.NoError(t, err)

	// Test CancelOrders
	err = client.CancelOrders(context.Background(), []string{"123456"})
	require.NoError(t, err)

	// Test CancelAllOpenOrders
	err = client.CancelAllOpenOrders(context.Background(), "BTCUSDT")
	require.NoError(t, err)

	// Test GetOpenOrders
	open, err := client.GetOpenOrders(context.Background(), "BTCUSDT")
	require.NoError(t, err)
	require.Len(t, open, 1)
	assert.Equal(t, "123456", open[0].OrderID)

	// Test GetOrder
	order, err := client.GetOrder(context.Background(), "BTCUSDT", "123456")
	require.NoError(t, err)
	assert.Equal(t, "123456", order.OrderID)

	// Test GetOrderByExternalID
	extOrder, err := client.GetOrderByExternalID(context.Background(), "BTCUSDT", "ext-123")
	require.NoError(t, err)
	assert.Equal(t, "123456", extOrder.OrderID)

	// Test GetHistoryOrders
	histOrders, err := client.GetHistoryOrders(context.Background(), "BTCUSDT", 0, 0, 0, 100)
	require.NoError(t, err)
	require.Len(t, histOrders, 1)
}

func TestClient_TradeAndPosition(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if r.Method == "POST" && r.URL.Path == "/api/v1/futures/account/change_leverage" {
			_, _ = w.Write([]byte(`{"code":0,"msg":"success","data":{"marginCoin":"USDT","leverage":20,"symbol":"BTCUSDT"}}`))
			return
		}
		if r.Method == "POST" && r.URL.Path == "/api/v1/futures/account/change_margin_mode" {
			_, _ = w.Write([]byte(`{"code":0,"msg":"success","data":{"marginMode":"ISOLATION"}}`))
			return
		}
		if r.Method == "POST" && r.URL.Path == "/api/v1/futures/account/change_position_mode" {
			_, _ = w.Write([]byte(`{"code":0,"msg":"success","data":{"positionMode":"HEDGE"}}`))
			return
		}
		if r.Method == "GET" && r.URL.Path == "/api/v1/futures/position/get_pending_positions" {
			_, _ = w.Write([]byte(`{
				"code": 0,
				"data": [
					{
						"positionId": "999888",
						"symbol": "BTCUSDT",
						"side": "LONG",
						"openPrice": "60000",
						"size": "0.1",
						"leverage": 20,
						"unrealizedProfit": "100",
						"marginMode": "ISOLATION"
					}
				]
			}`))
			return
		}
		if r.Method == "POST" && r.URL.Path == "/api/v1/futures/trade/place_order" {
			_, _ = w.Write([]byte(`{"code":0,"msg":"success","data":{"orderId":"555444"}}`))
			return
		}
		if r.Method == "POST" && r.URL.Path == "/api/v1/futures/trade/close_all_position" {
			_, _ = w.Write([]byte(`{"code":0,"msg":"success"}`))
			return
		}
	}))
	defer server.Close()

	client := bitunix.NewClient(server.Client(), server.URL, "my-key", "my-secret", config.LoggingConfig{})

	// Test ChangeLeverage
	err := client.ChangeLeverage(context.Background(), exchange.ChangeLeverageRequest{
		Symbol:   "BTCUSDT",
		Leverage: 20,
	})
	require.NoError(t, err)

	// Test SwitchMarginMode
	err = client.SwitchMarginMode(context.Background(), "BTCUSDT", "ISOLATED", 20, domain.SideOpenLong)
	require.NoError(t, err)

	// Test SwitchPositionMode
	err = client.SwitchPositionMode(context.Background(), "BTCUSDT", domain.PositionModeHedge)
	require.NoError(t, err)

	// Test GetOpenPositions
	positions, err := client.GetOpenPositions(context.Background(), "BTCUSDT")
	require.NoError(t, err)
	require.Len(t, positions, 1)
	assert.Equal(t, 0.1, positions[0].HoldVol)

	// Test ClosePosition
	err = client.ClosePosition(context.Background(), "BTCUSDT", domain.SideCloseLong, 0, domain.PositionModeHedge, 0)
	require.NoError(t, err)

	// Test CloseAllPositions
	err = client.CloseAllPositions(context.Background(), "BTCUSDT")
	require.NoError(t, err)
}

func TestClient_GetOrderPNL(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if r.Method == "GET" && r.URL.Path == "/api/v1/futures/trade/get_order_detail" {
			_, _ = w.Write([]byte(`{
				"code": 0,
				"data": {
					"orderId": "123456",
					"symbol": "BTCUSDT",
					"side": "BUY",
					"tradeSide": "OPEN",
					"price": "60000",
					"qty": "0.5",
					"status": "FILLED",
					"createTime": 1782576000000
				}
			}`))
			return
		}
		if r.Method == "GET" && r.URL.Path == "/api/v1/futures/position/get_history_positions" {
			_, _ = w.Write([]byte(`{
				"code": 0,
				"data": {
					"positionList": [
						{
							"symbol": "BTCUSDT",
							"side": "LONG",
							"maxQty": "0.5",
							"entryPrice": "59000",
							"closePrice": "60000",
							"realizedPNL": "50",
							"fee": "5",
							"funding": "2",
							"mtime": 1782576015000
						}
					]
				}
			}`))
			return
		}
	}))
	defer server.Close()

	client := bitunix.NewClient(server.Client(), server.URL, "my-key", "my-secret", config.LoggingConfig{})

	pnl, err := client.GetOrderPNL(context.Background(), "BTCUSDT", "123456")
	require.NoError(t, err)
	assert.Equal(t, 53.0, pnl.GrossPnL)
	assert.Equal(t, 5.0, pnl.Fee)
	assert.Equal(t, 2.0, pnl.FundingFee)
	assert.Equal(t, 50.0, pnl.NetPnl)
	assert.Equal(t, 59000.0, pnl.EntryPrice)
	assert.Equal(t, 60000.0, pnl.ExitPrice)
}
