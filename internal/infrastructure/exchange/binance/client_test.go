package binance_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/binance"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_GetServerTime(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/fapi/v1/ping":
			_, _ = w.Write([]byte(`{}`))
		case "/fapi/v1/time":
			_, _ = w.Write([]byte(`{"serverTime": 1672531200000}`))
		}
	}))
	defer server.Close()

	client := binance.NewClient(server.Client(), server.URL, "api_key", "api_secret", config.LoggingConfig{})

	timeVal, err := client.GetServerTime(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1672531200000), timeVal)
}

func TestClient_GetContractDetails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		assert.Contains(t, r.URL.Path, "/fapi/v1/exchangeInfo")

		_, _ = w.Write([]byte(`{
			"symbols": [
				{
					"symbol": "BTCUSDT",
					"status": "TRADING",
					"contractType": "PERPETUAL",
					"baseAsset": "BTC",
					"quoteAsset": "USDT",
					"marginAsset": "USDT",
					"pricePrecision": 2,
					"quantityPrecision": 3,
					"filters": [
						{
							"filterType": "PRICE_FILTER",
							"tickSize": "0.1"
						},
						{
							"filterType": "LOT_SIZE",
							"minQty": "0.001",
							"stepSize": "0.001"
						}
					]
				},
				{
					"symbol": "ETHUSDT",
					"status": "CLOSED",
					"contractType": "PERPETUAL"
				}
			]
		}`))
	}))
	defer server.Close()

	client := binance.NewClient(server.Client(), server.URL, "api_key", "api_secret", config.LoggingConfig{})

	details, err := client.GetContractDetails(context.Background())
	require.NoError(t, err)
	require.Len(t, details, 1)

	d := details[0]
	assert.Equal(t, "BTCUSDT", d.Symbol)
	assert.Equal(t, "BTC", d.BaseCoin)
	assert.Equal(t, "USDT", d.QuoteCoin)
	assert.Equal(t, "USDT", d.SettleCoin)
	assert.Equal(t, 0.1, d.PriceUnit)
	assert.Equal(t, 2, d.PriceScale)
	assert.Equal(t, 3, d.VolScale)
}

func TestClient_GetTickers_And_FundingRates(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/fapi/v1/ticker/24hr":
			_, _ = w.Write([]byte(`[{
				"symbol": "BTCUSDT",
				"lastPrice": "50000.0",
				"volume": "1000.0",
				"quoteVolume": "50000000.0"
			}]`))
		case "/fapi/v1/ticker/bookTicker":
			_, _ = w.Write([]byte(`[{
				"symbol": "BTCUSDT",
				"bidPrice": "49999.0",
				"askPrice": "50001.0"
			}]`))
		case "/fapi/v1/premiumIndex":
			_, _ = w.Write([]byte(`[{
				"symbol": "BTCUSDT",
				"markPrice": "50000.0",
				"indexPrice": "49998.0",
				"lastFundingRate": "0.0001",
				"nextFundingTime": 1672531200000
			}]`))
		}
	}))
	defer server.Close()

	client := binance.NewClient(server.Client(), server.URL, "api_key", "api_secret", config.LoggingConfig{})

	tickers, err := client.GetTickers(context.Background(), "BTCUSDT")
	require.NoError(t, err)
	require.Len(t, tickers, 1)

	t0 := tickers[0]
	assert.Equal(t, "BTCUSDT", t0.Symbol)
	assert.Equal(t, 50000.0, t0.LastPrice)
	assert.Equal(t, 49999.0, t0.Bid1)
	assert.Equal(t, 50001.0, t0.Ask1)

	rates, err := client.GetFundingRates(context.Background(), []string{"BTCUSDT"})
	require.NoError(t, err)
	require.Len(t, rates, 1)
	assert.Equal(t, "BTCUSDT", rates[0].Symbol)
	assert.Equal(t, 0.0001, rates[0].Rate)
	assert.Equal(t, int64(1672531200000), rates[0].SettleTime)
}

