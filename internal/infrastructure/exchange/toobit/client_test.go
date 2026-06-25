package toobit_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/toobit"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_GetTickers(t *testing.T) {
	t.Parallel()

	t.Run("success all symbols", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/quote/v1/contract/ticker/24hr", r.URL.Path)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[
				{
					"t": 1538725500422,
					"a": "1.1",
					"b": "1.0",
					"s": "BTC-SWAP-USDT",
					"c": "4.0",
					"o": "99.0",
					"h": "100.0",
					"l": "0.1",
					"v": "8913.3",
					"qv": "15.3",
					"pc": "1.0",
					"pcp": "2.0"
				}
			]`))
		}))
		defer server.Close()

		client := toobit.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
		tickers, err := client.GetTickers(context.Background(), "")
		require.NoError(t, err)
		require.Len(t, tickers, 1)

		assert.Equal(t, "BTC-SWAP-USDT", tickers[0].Symbol)
		assert.Equal(t, 4.0, tickers[0].LastPrice)
		assert.Equal(t, 1.0, tickers[0].Bid1)
		assert.Equal(t, 1.1, tickers[0].Ask1)
		assert.Equal(t, 8913.3, tickers[0].Volume24)
		assert.Equal(t, 15.3, tickers[0].AmountUSDT24)
		assert.Equal(t, int64(1538725500422), tickers[0].Timestamp)
	})

	t.Run("success single symbol", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/quote/v1/contract/ticker/24hr", r.URL.Path)
			assert.Equal(t, "BTC-SWAP-USDT", r.URL.Query().Get("symbol"))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[
				{
					"t": 1538725500422,
					"a": "1.1",
					"b": "1.0",
					"s": "BTC-SWAP-USDT",
					"c": "4.0",
					"o": "99.0",
					"h": "100.0",
					"l": "0.1",
					"v": "8913.3",
					"qv": "15.3",
					"pc": "1.0",
					"pcp": "2.0"
				}
			]`))
		}))
		defer server.Close()

		client := toobit.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
		tickers, err := client.GetTickers(context.Background(), "BTC-SWAP-USDT")
		require.NoError(t, err)
		require.Len(t, tickers, 1)
		assert.Equal(t, "BTC-SWAP-USDT", tickers[0].Symbol)
	})

	t.Run("http error status", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`bad request details`))
		}))
		defer server.Close()

		client := toobit.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
		_, err := client.GetTickers(context.Background(), "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "HTTP error 400: bad request details")
	})

	t.Run("unmarshal error", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{invalid json`))
		}))
		defer server.Close()

		client := toobit.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
		_, err := client.GetTickers(context.Background(), "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unmarshal tickers")
	})
}

func TestClient_GetFundingRates(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/v1/futures/fundingRate", r.URL.Path)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[
				{
					"symbol": "BTC-SWAP-USDT",
					"rate": "0.00180991",
					"period": "8H",
					"nextFundingTime": 1668427200000,
					"interest": "0.0001",
					"fundingRateCap": "0.003",
					"fundingRateFloor": "-0.003"
				},
				{
					"symbol": "ETH-SWAP-USDT",
					"rate": "-0.0005",
					"period": "8H",
					"nextFundingTime": 1668427200000,
					"interest": "0.0001",
					"fundingRateCap": "0.003",
					"fundingRateFloor": "-0.003"
				}
			]`))
		}))
		defer server.Close()

		client := toobit.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
		rates, err := client.GetFundingRates(context.Background(), []string{"BTC-SWAP-USDT", "ETH-SWAP-USDT", "SOL-SWAP-USDT"})
		require.NoError(t, err)
		require.Len(t, rates, 2)

		assert.Equal(t, "BTC-SWAP-USDT", rates[0].Symbol)
		assert.Equal(t, 0.00180991, rates[0].Rate)
		assert.Equal(t, int64(1668427200000), rates[0].SettleTime)

		assert.Equal(t, "ETH-SWAP-USDT", rates[1].Symbol)
		assert.Equal(t, -0.0005, rates[1].Rate)
		assert.Equal(t, int64(1668427200000), rates[1].SettleTime)
	})

	t.Run("empty symbols", func(t *testing.T) {
		t.Parallel()

		client := toobit.NewClient(nil, "https://api.toobit.com", "key", "secret", config.LoggingConfig{})
		rates, err := client.GetFundingRates(context.Background(), nil)
		require.NoError(t, err)
		assert.Nil(t, rates)
	})

	t.Run("unmarshal error", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{invalid json`))
		}))
		defer server.Close()

		client := toobit.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
		_, err := client.GetFundingRates(context.Background(), []string{"BTC-SWAP-USDT"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unmarshal funding rates")
	})
}

func TestClient_GetServerTime(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/time", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"serverTime": 1700000000000}`))
	}))
	defer server.Close()

	client := toobit.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	ts, err := client.GetServerTime(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1700000000000), ts)
}

