package bingx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/bingx"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_GetServerTime(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/openApi/swap/v2/server/time", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 0,
			"msg": "success",
			"data": {
				"serverTime": 1695812285073
			}
		}`))
	}))
	defer server.Close()

	client := bingx.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	ts, err := client.GetServerTime(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1695812285073), ts)
}

func TestClient_GetContractDetails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/openApi/swap/v2/quote/contracts", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 0,
			"msg": "success",
			"data": [
				{
					"symbol": "BTC-USDT",
					"quantity_precision": 3,
					"price_precision": 1,
					"maker_fee_rate": 0.0002,
					"taker_fee_rate": 0.0005,
					"trade_min_quantity": 1,
					"trade_min_usdt": 5,
					"currency": "USDT",
					"asset": "BTC",
					"status": 1
				}
			]
		}`))
	}))
	defer server.Close()

	client := bingx.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	details, err := client.GetContractDetails(context.Background())
	require.NoError(t, err)
	require.Len(t, details, 1)
	assert.Equal(t, "BTC-USDT", details[0].Symbol)
	assert.Equal(t, "BTC", details[0].BaseCoin)
	assert.Equal(t, 1, details[0].PriceScale)
	assert.Equal(t, 3, details[0].VolScale)
}

func TestClient_GetTickers(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/openApi/swap/v2/quote/ticker":
			_, _ = w.Write([]byte(`{
				"code": 0,
				"msg": "success",
				"data": [
					{
						"symbol": "BTC-USDT",
						"lastPrice": "50000.5",
						"bidPrice": "50000.0",
						"askPrice": "50001.0",
						"volume": "1000",
						"quoteVolume": "50000000",
						"time": "1695812285073"
					}
				]
			}`))
		case "/openApi/swap/v2/quote/premiumIndex":
			_, _ = w.Write([]byte(`{
				"code": 0,
				"msg": "success",
				"data": [
					{
						"symbol": "BTC-USDT",
						"markPrice": "50000.4",
						"lastFundingRate": "0.0001",
						"nextFundingTime": 1695841085073
					}
				]
			}`))
		}
	}))
	defer server.Close()

	client := bingx.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	tickers, err := client.GetTickers(context.Background(), "")
	require.NoError(t, err)
	require.Len(t, tickers, 1)
	assert.Equal(t, "BTC-USDT", tickers[0].Symbol)
	assert.Equal(t, 50000.5, tickers[0].LastPrice)
	assert.Equal(t, 50000.0, tickers[0].Bid1)
	assert.Equal(t, 50001.0, tickers[0].Ask1)
	assert.Equal(t, 50000.4, tickers[0].FairPrice)
	assert.Equal(t, 0.0001, tickers[0].FundingRate)
}

func TestClient_GetKlines(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/openApi/swap/v3/quote/klines", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 0,
			"msg": "success",
			"data": [
				{
					"open": "50000.0",
					"close": "50000.5",
					"high": "50001.0",
					"low": "49999.0",
					"volume": "10",
					"time": 1695812285000
				}
			]
		}`))
	}))
	defer server.Close()

	client := bingx.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	klines, err := client.GetKlines(context.Background(), "BTC-USDT", "1m", 0, 0)
	require.NoError(t, err)
	require.Len(t, klines, 1)
	assert.Equal(t, int64(1695812285000), klines[0].Timestamp)
	assert.Equal(t, 50000.0, klines[0].Open)
	assert.Equal(t, 50000.5, klines[0].Close)
}

func TestClient_CreateOrder(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/openApi/swap/v2/trade/order", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 0,
			"msg": "success",
			"data": {
				"orderId": "123456",
				"clientOid": "external_123"
			}
		}`))
	}))
	defer server.Close()

	client := bingx.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	res, err := client.CreateOrder(context.Background(), exchange.SubmitOrderRequest{
		Symbol:       "BTC-USDT",
		Vol:          0.5,
		Side:         exchange.SideOpenLong,
		Type:         exchange.OrderTypeLimit,
		Price:        50000.0,
		PositionMode: 1,
		ExternalOID:  "external_123",
	})
	require.NoError(t, err)
	assert.Equal(t, "123456", res.OrderID)
}

func TestClient_CancelOrder(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/openApi/swap/v2/trade/cancel", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 0,
			"msg": "success"
		}`))
	}))
	defer server.Close()

	client := bingx.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	err := client.CancelOrder(context.Background(), "BTC-USDT", "123456")
	require.NoError(t, err)
}

func TestClient_GetOrder(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/openApi/swap/v2/trade/order", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 0,
			"msg": "success",
			"data": {
				"orderId": "123456",
				"symbol": "BTC-USDT",
				"side": "BUY",
				"positionSide": "LONG",
				"type": "LIMIT",
				"quantity": "0.5",
				"price": "50000.0",
				"status": "FILLED",
				"executedQty": "0.5",
				"avgPrice": "50000.0"
			}
		}`))
	}))
	defer server.Close()

	client := bingx.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	info, err := client.GetOrder(context.Background(), "BTC-USDT", "123456")
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "123456", info.OrderID)
	assert.Equal(t, 50000.0, info.Price)
}