func TestClient_GetKlines(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		assert.Contains(t, r.URL.Path, "/fapi/v1/klines")

		_, _ = w.Write([]byte(`[
			[1672531200000, "50000", "50100", "49900", "50050", "10", 1672531259999, "500000", 100, "5", "250000", "0"]
		]`))
	}))
	defer server.Close()

	client := binance.NewClient(server.Client(), server.URL, "api_key", "api_secret", config.LoggingConfig{})

	klines, err := client.GetKlines(context.Background(), "BTCUSDT", "Min1", 0, 0)
	require.NoError(t, err)
	require.Len(t, klines, 1)

	k := klines[0]
	assert.Equal(t, int64(1672531200000), k.Timestamp)
	assert.Equal(t, 50000.0, k.Open)
	assert.Equal(t, 50050.0, k.Close)
	assert.Equal(t, 50100.0, k.High)
	assert.Equal(t, 49900.0, k.Low)
	assert.Equal(t, 10.0, k.Volume)
	assert.Equal(t, 500000.0, k.Amount)
}

func TestClient_GetDepthSnapshot(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		assert.Contains(t, r.URL.Path, "/fapi/v1/depth")

		_, _ = w.Write([]byte(`{
			"lastUpdateId": 987654321,
			"bids": [["49999", "1.5"]],
			"asks": [["50001", "2.5"]]
		}`))
	}))
	defer server.Close()

	client := binance.NewClient(server.Client(), server.URL, "api_key", "api_secret", config.LoggingConfig{})

	book, err := client.GetDepthSnapshot(context.Background(), "BTCUSDT", 20)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", book.Symbol)
	assert.Equal(t, int64(987654321), book.Version)
	require.Len(t, book.Bids, 1)
	require.Len(t, book.Asks, 1)
	assert.Equal(t, 49999.0, book.Bids[0].Price)
	assert.Equal(t, 1.5, book.Bids[0].Volume)
	assert.Equal(t, 50001.0, book.Asks[0].Price)
	assert.Equal(t, 2.5, book.Asks[0].Volume)

	commits, err := client.GetDepthCommits(context.Background(), "BTCUSDT", 20)
	require.NoError(t, err)
	assert.Nil(t, commits)
}

func TestClient_CreateOrder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		req         exchange.SubmitOrderRequest
		wantSide    string
		wantType    string
		wantTif     string
		wantPosSide string
		wantPrice   float32
		wantReduce  string
	}{
		{
			name: "Open Long Limit (Hedge)",
			req: exchange.SubmitOrderRequest{
				Symbol:       "BTCUSDT",
				Vol:          0.5,
				Side:         exchange.SideOpenLong,
				Type:         exchange.OrderTypeLimit,
				Price:        50000.0,
				PositionMode: 1, // Hedge
			},
			wantSide:    "BUY",
			wantType:    "LIMIT",
			wantTif:     "GTC",
			wantPosSide: "LONG",
			wantPrice:   50000.0,
		},
		{
			name: "Close Short (Hedge)",
			req: exchange.SubmitOrderRequest{
				Symbol:       "BTCUSDT",
				Vol:          0.5,
				Side:         exchange.SideCloseShort,
				Type:         exchange.OrderTypeMarket,
				PositionMode: 1,
			},
			wantSide:    "BUY",
			wantType:    "MARKET",
			wantPosSide: "SHORT",
		},
		{
			name: "Close One-Way with Reduce Only",
			req: exchange.SubmitOrderRequest{
				Symbol:       "BTCUSDT",
				Vol:          0.5,
				Side:         exchange.SideCloseShort,
				Type:         exchange.OrderTypeMarket,
				PositionMode: 2, // One-Way
				ReduceOnly:   true,
			},
			wantSide:   "BUY",
			wantType:   "MARKET",
			wantReduce: "true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "POST", r.Method)
				assert.Contains(t, r.URL.Path, "/fapi/v1/order")

				q := r.URL.Query()
				assert.Equal(t, tt.wantSide, q.Get("side"))
				assert.Equal(t, tt.wantType, q.Get("type"))
				if tt.wantTif != "" {
					assert.Equal(t, tt.wantTif, q.Get("timeInForce"))
				} else {
					assert.Empty(t, q.Get("timeInForce"))
				}
				if tt.wantPosSide != "" {
					assert.Equal(t, tt.wantPosSide, q.Get("positionSide"))
				} else {
					assert.Empty(t, q.Get("positionSide"))
				}
				if tt.wantReduce != "" {
					assert.Equal(t, tt.wantReduce, q.Get("reduceOnly"))
				} else {
					assert.Empty(t, q.Get("reduceOnly"))
				}

				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{
					"orderId": 1234567,
					"symbol": "BTCUSDT",
					"status": "NEW",
					"clientOrderId": "external_123"
				}`))
			}))
			defer server.Close()

			client := binance.NewClient(server.Client(), server.URL, "api_key", "api_secret", config.LoggingConfig{})

			res, err := client.CreateOrder(context.Background(), tt.req)
			require.NoError(t, err)
			assert.Equal(t, "1234567", res.OrderID)
		})
	}
}

func TestClient_CancelOrder(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DELETE", r.Method)
		assert.Contains(t, r.URL.Path, "/fapi/v1/order")

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"orderId": 1234567,
			"status": "CANCELED"
		}`))
	}))
	defer server.Close()

	client := binance.NewClient(server.Client(), server.URL, "api_key", "api_secret", config.LoggingConfig{})

	err := client.CancelOrder(context.Background(), "BTCUSDT", "1234567")
	require.NoError(t, err)
}