func TestClient_GetContractDetails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/exchangeInfo", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"timezone": "UTC",
			"contracts": [
				{
					"symbol": "BTC-SWAP-USDT",
					"baseAsset": "BTC",
					"quoteAsset": "USDT",
					"marginAsset": "USDT",
					"contractMultiplier": "0.0001",
					"filters": [
						{
							"filterType": "PRICE_FILTER",
							"tickSize": "0.1"
						},
						{
							"filterType": "LOT_SIZE",
							"minQty": "1",
							"stepSize": "1"
						}
					]
				}
			]
		}`))
	}))
	defer server.Close()

	client := toobit.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	details, err := client.GetContractDetails(context.Background())
	require.NoError(t, err)
	require.Len(t, details, 1)

	assert.Equal(t, "BTC-SWAP-USDT", details[0].Symbol)
	assert.Equal(t, 0.0001, details[0].ContractSize)
	assert.Equal(t, 1, details[0].VolUnit)
	assert.Equal(t, 1, details[0].PriceScale)
	assert.Equal(t, 0, details[0].VolScale)
}

func TestClient_GetPotentialFundingSymbols(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/quote/v1/contract/ticker/24hr" {
			_, _ = w.Write([]byte(`[
				{
					"t": 1538725500422,
					"a": "1.1",
					"b": "1.0",
					"s": "BTC-SWAP-USDT",
					"c": "4.0",
					"v": "8913.3",
					"qv": "5000000.0"
				}
			]`))
		} else {
			_, _ = w.Write([]byte(`[
				{
					"symbol": "BTC-SWAP-USDT",
					"rate": "0.001",
					"nextFundingTime": 1668427200000
				}
			]`))
		}
	}))
	defer server.Close()

	client := toobit.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	res, err := client.GetPotentialFundingSymbols(context.Background(), 1000000, 10000000, []string{"BTC-SWAP-USDT"}, nil)
	require.NoError(t, err)
	require.Len(t, res, 1)
	assert.Equal(t, "BTC-SWAP-USDT", res[0].Symbol)
	assert.Equal(t, 0.001, res[0].Rate)
	assert.Equal(t, 5000000.0, res[0].Volume24h)
}

func TestClient_CreateOrder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		orderType    domain.OrderType
		expectedType string
		expectedTif  string
	}{
		{
			name:         "limit GTC",
			orderType:    exchange.OrderTypeLimit,
			expectedType: "LIMIT",
			expectedTif:  "GTC",
		},
		{
			name:         "post only",
			orderType:    exchange.OrderTypePostOnly,
			expectedType: "LIMIT",
			expectedTif:  "POST_ONLY",
		},
		{
			name:         "IOC",
			orderType:    exchange.OrderTypeIOC,
			expectedType: "LIMIT",
			expectedTif:  "IOC",
		},
		{
			name:         "FOK",
			orderType:    exchange.OrderTypeFOK,
			expectedType: "LIMIT",
			expectedTif:  "FOK",
		},
		{
			name:         "market",
			orderType:    exchange.OrderTypeMarket,
			expectedType: "MARKET",
			expectedTif:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/api/v2/futures/order", r.URL.Path)
				assert.Equal(t, "POST", r.Method)

				err := r.ParseForm()
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedType, r.Form.Get("type"))
				assert.Equal(t, "1", r.Form.Get("quantity"))
				assert.Empty(t, r.Form.Get("qty"))
				if tt.expectedTif != "" {
					assert.Equal(t, tt.expectedTif, r.Form.Get("timeInForce"))
				} else {
					assert.Empty(t, r.Form.Get("timeInForce"))
				}

				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{
					"code": 200,
					"msg": "success",
					"data": {
						"orderId": "order-12345",
						"clientOrderId": "client-abc"
					}
				}`))
			}))
			defer server.Close()

			client := toobit.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
			res, err := client.CreateOrder(context.Background(), exchange.SubmitOrderRequest{
				Symbol:          "BTC-SWAP-USDT",
				Vol:             1.0,
				Side:            exchange.SideOpenLong,
				Type:            tt.orderType,
				Price:           60000.0,
				StopLossPrice:   58000.0,
				TakeProfitPrice: 62000.0,
			})
			require.NoError(t, err)
			assert.Equal(t, "order-12345", res.OrderID)
			assert.True(t, res.TPSLSubmitted)
		})
	}
}

