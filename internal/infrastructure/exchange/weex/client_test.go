package weex_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/weex"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var NewClient = weex.NewClient

type mockClock struct {
	now time.Time
}

func (m mockClock) Now() time.Time {
	return m.now
}

func TestClient_Signature(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/capi/v3/order", r.URL.Path)
		assert.Equal(t, "my-api-key", r.Header.Get("ACCESS-KEY"))
		assert.Equal(t, "my-passphrase", r.Header.Get("ACCESS-PASSPHRASE"))
		assert.Equal(t, "10002000", r.Header.Get("ACCESS-TIMESTAMP"))

		// Message to sign: 10002000POST/capi/v3/order{"symbol":"BTCUSDT"}
		// HMAC-SHA256 of message with secret "my-api-secret":
		// Message = 10002000POST/capi/v3/order{"symbol":"BTCUSDT"}
		// secret = my-api-secret
		// Expected ACCESS-SIGN header
		assert.NotEmpty(t, r.Header.Get("ACCESS-SIGN"))

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":"00000","msg":"success","data":{}}`))
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "my-api-key", "my-api-secret", "my-passphrase", config.LoggingConfig{})
	client.SetClock(mockClock{now: time.UnixMilli(10002000)})

	res, err := client.RawRequest(context.Background(), "POST", "/capi/v3/order", nil, []byte(`{"symbol": "BTCUSDT"}`))
	require.NoError(t, err)
	assert.NotEmpty(t, res)
}

func TestClient_GetTickers(t *testing.T) {
	t.Parallel()

	t.Run("success all symbols", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/capi/v3/market/ticker/24hr", r.URL.Path)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[
				{
					"symbol": "BTCUSDT",
					"priceChange": "150.5",
					"priceChangePercent": "0.22",
					"lastPrice": "69350.5",
					"openPrice": "69200.0",
					"highPrice": "69980.0",
					"lowPrice": "68888.0",
					"volume": "1234.567",
					"quoteVolume": "85679012.45",
					"markPrice": "69348.7",
					"indexPrice": "69347.9",
					"openTime": 1764419370000,
					"closeTime": 1764505770000
				}
			]`))
		}))
		defer server.Close()

		client := NewClient(server.Client(), server.URL, "", "", "", config.LoggingConfig{})
		tickers, err := client.GetTickers(context.Background(), "")
		require.NoError(t, err)
		require.Len(t, tickers, 1)

		assert.Equal(t, "BTCUSDT", tickers[0].Symbol)
		assert.Equal(t, 69350.5, tickers[0].LastPrice)
		assert.Equal(t, 1234.567, tickers[0].Volume24)
		assert.Equal(t, 85679012.45, tickers[0].AmountUSDT24)
		assert.Equal(t, int64(1764505770000), tickers[0].Timestamp)
	})

	t.Run("success single symbol", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/capi/v3/market/ticker/24hr", r.URL.Path)
			assert.Equal(t, "BTCUSDT", r.URL.Query().Get("symbol"))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[
				{
					"symbol": "BTCUSDT",
					"priceChange": "150.5",
					"priceChangePercent": "0.22",
					"lastPrice": "69350.5",
					"openPrice": "69200.0",
					"highPrice": "69980.0",
					"lowPrice": "68888.0",
					"volume": "1234.567",
					"quoteVolume": "85679012.45",
					"markPrice": "69348.7",
					"indexPrice": "69347.9",
					"openTime": 1764419370000,
					"closeTime": 1764505770000
				}
			]`))
		}))
		defer server.Close()

		client := NewClient(server.Client(), server.URL, "", "", "", config.LoggingConfig{})
		tickers, err := client.GetTickers(context.Background(), "BTCUSDT")
		require.NoError(t, err)
		require.Len(t, tickers, 1)
		assert.Equal(t, "BTCUSDT", tickers[0].Symbol)
	})
}