func TestClient_GetAssets(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		assert.Contains(t, r.URL.Path, "/fapi/v2/balance")

		_, _ = w.Write([]byte(`[
			{
				"asset": "USDT",
				"balance": "1000.0",
				"crossUnPnl": "50.0",
				"availableBalance": "950.0"
			}
		]`))
	}))
	defer server.Close()

	client := binance.NewClient(server.Client(), server.URL, "api_key", "api_secret", config.LoggingConfig{})

	assets, err := client.GetAssets(context.Background())
	require.NoError(t, err)
	require.Len(t, assets, 1)

	assert.Equal(t, "USDT", assets[0].Currency)
	assert.Equal(t, 1000.0, assets[0].CashBalance)
	assert.Equal(t, 1050.0, assets[0].Equity)
	assert.Equal(t, 50.0, assets[0].Unrealized)
	assert.Equal(t, 950.0, assets[0].AvailableBalance)

	asset, err := client.GetAssetByCurrency(context.Background(), "USDT")
	require.NoError(t, err)
	assert.Equal(t, "USDT", asset.Currency)
	assert.Equal(t, 1000.0, asset.CashBalance)
}

func TestClient_GetOpenPositions(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		assert.Contains(t, r.URL.Path, "/fapi/v3/positionRisk")

		_, _ = w.Write([]byte(`[
			{
				"symbol": "BTCUSDT",
				"positionAmt": "0.5",
				"entryPrice": "50000.0",
				"liquidationPrice": "45000.0",
				"unRealizedProfit": "100.0",
				"leverage": "10",
				"positionSide": "LONG",
				"marginType": "cross"
			}
		]`))
	}))
	defer server.Close()

	client := binance.NewClient(server.Client(), server.URL, "api_key", "api_secret", config.LoggingConfig{})

	positions, err := client.GetOpenPositions(context.Background(), "BTCUSDT")
	require.NoError(t, err)
	require.Len(t, positions, 1)

	p := positions[0]
	assert.Equal(t, "BTCUSDT", p.Symbol)
	assert.Equal(t, 0.5, p.HoldVol)
	assert.Equal(t, 50000.0, p.HoldAvgPrice)
	assert.Equal(t, exchange.PositionTypeLong, p.PositionType) // Long
}