func TestClient_CancelOrder(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v2/futures/order", r.URL.Path)
		assert.Equal(t, "DELETE", r.Method)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code": "200", "msg": "success", "data": null}`))
	}))
	defer server.Close()

	client := toobit.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	err := client.CancelOrder(context.Background(), "BTC-SWAP-USDT", "order-12345")
	require.NoError(t, err)
}

func TestClient_CancelOrders(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/futures/cancelOrderByIds", r.URL.Path)
		assert.Equal(t, "DELETE", r.Method)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code": 200, "msg": "success"}`))
	}))
	defer server.Close()

	client := toobit.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	err := client.CancelOrders(context.Background(), []string{"order-1", "order-2"})
	require.NoError(t, err)
}

func TestClient_CancelAllOpenOrders(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/futures/batchOrders", r.URL.Path)
		assert.Equal(t, "DELETE", r.Method)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code": 200, "msg": "success"}`))
	}))
	defer server.Close()

	client := toobit.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	err := client.CancelAllOpenOrders(context.Background(), "BTC-SWAP-USDT")
	require.NoError(t, err)
}

func TestClient_GetOrder_And_GetOrderByExternalID(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/futures/order", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code": 200,
			"msg": "success",
			"data": {
				"orderId": "order-123",
				"symbol": "BTC-SWAP-USDT",
				"price": "60000",
				"origQty": "1.5",
				"avgPrice": "59950",
				"executedQty": "1.0",
				"status": "PARTIALLY_FILLED",
				"clientOrderId": "client-123",
				"side": "BUY_OPEN",
				"type": "LIMIT",
				"time": 1700000000000
			}
		}`))
	}))
	defer server.Close()

	client := toobit.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	ord, err := client.GetOrder(context.Background(), "BTC-SWAP-USDT", "order-123")
	require.NoError(t, err)
	assert.Equal(t, "order-123", ord.OrderID)
	assert.Equal(t, domain.OrderStatePartiallyFilled, ord.State)

	ordExt, err := client.GetOrderByExternalID(context.Background(), "BTC-SWAP-USDT", "client-123")
	require.NoError(t, err)
	assert.Equal(t, "client-123", ordExt.ExternalOID)
}

func TestClient_GetOpenOrders(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/futures/openOrders", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[
			{
				"orderId": "order-123",
				"symbol": "BTC-SWAP-USDT",
				"price": "60000",
				"origQty": "1.5",
				"status": "NEW"
			}
		]`))
	}))
	defer server.Close()

	client := toobit.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	orders, err := client.GetOpenOrders(context.Background(), "BTC-SWAP-USDT")
	require.NoError(t, err)
	require.Len(t, orders, 1)
	assert.Equal(t, "order-123", orders[0].OrderID)
}

func TestClient_GetOpenPositions(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/futures/positions", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code": 200,
			"msg": "success",
			"data": [
				{
					"symbol": "BTC-SWAP-USDT",
					"side": "LONG",
					"avgPrice": "55000",
					"position": "0.5",
					"unrealizedPnl": "250.0"
				}
			]
		}`))
	}))
	defer server.Close()

	client := toobit.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	positions, err := client.GetOpenPositions(context.Background(), "BTC-SWAP-USDT")
	require.NoError(t, err)
	require.Len(t, positions, 1)

	assert.Equal(t, "BTC-SWAP-USDT", positions[0].Symbol)
	assert.Equal(t, 0.5, positions[0].HoldVol)
	assert.Equal(t, exchange.PositionTypeLong, positions[0].PositionType)
}

func TestClient_ChangeLeverage_And_SwitchMarginMode(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code": 200, "msg": "success"}`))
	}))
	defer server.Close()

	client := toobit.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	err := client.ChangeLeverage(context.Background(), exchange.ChangeLeverageRequest{
		Symbol:   "BTC-SWAP-USDT",
		Leverage: 10,
	})
	require.NoError(t, err)

	err = client.SwitchMarginMode(context.Background(), "BTC-SWAP-USDT", "ISOLATED", 10, exchange.SideOpenLong)
	require.NoError(t, err)
}