func TestClient_GetFundingRates(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/capi/v3/market/premiumIndex", r.URL.Path)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[
				{
					"symbol": "BTCUSDT",
					"markPrice": "69348.6",
					"indexPrice": "69347.9",
					"lastFundingRate": "0.00025",
					"forecastFundingRate": "0.00025",
					"interestRate": "0.0001",
					"nextFundingTime": 1764510000000,
					"time": 1764505777345,
					"collectCycle": 480
				}
			]`))
		}))
		defer server.Close()

		client := NewClient(server.Client(), server.URL, "", "", "", config.LoggingConfig{})
		rates, err := client.GetFundingRates(context.Background(), []string{"BTCUSDT"})
		require.NoError(t, err)
		require.Len(t, rates, 1)

		assert.Equal(t, "BTCUSDT", rates[0].Symbol)
		assert.Equal(t, 0.00025, rates[0].Rate)
		assert.Equal(t, int64(1764510000000), rates[0].SettleTime)
	})

	t.Run("empty symbols", func(t *testing.T) {
		t.Parallel()

		client := NewClient(nil, "https://api-contract.weex.com", "", "", "", config.LoggingConfig{})
		rates, err := client.GetFundingRates(context.Background(), nil)
		require.NoError(t, err)
		assert.Nil(t, rates)
	})
}

func TestClient_GetServerTime(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/capi/v3/market/time", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":"00000","msg":"success","data":"1764505777345"}`))
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "", "", "", config.LoggingConfig{})
	timeMs, err := client.GetServerTime(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1764505777345), timeMs)
}

func TestClient_GetContractDetails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/capi/v3/market/exchangeInfo", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code":"00000",
			"msg":"success",
			"data":{
				"symbols":[
					{
						"symbol": "BTCUSDT",
						"baseAsset": "BTC",
						"quoteAsset": "USDT",
						"marginAsset": "USDT",
						"pricePrecision": 1,
						"quantityPrecision": 3,
						"minOrderSize": "0.001",
						"maxLeverage": 125
					}
				]
			}
		}`))
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "", "", "", config.LoggingConfig{})
	details, err := client.GetContractDetails(context.Background())
	require.NoError(t, err)
	require.Len(t, details, 1)

	assert.Equal(t, "BTCUSDT", details[0].Symbol)
	assert.Equal(t, "BTC", details[0].BaseCoin)
	assert.Equal(t, "USDT", details[0].QuoteCoin)
	assert.Equal(t, 125, details[0].MaxLeverage)
	assert.Equal(t, 1, details[0].PriceScale)
	assert.Equal(t, 3, details[0].VolScale)
	assert.Equal(t, 0, details[0].MinVol) // int(0.001) is 0
}

func TestClient_CreateOrder(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/capi/v3/order", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code":"00000",
			"msg":"success",
			"data":{
				"orderId": "order-12345",
				"clientOrderId": "client-123",
				"success": true
			}
		}`))
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "", "", "", config.LoggingConfig{})
	res, err := client.CreateOrder(context.Background(), exchange.SubmitOrderRequest{
		Symbol:          "BTCUSDT",
		Price:           69000,
		Vol:             0.1,
		Side:            domain.SideOpenLong,
		Type:            exchange.OrderTypeLimit,
		TakeProfitPrice: 70000,
		StopLossPrice:   68000,
	})
	require.NoError(t, err)
	assert.Equal(t, "order-12345", res.OrderID)
	assert.True(t, res.TPSLSubmitted)
}