func TestClient_ExtendedPrivateMethods(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method == "DELETE" && r.URL.Path == "/fapi/v1/allOpenOrders" {
			_, _ = w.Write([]byte(`{"code": 200, "msg": "success"}`))
			return
		}

		if r.Method == "DELETE" && r.URL.Path == "/fapi/v1/order" {
			_, _ = w.Write([]byte(`{
				"orderId": 1234567,
				"status": "CANCELED"
			}`))
			return
		}

		if r.Method == "GET" && r.URL.Path == "/fapi/v1/order" {
			_, _ = w.Write([]byte(`{
				"orderId": 1234567,
				"symbol": "BTCUSDT",
				"price": "50000.0",
				"origQty": "0.5",
				"avgPrice": "50000.0",
				"executedQty": "0.5",
				"clientOrderId": "external_123",
				"positionSide": "LONG",
				"side": "BUY",
				"status": "FILLED",
				"time": 1672531200000,
				"updateTime": 1672531200000
			}`))
			return
		}

		if r.Method == "GET" && r.URL.Path == "/fapi/v1/openOrders" {
			_, _ = w.Write([]byte(`[
				{
					"orderId": 1234567,
					"symbol": "BTCUSDT",
					"price": "50000.0",
					"origQty": "0.5",
					"avgPrice": "50000.0",
					"executedQty": "0.5",
					"clientOrderId": "external_123",
					"positionSide": "LONG",
					"side": "BUY",
					"status": "FILLED",
					"time": 1672531200000,
					"updateTime": 1672531200000
				}
			]`))
			return
		}

		if r.Method == "POST" && r.URL.Path == "/fapi/v1/leverage" {
			_, _ = w.Write([]byte(`{"symbol": "BTCUSDT", "leverage": 20}`))
			return
		}

		if r.Method == "POST" && r.URL.Path == "/fapi/v1/order" {
			_, _ = w.Write([]byte(`{
				"orderId": 1234567,
				"symbol": "BTCUSDT",
				"status": "FILLED"
			}`))
			return
		}

		if r.Method == "GET" && r.URL.Path == "/fapi/v3/positionRisk" {
			_, _ = w.Write([]byte(`[
				{
					"symbol": "BTCUSDT",
					"positionAmt": "0.5",
					"entryPrice": "50000.0",
					"liquidationPrice": "45000.0",
					"unRealizedProfit": "100.0",
					"leverage": "10",
					"positionSide": "LONG",
					"marginType": "cross"
				}
			]`))
			return
		}
	}))
	defer server.Close()

	client := binance.NewClient(server.Client(), server.URL, "api_key", "api_secret", config.LoggingConfig{})

	// 1. CancelAllOpenOrders
	err := client.CancelAllOpenOrders(context.Background(), "BTCUSDT")
	assert.NoError(t, err)

	// 2. GetOrder
	order, err := client.GetOrder(context.Background(), "BTCUSDT", "1234567")
	require.NoError(t, err)
	assert.Equal(t, "1234567", order.OrderID)
	assert.Equal(t, 50000.0, order.Price)

	// 3. GetOpenOrders
	orders, err := client.GetOpenOrders(context.Background(), "BTCUSDT")
	require.NoError(t, err)
	require.Len(t, orders, 1)
	assert.Equal(t, "1234567", orders[0].OrderID)

	// 4. ChangeLeverage
	err = client.ChangeLeverage(context.Background(), exchange.ChangeLeverageRequest{
		Symbol:   "BTCUSDT",
		Leverage: 20,
	})
	assert.NoError(t, err)

	// 5. ClosePosition
	err = client.ClosePosition(context.Background(), "BTCUSDT", domain.SideCloseLong, 0.5, 1)
	assert.NoError(t, err)

	// 6. CloseAllPositions
	err = client.CloseAllPositions(context.Background(), "BTCUSDT")
	assert.NoError(t, err)

	// 7. CancelOrders
	err = client.CancelOrders(context.Background(), []string{"1234567"})
	assert.NoError(t, err)

	// 8. CreateTrackOrder (stub, should fail)
	_, err = client.CreateTrackOrder(context.Background(), exchange.SubmitTrackOrderRequest{})
	assert.Error(t, err)
}