func TestClient_ListenKeys(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"listenKey": "lk-123"}`))
	}))
	defer server.Close()

	client := toobit.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	lk, err := client.CreateListenKey(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "lk-123", lk)

	err = client.KeepAliveListenKey(context.Background(), lk)
	require.NoError(t, err)
}

func TestClient_Helpers_And_Errors(t *testing.T) {
	t.Parallel()

	// 1. SupportLeverageOnOrder & WarmUp
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"serverTime": 12345}`))
	}))
	defer server.Close()

	client := toobit.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	assert.False(t, client.SupportLeverageOnOrder())
	client.WarmUp(context.Background(), time.Second)

	// 2. parseResponse error paths
	serverErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code": 400, "msg": "failed parameters"}`))
	}))
	defer serverErr.Close()

	clientErr := toobit.NewClient(serverErr.Client(), serverErr.URL, "key", "secret", config.LoggingConfig{})
	_, err := clientErr.CreateListenKey(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error code 400: failed parameters")

	// 3. rate limit error path
	serverRate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`Too many requests`))
	}))
	defer serverRate.Close()

	clientRate := toobit.NewClient(serverRate.Client(), serverRate.URL, "key", "secret", config.LoggingConfig{})
	_, err = clientRate.GetServerTime(context.Background())
	require.Error(t, err)

	// 4. ClosePosition and CloseAllPositions
	serverClose := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/futures/positions" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"code": 200, "data": [{"symbol": "BTC-SWAP-USDT", "side": "SHORT", "avgPrice": "50000", "position": "1.0"}]}`))
		} else {
			_ = r.ParseForm()
			clientOid := r.FormValue("newClientOrderId")
			if clientOid == "" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"code": -1004, "msg": "Missing required parameter 'newClientOrderId'"}`))
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"code": 200, "data": {"orderId": "order-close"}}`))
		}
	}))
	defer serverClose.Close()

	clientClose := toobit.NewClient(serverClose.Client(), serverClose.URL, "key", "secret", config.LoggingConfig{})
	err = clientClose.ClosePosition(context.Background(), "BTC-SWAP-USDT", domain.SideCloseShort, 1.0, domain.PositionModeHedge)
	require.NoError(t, err)

	err = clientClose.CloseAllPositions(context.Background(), "BTC-SWAP-USDT")
	require.NoError(t, err)

	// 5. CloseAllPositions error propagation
	serverCloseErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/futures/positions" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"code": 200, "data": [{"symbol": "BTC-SWAP-USDT", "side": "SHORT", "avgPrice": "50000", "position": "1.0"}]}`))
		} else {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code": -1004, "msg": "failed to place order"}`))
		}
	}))
	defer serverCloseErr.Close()

	clientCloseErr := toobit.NewClient(serverCloseErr.Client(), serverCloseErr.URL, "key", "secret", config.LoggingConfig{})
	err = clientCloseErr.CloseAllPositions(context.Background(), "BTC-SWAP-USDT")
	require.Error(t, err)
}