func TestClient_CancelOrder(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/capi/v3/order", r.URL.Path)
		assert.Equal(t, "BTCUSDT", r.URL.Query().Get("symbol"))
		assert.Equal(t, "order-12345", r.URL.Query().Get("orderId"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":"00000","msg":"success"}`))
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "", "", "", config.LoggingConfig{})
	err := client.CancelOrder(context.Background(), "BTCUSDT", "order-12345")
	require.NoError(t, err)
}

func TestClient_CancelOrders(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/capi/v3/batchOrders", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":"00000","msg":"success"}`))
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "", "", "", config.LoggingConfig{})
	err := client.CancelOrders(context.Background(), []string{"order-1", "order-2"})
	require.NoError(t, err)
}

func TestClient_CancelAllOpenOrders(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/capi/v3/allOpenOrders", r.URL.Path)
		assert.Equal(t, "BTCUSDT", r.URL.Query().Get("symbol"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":"00000","msg":"success"}`))
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "", "", "", config.LoggingConfig{})
	err := client.CancelAllOpenOrders(context.Background(), "BTCUSDT")
	require.NoError(t, err)
}

func TestClient_GetOrder(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/capi/v3/order", r.URL.Path)
		assert.Empty(t, r.URL.Query().Get("symbol"))
		assert.Equal(t, "order-12345", r.URL.Query().Get("orderId"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code":"00000",
			"msg":"success",
			"data":{
				"orderId": "order-12345",
				"symbol": "BTCUSDT",
				"price": "69000",
				"origQty": "0.1",
				"executedQty": "0.1",
				"avgPrice": "69000",
				"status": "FILLED",
				"side": "BUY",
				"positionSide": "LONG"
			}
		}`))
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "", "", "", config.LoggingConfig{})
	ord, err := client.GetOrder(context.Background(), "BTCUSDT", "order-12345")
	require.NoError(t, err)
	assert.Equal(t, "order-12345", ord.OrderID)
	assert.Equal(t, exchange.OrderStateFilled, ord.State)
}

func TestClient_GetOrderByExternalID(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/capi/v3/openOrders" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"code":"00000","msg":"success","data":[]}`))
			return
		}
		if r.URL.Path == "/capi/v3/order/history" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"code":"00000",
				"msg":"success",
				"data":[
					{
						"orderId": "order-12345",
						"clientOrderId": "client-123",
						"symbol": "BTCUSDT",
						"price": "69000",
						"origQty": "0.1",
						"executedQty": "0.1",
						"avgPrice": "69000",
						"status": "FILLED",
						"side": "BUY",
						"positionSide": "LONG",
						"time": 1764505777345,
						"updateTime": 1764505777345
					}
				]
			}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "", "", "", config.LoggingConfig{})
	ord, err := client.GetOrderByExternalID(context.Background(), "BTCUSDT", "client-123")
	require.NoError(t, err)
	assert.Equal(t, "order-12345", ord.OrderID)
	assert.Equal(t, "client-123", ord.ExternalOID)
	assert.Equal(t, exchange.OrderStateFilled, ord.State)
}

func TestClient_GetOpenOrders(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/capi/v3/openOrders", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code":"00000",
			"msg":"success",
			"data":[
				{
					"orderId": "order-12345",
					"clientOrderId": "client-123",
					"symbol": "BTCUSDT",
					"price": "69000",
					"origQty": "0.1",
					"executedQty": "0.0",
					"avgPrice": "0.0",
					"status": "NEW",
					"side": "BUY",
					"positionSide": "LONG",
					"time": 1764505777345,
					"updateTime": 1764505777345
				}
			]
		}`))
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "", "", "", config.LoggingConfig{})
	orders, err := client.GetOpenOrders(context.Background(), "BTCUSDT")
	require.NoError(t, err)
	require.Len(t, orders, 1)
	assert.Equal(t, "order-12345", orders[0].OrderID)
	assert.Equal(t, exchange.OrderStateNew, orders[0].State)
}

func TestClient_GetOpenPositions(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/capi/v3/account/position/allPosition", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code":"00000",
			"msg":"success",
			"data":[
				{
					"symbol": "BTCUSDT",
					"side": "LONG",
					"size": "0.5",
					"leverage": "20",
					"entryPrice": "68500.0"
				}
			]
		}`))
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "", "", "", config.LoggingConfig{})
	positions, err := client.GetOpenPositions(context.Background(), "BTCUSDT")
	require.NoError(t, err)
	require.Len(t, positions, 1)

	assert.Equal(t, "BTCUSDT", positions[0].Symbol)
	assert.Equal(t, 0.5, positions[0].HoldVolContract)
	assert.Equal(t, exchange.PositionTypeLong, positions[0].PositionType)
	assert.Equal(t, 20, positions[0].Leverage)
}