func TestClient_WarmUp_And_Latency(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Contains(t, r.URL.Path, "/fapi/v1/ping")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := binance.NewClient(server.Client(), server.URL, "api_key", "api_secret", config.LoggingConfig{})

	// 1. Test Latency
	latency, err := client.Latency(context.Background())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, latency, int64(0))

	// 2. Test WarmUp
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client.WarmUp(ctx, time.Second)
}

func TestClient_ListenKey_And_LeverageOnOrder(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" && r.URL.Path == "/fapi/v1/listenKey" {
			_, _ = w.Write([]byte(`{"listenKey": "test_listen_key"}`))
			return
		}
		if r.Method == "PUT" && r.URL.Path == "/fapi/v1/listenKey" {
			_, _ = w.Write([]byte(`{}`))
			return
		}
	}))
	defer server.Close()

	client := binance.NewClient(server.Client(), server.URL, "api_key", "api_secret", config.LoggingConfig{})

	// 1. CreateListenKey
	lk, err := client.CreateListenKey(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "test_listen_key", lk)

	// 2. KeepAliveListenKey
	err = client.KeepAliveListenKey(context.Background())
	assert.NoError(t, err)

	// 3. SupportLeverageOnOrder
	assert.False(t, client.SupportLeverageOnOrder())
}