func TestClient_GetOpenOrders(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/openApi/swap/v2/trade/openOrders", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 0,
			"msg": "success",
			"data": [
				{
					"orderId": "123456",
					"symbol": "BTC-USDT",
					"side": "SELL",
					"positionSide": "SHORT",
					"type": "LIMIT",
					"quantity": "0.5",
					"price": "50000.0",
					"status": "PARTIALLY_FILLED",
					"executedQty": "0.1",
					"avgPrice": "50000.0"
				}
			]
		}`))
	}))
	defer server.Close()

	client := bingx.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	orders, err := client.GetOpenOrders(context.Background(), "BTC-USDT")
	require.NoError(t, err)
	require.Len(t, orders, 1)
	assert.Equal(t, "123456", orders[0].OrderID)
}

func TestClient_GetAssets(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/openApi/swap/v2/user/balance", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 0,
			"msg": "success",
			"data": [
				{
					"asset": "USDT",
					"balance": "1000.0",
					"equity": "1050.0",
					"availableMargin": "950.0"
				}
			]
		}`))
	}))
	defer server.Close()

	client := bingx.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	assets, err := client.GetAssets(context.Background())
	require.NoError(t, err)
	require.Len(t, assets, 1)
	assert.Equal(t, "USDT", assets[0].Currency)
	assert.Equal(t, 1000.0, assets[0].CashBalance)
	assert.Equal(t, 1050.0, assets[0].Equity)
	assert.Equal(t, 950.0, assets[0].AvailableBalance)

	asset, err := client.GetAssetByCurrency(context.Background(), "USDT")
	require.NoError(t, err)
	assert.Equal(t, "USDT", asset.Currency)
}

func TestClient_GetOpenPositions(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/openApi/swap/v2/user/positions", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 0,
			"msg": "success",
			"data": [
				{
					"symbol": "BTC-USDT",
					"positionSide": "SHORT",
					"positionAmt": "-0.5",
					"entryPrice": "50000.0",
					"unrealizedProfit": "50.0",
					"leverage": "10",
					"isolated": false
				}
			]
		}`))
	}))
	defer server.Close()

	client := bingx.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	positions, err := client.GetOpenPositions(context.Background(), "BTC-USDT")
	require.NoError(t, err)
	require.Len(t, positions, 1)
	assert.Equal(t, "BTC-USDT", positions[0].Symbol)
	assert.Equal(t, 0.5, positions[0].HoldVol)
	assert.Equal(t, 2, positions[0].PositionType) // Short
}

func TestClient_ChangeLeverage(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/openApi/swap/v2/trade/leverage", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 0,
			"msg": "success"
		}`))
	}))
	defer server.Close()

	client := bingx.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	err := client.ChangeLeverage(context.Background(), exchange.ChangeLeverageRequest{
		Symbol:   "BTC-USDT",
		Leverage: 10,
	})
	require.NoError(t, err)
}

func TestClient_CancelAllOpenOrders_and_CloseAll(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /openApi/swap/v2/trade/openOrders":
			_, _ = w.Write([]byte(`{
				"code": 0,
				"msg": "success",
				"data": [
					{
						"orderId": "123456",
						"symbol": "BTC-USDT",
						"side": "BUY",
						"positionSide": "LONG",
						"type": "LIMIT",
						"quantity": "0.5",
						"price": "50000.0",
						"status": "NEW",
						"executedQty": "0.0",
						"avgPrice": "0.0"
					}
				]
			}`))
		case "POST /openApi/swap/v2/trade/cancel":
			_, _ = w.Write([]byte(`{
				"code": 0,
				"msg": "success"
			}`))
		case "GET /openApi/swap/v2/user/positions":
			_, _ = w.Write([]byte(`{
				"code": 0,
				"msg": "success",
				"data": [
					{
						"symbol": "BTC-USDT",
						"positionSide": "LONG",
						"positionAmt": "0.5",
						"entryPrice": "50000.0",
						"unrealizedProfit": "50.0",
						"leverage": "10",
						"isolated": true
					}
				]
			}`))
		case "POST /openApi/swap/v2/trade/order":
			_, _ = w.Write([]byte(`{
				"code": 0,
				"msg": "success",
				"data": {
					"orderId": "1234567"
				}
			}`))
		}
	}))
	defer server.Close()

	client := bingx.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})

	err := client.CancelAllOpenOrders(context.Background(), "BTC-USDT")
	require.NoError(t, err)

	err = client.CloseAllPositions(context.Background(), "BTC-USDT")
	require.NoError(t, err)
}

func TestClient_ErrorPaths(t *testing.T) {
	t.Parallel()

	// 1. CreateTrackOrder unimplemented
	client := bingx.NewClient(nil, "", "key", "secret", config.LoggingConfig{})
	_, err := client.CreateTrackOrder(context.Background(), exchange.SubmitTrackOrderRequest{})
	assert.ErrorContains(t, err, "CreateTrackOrder not implemented")

	// 2. CancelOrders unimplemented
	err = client.CancelOrders(context.Background(), []string{"1"})
	assert.ErrorContains(t, err, "batch CancelOrders not implemented")

	// 3. HTTP Server Non-200 Status
	serverNon200 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code": 10001, "msg": "bad request"}`))
	}))
	defer serverNon200.Close()

	clientNon200 := bingx.NewClient(serverNon200.Client(), serverNon200.URL, "key", "secret", config.LoggingConfig{})
	_, err = clientNon200.GetServerTime(context.Background())
	assert.Error(t, err)

	// 4. HTTP Server Rate Limited Status
	serverRateLimit := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`rate limited`))
	}))
	defer serverRateLimit.Close()

	clientRateLimit := bingx.NewClient(serverRateLimit.Client(), serverRateLimit.URL, "key", "secret", config.LoggingConfig{})
	_, err = clientRateLimit.GetServerTime(context.Background())
	assert.Error(t, err)

	// 5. GetAssets unmarshal fallback failure
	serverInvalidJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{invalid-json`))
	}))
	defer serverInvalidJSON.Close()

	clientInvalidJSON := bingx.NewClient(serverInvalidJSON.Client(), serverInvalidJSON.URL, "key", "secret", config.LoggingConfig{})
	_, err = clientInvalidJSON.GetAssets(context.Background())
	assert.Error(t, err)
}