func TestClient_ClosePosition(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/capi/v3/order", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code":"00000",
			"msg":"success",
			"data":{
				"orderId": "order-close",
				"clientOrderId": "client-close"
			}
		}`))
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "", "", "", config.LoggingConfig{})
	err := client.ClosePosition(context.Background(), "BTCUSDT", domain.SideCloseLong, 0.5, 1, 20)
	require.NoError(t, err)
}

func TestClient_CloseAllPositions(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/capi/v3/closePositions", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code":"00000",
			"msg":"success",
			"data":[
				{
					"positionId": 689987235755328154,
					"success": true,
					"successOrderId": 702345678901234580,
					"errorMessage": ""
				}
			]
		}`))
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "", "", "", config.LoggingConfig{})
	err := client.CloseAllPositions(context.Background(), "BTCUSDT")
	require.NoError(t, err)
}

func TestClient_ChangeLeverage(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/capi/v3/account/leverage", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":"00000","msg":"success"}`))
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "", "", "", config.LoggingConfig{})
	err := client.ChangeLeverage(context.Background(), exchange.ChangeLeverageRequest{
		Symbol:   "BTCUSDT",
		Leverage: 15,
	})
	require.NoError(t, err)
}

func TestClient_SwitchMarginMode(t *testing.T) {
	t.Parallel()

	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/capi/v3/account/marginType", r.URL.Path)
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"code":-1054,"msg":"FAILED_PRECONDITION: The contract does not support the 'COMBINED' separated mode"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":"200","msg":"success"}`))
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "", "", "", config.LoggingConfig{})
	err := client.SwitchMarginMode(context.Background(), "BTCUSDT", "CROSS", 15, domain.SideOpenLong)
	require.NoError(t, err)
	assert.Equal(t, 2, attempts)
}