func TestClient_PlaceTPSL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		req        exchange.TPSLRequest
		wantPath   string
		wantMethod string
	}{
		{
			name: "Place TP and SL for OpenLong in HedgeMode",
			req: exchange.TPSLRequest{
				Symbol:          "BTCUSDT",
				Side:            exchange.SideOpenLong,
				TakeProfitPrice: 55000.0,
				StopLossPrice:   45000.0,
				PositionMode:    1,
			},
			wantPath:   "/fapi/v1/algoOrder",
			wantMethod: "POST",
		},
		{
			name: "Place TP and SL for OpenShort in OneWayMode",
			req: exchange.TPSLRequest{
				Symbol:          "BTCUSDT",
				Side:            exchange.SideOpenShort,
				TakeProfitPrice: 45000.0,
				StopLossPrice:   55000.0,
				PositionMode:    2,
			},
			wantPath:   "/fapi/v1/algoOrder",
			wantMethod: "POST",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var calledCount int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, tt.wantMethod, r.Method)
				assert.Contains(t, r.URL.Path, tt.wantPath)

				q := r.URL.Query()
				assert.Equal(t, "CONDITIONAL", q.Get("algoType"))
				assert.NotEmpty(t, q.Get("triggerPrice"))
				assert.Equal(t, "true", q.Get("closePosition"))

				calledCount++

				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"code": 200, "msg": "success"}`))
			}))
			defer server.Close()

			client := binance.NewClient(server.Client(), server.URL, "api_key", "api_secret", config.LoggingConfig{})

			err := client.PlaceTPSL(context.Background(), tt.req)
			require.NoError(t, err)
			assert.Equal(t, 2, calledCount)
		})
	}

	t.Run("Invalid Side", func(t *testing.T) {
		t.Parallel()
		client := binance.NewClient(nil, "", "api_key", "api_secret", config.LoggingConfig{})
		err := client.PlaceTPSL(context.Background(), exchange.TPSLRequest{
			Side: 999,
		})
		assert.Error(t, err)
	})
}

func TestClient_GetRecentClosedPnL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name            string
		orderResponse   string
		tradesResponse  string
		incomeResponse  string
		orderErr        bool
		expectedError   string
		expectedEntry   float64
		expectedExit    float64
		expectedSize    float64
		expectedGross   float64
		expectedFee     float64
		expectedFunding float64
		expectedNet     float64
	}{
		{
			name: "Standard",
			orderResponse: `{
				"orderId": 123456,
				"symbol": "BTCUSDT",
				"status": "FILLED",
				"clientOrderId": "ext_123",
				"side": "BUY"
			}`,
			tradesResponse: `[
				{
					"symbol": "BTCUSDT",
					"id": 1,
					"orderId": 123456,
					"price": "50000.0",
					"qty": "0.5",
					"commission": "1.5",
					"commissionAsset": "USDT",
					"realizedPnl": "0.0",
					"side": "BUY",
					"time": 1672531200000
				},
				{
					"symbol": "BTCUSDT",
					"id": 2,
					"orderId": 789012,
					"price": "52000.0",
					"qty": "0.5",
					"commission": "1.56",
					"commissionAsset": "USDT",
					"realizedPnl": "1000.0",
					"side": "SELL",
					"time": 1672531260000
				}
			]`,
			incomeResponse: `[
				{
					"symbol": "BTCUSDT",
					"incomeType": "FUNDING_FEE",
					"income": "-5.5",
					"asset": "USDT",
					"time": 1672531230000
				}
			]`,
			expectedEntry:   50000.0,
			expectedExit:    52000.0,
			expectedSize:    0.5,
			expectedGross:   1000.0,
			expectedFee:     3.06,
			expectedFunding: -5.5,
			expectedNet:     1000.0 - 3.06 - 5.5,
		},
		{
			name: "Capping and Sorting",
			orderResponse: `{
				"orderId": 123456,
				"symbol": "BTCUSDT",
				"status": "FILLED",
				"clientOrderId": "ext_123",
				"side": "BUY",
				"executedQty": "0.5"
			}`,
			tradesResponse: `[
				{
					"symbol": "BTCUSDT",
					"id": 3,
					"orderId": 789013,
					"price": "53000.0",
					"qty": "0.4",
					"commission": "2.0",
					"commissionAsset": "USDT",
					"realizedPnl": "1200.0",
					"side": "SELL",
					"time": 1672531260000
				},
				{
					"symbol": "BTCUSDT",
					"id": 2,
					"orderId": 789012,
					"price": "52000.0",
					"qty": "0.3",
					"commission": "1.0",
					"commissionAsset": "USDT",
					"realizedPnl": "600.0",
					"side": "SELL",
					"time": 1672531230000
				},
				{
					"symbol": "BTCUSDT",
					"id": 1,
					"orderId": 123456,
					"price": "50000.0",
					"qty": "0.5",
					"commission": "1.5",
					"commissionAsset": "USDT",
					"realizedPnl": "0.0",
					"side": "BUY",
					"time": 1672531200000
				}
			]`,
			incomeResponse:  `[]`,
			expectedEntry:   50000.0,
			expectedExit:    52571.428571,
			expectedSize:    0.7,
			expectedGross:   1800.0,
			expectedFee:     4.5,
			expectedFunding: 0.0,
			expectedNet:     1800.0 - 4.5,
		},
		{
			name: "Fallback (No closing trades found)",
			orderResponse: `{
				"orderId": 123456,
				"symbol": "BTCUSDT",
				"status": "FILLED",
				"clientOrderId": "ext_123",
				"side": "BUY"
			}`,
			tradesResponse: `[
				{
					"symbol": "BTCUSDT",
					"id": 1,
					"orderId": 123456,
					"price": "50000.0",
					"qty": "0.5",
					"commission": "1.5",
					"commissionAsset": "USDT",
					"realizedPnl": "0.0",
					"side": "BUY",
					"time": 1672531200000
				}
			]`,
			incomeResponse:  `[]`,
			expectedEntry:   50000.0,
			expectedExit:    50000.0,
			expectedSize:    0.5,
			expectedGross:   0.0,
			expectedFee:     1.5,
			expectedFunding: 0.0,
			expectedNet:     -1.5,
		},
		{
			name:          "Order Error",
			orderErr:      true,
			expectedError: "binance get order by external ID",
		},
		{
			name: "Parse Order ID Error",
			orderResponse: `{
				"symbol": "BTCUSDT",
				"status": "FILLED"
			}`,
			expectedError: "binance parse numeric order ID",
		},
		{
			name: "Zero Opening Qty Error",
			orderResponse: `{
				"orderId": 123456,
				"symbol": "BTCUSDT",
				"status": "FILLED",
				"clientOrderId": "ext_123",
				"side": "BUY"
			}`,
			tradesResponse: `[
				{
					"symbol": "BTCUSDT",
					"id": 1,
					"orderId": 999999,
					"price": "50000.0",
					"qty": "0.5",
					"commission": "1.5",
					"commissionAsset": "USDT",
					"realizedPnl": "0.0",
					"side": "BUY",
					"time": 1672531200000
				}
			]`,
			incomeResponse: `[]`,
			expectedError:  "zero opening quantity for order",
		},
		{
			name: "Short Position PreExisting",
			orderResponse: `{
				"orderId": 123456,
				"symbol": "BTCUSDT",
				"status": "FILLED",
				"clientOrderId": "ext_123",
				"side": "SELL",
				"executedQty": "0.5"
			}`,
			tradesResponse: `[
				{
					"symbol": "BTCUSDT",
					"id": 1,
					"orderId": 123456,
					"price": "50000.0",
					"qty": "0.5",
					"commission": "1.5",
					"commissionAsset": "USDT",
					"realizedPnl": "0.0",
					"side": "SELL",
					"time": 1672531200000
				},
				{
					"symbol": "BTCUSDT",
					"id": 2,
					"orderId": 789012,
					"price": "49000.0",
					"qty": "1.0",
					"commission": "1.5",
					"commissionAsset": "USDT",
					"realizedPnl": "2000.0",
					"side": "BUY",
					"time": 1672531260000
				}
			]`,
			incomeResponse:  `[]`,
			expectedEntry:   51000.0,
			expectedExit:    49000.0,
			expectedSize:    1.0,
			expectedGross:   2000.0,
			expectedFee:     3.0,
			expectedFunding: 0.0,
			expectedNet:     2000.0 - 3.0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if tc.orderErr {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"code": -1102, "msg": "Mandatory parameter was not sent"}`))
					return
				}
				switch r.URL.Path {
				case "/fapi/v1/order":
					_, _ = w.Write([]byte(tc.orderResponse))
				case "/fapi/v1/userTrades":
					_, _ = w.Write([]byte(tc.tradesResponse))
				case "/fapi/v1/income":
					_, _ = w.Write([]byte(tc.incomeResponse))
				}
			}))
			defer server.Close()

			client := binance.NewClient(server.Client(), server.URL, "api_key", "api_secret", config.LoggingConfig{})
			res, err := client.GetRecentClosedPnL(context.Background(), "BTCUSDT", "ext_123", time.UnixMilli(1672531100000))

			if tc.expectedError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.expectedError)
			} else {
				require.NoError(t, err)
				assert.Equal(t, "BTCUSDT", res.Symbol)
				assert.InDelta(t, tc.expectedEntry, res.EntryPrice, 0.0001)
				assert.InDelta(t, tc.expectedExit, res.ExitPrice, 0.0001)
				assert.InDelta(t, tc.expectedSize, res.ClosedSize, 0.0001)
				assert.InDelta(t, tc.expectedGross, res.GrossPnL, 0.0001)
				assert.InDelta(t, tc.expectedFee, res.Fee, 0.0001)
				assert.InDelta(t, tc.expectedFunding, res.FundingFee, 0.0001)
				assert.InDelta(t, tc.expectedNet, res.NetPnl, 0.0001)
			}
		})
	}
}

func TestDecompressionRoundTripper(t *testing.T) {
	t.Parallel()

	data := []byte(`{"serverTime": 1672531200000}`)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, _ = gz.Write(data)
	_ = gz.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "gzip", r.Header.Get("Accept-Encoding"))
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buf.Bytes())
	}))
	defer server.Close()

	client := binance.NewClient(
		server.Client(),
		server.URL,
		"api_key",
		"api_secret",
		config.LoggingConfig{HTTP: true},
	)

	timeVal, err := client.GetServerTime(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1672531200000), timeVal)
}