func TestClient_RawRequests(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code": 200, "msg": "success", "data": "raw"}`))
	}))
	defer server.Close()

	client := toobit.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})

	// 1. GetFundingRateRaw
	res, err := client.GetFundingRateRaw(context.Background(), map[string]string{"symbol": "BTC-SWAP-USDT"})
	require.NoError(t, err)
	assert.Contains(t, string(res), "raw")

	// 2. GetTickersRaw
	res, err = client.GetTickersRaw(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, string(res), "raw")

	// 3. GetOpenPositionsRaw
	res, err = client.GetOpenPositionsRaw(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, string(res), "raw")

	// 4. GetHistoryPositionsRaw
	res, err = client.GetHistoryPositionsRaw(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, string(res), "raw")

	// 5. GetOrderDetailRaw
	res, err = client.GetOrderDetailRaw(context.Background(), "order-123", nil)
	require.NoError(t, err)
	assert.Contains(t, string(res), "raw")

	// 6. GetHistoryOrdersRaw
	res, err = client.GetHistoryOrdersRaw(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, string(res), "raw")

	// 7. GetFuturesBalanceFlowRaw
	res, err = client.GetFuturesBalanceFlowRaw(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, string(res), "raw")

	// 8. GetOrderPNLRaw
	serverPNL := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		switch r.URL.Path {
		case "/api/v1/futures/order":
			_, _ = w.Write([]byte(`{"code": "200", "data": {"orderId": "close-order-123", "status": "FILLED", "side": "SELL_CLOSE", "time": 1579007187214}}`))
		case "/api/v1/futures/historyPositions":
			_, _ = w.Write([]byte(`{"code": "200", "data": [{"symbol": "BTC-SWAP-USDT", "side": "LONG", "openAvgPrice": "24000", "closeAvgPrice": "25000", "closeTotalQty": "10", "realizedPnL": "1000", "realizedPnlWithoutFee": "1000.5", "openFee": "0.1", "closeFee": "0.1"}]}`))
		case "/api/v1/futures/balanceFlow":
			_, _ = w.Write([]byte(`{"code": "200", "data": [{"id": 1, "coin": "USDT", "flowTypeValue": 32, "change": "-0.3"}]}`))
		}
	}))
	defer serverPNL.Close()

	clientPNL := toobit.NewClient(serverPNL.Client(), serverPNL.URL, "key", "secret", config.LoggingConfig{})
	res, err = clientPNL.GetOrderPNLRaw(context.Background(), map[string]string{"symbol": "BTC-SWAP-USDT", "order_id": "close-order-123"})
	require.NoError(t, err)
	assert.Contains(t, string(res), "toobit")
}

func TestClient_GetOrderPNL(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		switch r.URL.Path {
		case "/api/v1/futures/order":
			_, _ = w.Write([]byte(`{
				"code": "200",
				"data": {
					"orderId": "close-order-123",
					"symbol": "BTC-SWAP-USDT",
					"status": "FILLED",
					"side": "SELL_CLOSE",
					"origQty": "10",
					"avgPrice": "25000",
					"time": 1579007187214
				}
			}`))
		case "/api/v1/futures/historyPositions":
			_, _ = w.Write([]byte(`{
				"code": "200",
				"data": [
					{
						"symbol": "BTC-SWAP-USDT",
						"side": "LONG",
						"position": "10",
						"closeTotalQty": "10",
						"realizedPnL": "1000",
						"realizedPnlRate": "0.0042",
						"realizedPnlWithoutFee": "1000.5",
						"status": "CLOSED",
						"openAvgPrice": "24000",
						"closeAvgPrice": "25000",
						"openFee": "-0.1",
						"closeFee": "-0.1",
						"openTime": 1579007187214,
						"closeTime": 1579093587214,
						"id": "1000"
					}
				]
			}`))
		case "/api/v1/futures/balanceFlow":
			_, _ = w.Write([]byte(`{
				"code": "200",
				"data": [
					{
						"id": 539870570957903104,
						"coin": "USDT",
						"flowTypeValue": 32,
						"change": "-0.3",
						"created": 1579093587214
					}
				]
			}`))
		}
	}))
	defer server.Close()

	client := toobit.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	info, err := client.GetOrderPNL(context.Background(), "BTC-SWAP-USDT", "close-order-123")
	require.NoError(t, err)

	assert.Equal(t, "toobit", info.Exchange)
	assert.Equal(t, "BTC-SWAP-USDT", info.Symbol)
	assert.Equal(t, 24000.0, info.EntryPrice)
	assert.Equal(t, 25000.0, info.ExitPrice)
	assert.Equal(t, 10.0, info.ClosedSize)
	assert.Equal(t, 1000.5, info.GrossPnL)
	assert.Equal(t, 0.2, info.Fee)
	assert.Equal(t, -0.3, info.FundingFee)
	assert.Equal(t, int64(86400000), info.DurationMs)
	assert.Equal(t, 1000.0, info.NetPnl)
	assert.InDelta(t, 4.16666667, info.PnLRate, 0.0001)
}