func TestClient_GetOrderPNL(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if r.URL.Path == "/capi/v3/order" {
			_, _ = w.Write([]byte(`{
				"code": "00000",
				"msg": "success",
				"data": {
					"orderId": "12345",
					"clientOrderId": "client-123",
					"status": "FILLED",
					"side": "BUY",
					"positionSide": "LONG",
					"time": 1764505600000,
					"updateTime": 1764505602000
				}
			}`))
			return
		}
		if r.URL.Path == "/capi/v3/userTrades" {
			assert.Empty(t, r.URL.Query().Get("orderId"))
			assert.Equal(t, "1764505600000", r.URL.Query().Get("startTime"))
			_, _ = w.Write([]byte(`{
				"code": "00000",
				"msg": "success",
				"data": [
					{
						"id": 801234567890123000,
						"orderId": 12345,
						"symbol": "BTCUSDT",
						"price": "64000",
						"qty": "0.01",
						"quoteQty": "640",
						"realizedPnl": "0.0",
						"commission": "0.128",
						"time": 1764505600000,
						"side": "BUY",
						"positionSide": "LONG"
					},
					{
						"id": 801234567890123456,
						"orderId": 99999,
						"symbol": "BTCUSDT",
						"price": "69000",
						"qty": "0.01",
						"quoteQty": "690",
						"realizedPnl": "50.0",
						"commission": "0.138",
						"time": 1764505701456,
						"side": "SELL",
						"positionSide": "LONG"
					}
				]
			}`))
			return
		}
		if r.URL.Path == "/capi/v3/account/income" {
			_, _ = w.Write([]byte(`{
				"code": "00000",
				"msg": "success",
				"data": {
					"hasNextPage": false,
					"items": [
						{
							"billId": 686960019383517338,
							"asset": "USDT",
							"symbol": "BTCUSDT",
							"income": "-0.05",
							"incomeType": "position_funding",
							"time": 1764505701000
						}
					]
				}
			}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "", "", "", config.LoggingConfig{})
	res, err := client.GetOrderPNL(context.Background(), "BTCUSDT", "12345")
	require.NoError(t, err)
	assert.Equal(t, 69000.0, res.ExitPrice)
	assert.Equal(t, 64000.0, res.EntryPrice) // exit - (pnl / qty) = 69000 - (50 / 0.01) = 69000 - 5000 = 64000
	assert.Equal(t, 50.0, res.GrossPnL)
	assert.Equal(t, 0.266, res.Fee)
	assert.Equal(t, -0.05, res.FundingFee)
	assert.InDelta(t, 49.684, res.NetPnl, 1e-9) // 50.0 - 0.266 + (-0.05) = 49.684
}

func TestClient_WeexRemainingMethods(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/capi/v3/market/ticker/24hr":
			_, _ = w.Write([]byte(`{
				"code": "00000",
				"msg": "success",
				"data": [
					{
						"symbol": "BTCUSDT",
						"lastPrice": "64000",
						"volume": "100.5",
						"quoteVolume": "15000000",
						"closeTime": 1700000000000
					}
				]
			}`))
		case "/capi/v3/market/premiumIndex":
			_, _ = w.Write([]byte(`{
				"code": "00000",
				"msg": "success",
				"data": [
					{
						"symbol": "BTCUSDT",
						"lastFundingRate": "0.001",
						"nextFundingTime": "1700000000"
					}
				]
			}`))
		}
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})

	// 1. GetPotentialFundingSymbols
	res, err := client.GetPotentialFundingSymbols(context.Background(), 10000000, 0, nil, nil)
	require.NoError(t, err)
	require.Len(t, res, 1)
	assert.Equal(t, "BTCUSDT", res[0].Symbol)

	// 2. SupportLeverageOnOrder
	assert.False(t, client.SupportLeverageOnOrder())

	// 3. WarmUp
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client.WarmUp(ctx, 10*time.Millisecond)

	// 4. Trigger toAPIError
	serverErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"12345","msg":"api error message"}`))
	}))
	defer serverErr.Close()
	clientErr := NewClient(serverErr.Client(), serverErr.URL, "key", "secret", "pass", config.LoggingConfig{})
	_, err = clientErr.GetPotentialFundingSymbols(context.Background(), 10000000, 0, nil, nil)
	assert.ErrorContains(t, err, "api error message")
}

func TestClient_RawMethods(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":"00000","msg":"success","data":"raw-data"}`))
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	ctx := context.Background()

	res, err := client.GetFundingRateRaw(ctx, nil)
	require.NoError(t, err)
	assert.Contains(t, string(res), "raw-data")

	res, err = client.GetTickersRaw(ctx, nil)
	require.NoError(t, err)
	assert.Contains(t, string(res), "raw-data")

	res, err = client.GetOpenPositionsRaw(ctx, nil)
	require.NoError(t, err)
	assert.Contains(t, string(res), "raw-data")

	res, err = client.GetHistoryPositionsRaw(ctx, nil)
	require.NoError(t, err)
	assert.Contains(t, string(res), "raw-data")

	res, err = client.GetOrderDetailRaw(ctx, "123", nil)
	require.NoError(t, err)
	assert.Contains(t, string(res), "raw-data")

	res, err = client.GetHistoryOrdersRaw(ctx, nil)
	require.NoError(t, err)
	assert.Contains(t, string(res), "raw-data")

	res, err = client.GetOrderPNLRaw(ctx, nil)
	require.NoError(t, err)
	assert.Contains(t, string(res), "raw-data")
}
