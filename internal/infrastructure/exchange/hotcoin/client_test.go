package hotcoin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/hotcoin"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockClock struct {
	now time.Time
}

func (m mockClock) Now() time.Time {
	return m.now
}

func TestClient_SignatureAndRawRequest(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/v1/perpetual/products/btcusdt/order", r.URL.Path)

		query := r.URL.Query()
		assert.Equal(t, "my-api-key", query.Get("AccessKeyId"))
		assert.Equal(t, "HmacSHA256", query.Get("SignatureMethod"))
		assert.Equal(t, "2", query.Get("SignatureVersion"))
		assert.Equal(t, "2026-07-05T00:00:00.000Z", query.Get("Timestamp"))
		assert.NotEmpty(t, query.Get("Signature"))

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code": 200, "msg": "success"}`))
	}))
	defer server.Close()

	client := hotcoin.NewClient(server.Client(), server.URL, "my-api-key", "my-api-secret", config.LoggingConfig{})
	client.SetClock(mockClock{now: time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)})

	res, err := client.RawRequest(context.Background(), "POST", "/api/v1/perpetual/products/btcusdt/order", nil, []byte(`{"amount": 1}`))
	require.NoError(t, err)
	assert.NotEmpty(t, res)
}

func TestClient_RawRequestHelpers(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": "ok"}`))
	}))
	defer server.Close()

	client := hotcoin.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	client.SetClock(mockClock{now: time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)})

	ctx := context.Background()

	_, err := client.GetFundingRateRaw(ctx, map[string]string{"symbol": "BTCUSDT"})
	assert.NoError(t, err)

	_, err = client.GetTickersRaw(ctx, nil)
	assert.NoError(t, err)

	_, err = client.GetOpenPositionsRaw(ctx, map[string]string{"symbol": "BTCUSDT"})
	assert.NoError(t, err)

	_, err = client.GetOpenPositionsRaw(ctx, map[string]string{})
	assert.Error(t, err)

	_, err = client.GetOrderDetailRaw(ctx, "123", map[string]string{"symbol": "BTCUSDT"})
	assert.NoError(t, err)

	_, err = client.GetHistoryOrdersRaw(ctx, map[string]string{"symbol": "BTCUSDT"})
	assert.NoError(t, err)

	_, err = client.GetOrderPNLRaw(ctx, nil)
	assert.NoError(t, err)

	_, err = client.GetHistoryPositionsRaw(ctx, nil)
	assert.Error(t, err)
}

func TestClient_GetTickers(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/perpetual/public/contracts", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code": 200,
			"msg": "success",
			"data": [
				{
					"tickerId": "BTCUSDT",
					"lastPrice": "98123.5",
					"bid": "98122.0",
					"ask": "98124.0",
					"baseVolume": "567.89",
					"nextFundingRate": "0.0001",
					"nextFundingRateTimestamp": 1783229346887
				}
			]
		}`))
	}))
	defer server.Close()

	client := hotcoin.NewClient(server.Client(), server.URL, "", "", config.LoggingConfig{})
	tickers, err := client.GetTickers(context.Background(), "")
	require.NoError(t, err)
	require.Len(t, tickers, 1)

	assert.Equal(t, "BTC_USDT", tickers[0].Symbol)
	assert.Equal(t, 98123.5, tickers[0].LastPrice)
	assert.Equal(t, 98122.0, tickers[0].Bid1)
	assert.Equal(t, 98124.0, tickers[0].Ask1)
	assert.Equal(t, 567.89, tickers[0].Volume24)
	assert.Equal(t, 567.89*98123.5, tickers[0].AmountUSDT24)
}

func TestClient_GetContractDetails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/perpetual/public", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code": 200,
			"msg": "success",
			"data": [
				{
					"code": "BTCUSDT",
					"base": "BTC",
					"quote": "USDT",
					"indexBase": "BTC",
					"minQuoteDigit": 1,
					"minTradeDigit": 4,
					"minTradeUnit": 1,
					"maxLever": 100,
					"unitAmount": "0.001"
				}
			]
		}`))
	}))
	defer server.Close()

	client := hotcoin.NewClient(server.Client(), server.URL, "", "", config.LoggingConfig{})
	details, err := client.GetContractDetails(context.Background())
	require.NoError(t, err)
	require.Len(t, details, 1)

	assert.Equal(t, "BTC_USDT", details[0].Symbol)
	assert.Equal(t, "BTCUSDT", details[0].DisplayName)
	assert.Equal(t, 1, details[0].PriceScale)
	assert.Equal(t, 0.1, details[0].PriceUnit)
	assert.Equal(t, 4, details[0].VolScale)
	assert.Equal(t, 1, details[0].MinVol)
	assert.Equal(t, 100, details[0].MaxLeverage)
	assert.Equal(t, 0.001, details[0].ContractSize)
}

func TestClient_GetFundingRates(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/perpetual/public/contracts", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code": 200,
			"msg": "success",
			"data": [
				{
					"tickerId": "BTCUSDT",
					"nextFundingRate": "0.00015",
					"nextFundingRateTimestamp": 1783229346000
				}
			]
		}`))
	}))
	defer server.Close()

	client := hotcoin.NewClient(server.Client(), server.URL, "", "", config.LoggingConfig{})
	rates, err := client.GetFundingRates(context.Background(), []string{"BTC_USDT"})
	require.NoError(t, err)
	require.Len(t, rates, 1)

	assert.Equal(t, "BTC_USDT", rates[0].Symbol)
	assert.Equal(t, 0.00015, rates[0].Rate)
	assert.Equal(t, int64(1783229346000), rates[0].SettleTime)
}

func TestClient_GetPotentialFundingSymbols(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code": 200,
			"msg": "success",
			"data": [
				{
					"tickerId": "BTCUSDT",
					"lastPrice": "95000",
					"nextFundingRate": "0.0002",
					"nextFundingRateTimestamp": 1783229346000,
					"targetVolume": "10000000"
				}
			]
		}`))
	}))
	defer server.Close()

	client := hotcoin.NewClient(server.Client(), server.URL, "", "", config.LoggingConfig{})
	res, err := client.GetPotentialFundingSymbols(context.Background(), 1000, 0, nil, nil)
	require.NoError(t, err)
	require.Len(t, res, 1)

	assert.Equal(t, "BTC_USDT", res[0].Symbol)
	assert.Equal(t, 0.0002, res[0].Rate)
	assert.Equal(t, 10000000.0, res[0].Volume24h)
	assert.Equal(t, 95000.0, res[0].Price)
}

func TestClient_CreateAndCancelOrder(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "POST":
			assert.Equal(t, "/api/v1/perpetual/products/btcusdt/order", r.URL.Path)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id": "3799035537965136"}`))
		case "DELETE":
			if strings.Contains(r.URL.Path, "/order/999") {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"code": 400, "msg": "order not found"}`))
			} else {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"code": 200, "msg": "success"}`))
			}
		case "GET":
			w.WriteHeader(http.StatusOK)
			if strings.Contains(r.URL.Path, "/public") {
				_, _ = w.Write([]byte(`{"code": 200, "msg": "success", "data": [{"code":"BTCUSDT","indexBase":"BTC","quote":"USDT"}]}`))
			} else {
				_, _ = w.Write([]byte(`[
					{
						"id": 3799035537965136,
						"amount": "1",
						"dealAmount": "0",
						"price": "95000",
						"avgPrice": "0",
						"status": 0,
						"detailSide": "open_long"
					}
				]`))
			}
		}
	}))
	defer server.Close()

	client := hotcoin.NewClient(server.Client(), server.URL, "api-key", "api-secret", config.LoggingConfig{})
	client.SetClock(mockClock{now: time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)})

	// 1. Create Limit Order
	res, err := client.CreateOrder(context.Background(), exchange.SubmitOrderRequest{
		Symbol: "BTC_USDT",
		Side:   domain.SideOpenLong,
		Type:   domain.OrderTypeLimit,
		Price:  95000.0,
		Vol:    1.0,
	})
	require.NoError(t, err)
	assert.Equal(t, "3799035537965136", res.OrderID)

	// 2. Cancel Order
	err = client.CancelOrder(context.Background(), "BTC_USDT", "3799035537965136")
	require.NoError(t, err)

	// 2b. Create IOC Order (should succeed via emulation)
	resIOC, err := client.CreateOrder(context.Background(), exchange.SubmitOrderRequest{
		Symbol: "BTC_USDT",
		Side:   domain.SideOpenLong,
		Type:   domain.OrderTypeIOC,
		Price:  95000.0,
		Vol:    1.0,
	})
	require.NoError(t, err)
	assert.Equal(t, "3799035537965136", resIOC.OrderID)

	// 2c. Cancel conditional order that fails regular cancel but succeeds on conditional cancel fallback
	err = client.CancelOrder(context.Background(), "BTC_USDT", "999")
	assert.NoError(t, err)

	// 3. Cancel Orders
	err = client.CancelOrders(context.Background(), []string{"3799035537965136"})
	assert.NoError(t, err)

	// 4. Cancel All Open Orders
	err = client.CancelAllOpenOrders(context.Background(), "BTC_USDT")
	assert.NoError(t, err)
}

func TestClient_GetOrderAndGetOpenOrders(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/list"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[
				{
					"id": 3799035537965136,
					"amount": "1",
					"dealAmount": "0",
					"price": "95000",
					"avgPrice": "0",
					"status": 0,
					"detailSide": "open_long",
					"tag": "my-tag",
					"createdDate": 1783229346000
				}
			]`))
		case strings.Contains(r.URL.Path, "/history-list"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"code": 200,
				"data": {
					"rows": [
						{
							"id": 3799035537965136,
							"amount": "1",
							"dealAmount": "1",
							"price": "95000",
							"avgPrice": "95000",
							"status": 2,
							"detailSide": "open_long",
							"tag": "history-tag",
							"createdDate": "2022-04-18 16:07:49"
						}
					]
				}
			}`))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"id": 3799035537965136,
				"amount": "1",
				"dealAmount": "1",
				"price": "95000",
				"avgPrice": "95000",
				"status": 2,
				"detailSide": "open_long",
				"tag": "my-tag",
				"createdDate": 1783229346000
			}`))
		}
	}))
	defer server.Close()

	client := hotcoin.NewClient(server.Client(), server.URL, "api-key", "api-secret", config.LoggingConfig{})
	client.SetClock(mockClock{now: time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)})

	// 1. Get Open Orders
	open, err := client.GetOpenOrders(context.Background(), "BTC_USDT")
	require.NoError(t, err)
	require.Len(t, open, 1)
	assert.Equal(t, "3799035537965136", open[0].OrderID)
	assert.Equal(t, domain.OrderStateNew, open[0].State)
	assert.Equal(t, "my-tag", open[0].ExternalOID)

	// 2. Get Single Order
	order, err := client.GetOrder(context.Background(), "BTC_USDT", "3799035537965136")
	require.NoError(t, err)
	require.NotNil(t, order)
	assert.Equal(t, "3799035537965136", order.OrderID)
	assert.Equal(t, domain.OrderStateFilled, order.State)
	assert.Equal(t, 1.0, order.DealVol)

	// 3. Get Order by External ID (from open orders)
	extOrder, err := client.GetOrderByExternalID(context.Background(), "BTC_USDT", "my-tag")
	require.NoError(t, err)
	require.NotNil(t, extOrder)
	assert.Equal(t, "my-tag", extOrder.ExternalOID)

	// 4. Get Order by External ID (from history orders fallback)
	extOrderHist, err := client.GetOrderByExternalID(context.Background(), "BTC_USDT", "history-tag")
	require.NoError(t, err)
	require.NotNil(t, extOrderHist)
	assert.Equal(t, "history-tag", extOrderHist.ExternalOID)
	assert.Equal(t, int64(1650298069000), extOrderHist.CreateTime)
}

func TestClient_GetOrderPNL(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/deal-record") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"code": 200,
				"data": {
					"data": [
						{
							"amount": "1",
							"price": "96000",
							"fee": "-0.0001",
							"profit": "1000",
							"orderId": 3799035537965136,
							"detailSide": "close_long",
							"createDate": "2026-07-05 00:00:10"
						}
					]
				}
			}`))
		} else {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"id": 3799035537965136,
				"amount": "1",
				"dealAmount": "1",
				"price": "96000",
				"avgPrice": "96000",
				"status": 2,
				"detailSide": "close_long",
				"tag": "my-tag",
				"createdDate": 1783229340000
			}`))
		}
	}))
	defer server.Close()

	client := hotcoin.NewClient(server.Client(), server.URL, "api-key", "api-secret", config.LoggingConfig{})
	client.SetClock(mockClock{now: time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)})

	pnl, err := client.GetOrderPNL(context.Background(), "BTC_USDT", "3799035537965136")
	require.NoError(t, err)
	require.NotNil(t, pnl)

	assert.Equal(t, "hotcoin", pnl.Exchange)
	assert.Equal(t, "BTC_USDT", pnl.Symbol)
	assert.Equal(t, 96000.0, pnl.ExitPrice)
	assert.Equal(t, 95000.0, pnl.EntryPrice) // ExitPrice - (profit / amount) = 96000 - 1000 = 95000
	assert.Equal(t, 1.0, pnl.ClosedSize)
	assert.Equal(t, 1000.0, pnl.GrossPnL)
	assert.Equal(t, 0.0001, pnl.Fee)
	assert.Equal(t, 999.9999, pnl.NetPnl)
}

func TestClient_SystemOperations(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/time") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"timestamp": 1783229346000}`))
		} else {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"code": 200, "msg": "success"}`))
		}
	}))
	defer server.Close()

	client := hotcoin.NewClient(server.Client(), server.URL, "", "", config.LoggingConfig{})
	client.SetClock(mockClock{now: time.UnixMilli(1783229340000)})

	// 1. GetServerTime
	serverTime, err := client.GetServerTime(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1783229346000), serverTime)

	// 2. Warmup
	ctx := t.Context()
	go client.WarmUp(ctx, 50*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
}

func TestClient_TradeAndPositionOperations(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/perpetual/position/btcusdt/lever" {
			assert.Equal(t, http.MethodPost, r.Method)
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			assert.Contains(t, body, "type")
			assert.Contains(t, body, "lever")
			assert.Contains(t, body, "side")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code": 200, "msg": "success"}`))
	}))
	defer server.Close()

	client := hotcoin.NewClient(server.Client(), server.URL, "api-key", "api-secret", config.LoggingConfig{})
	client.SetClock(mockClock{now: time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)})

	// 1. ChangeLeverage
	err := client.ChangeLeverage(context.Background(), exchange.ChangeLeverageRequest{
		Symbol:   "BTC_USDT",
		Leverage: 10,
	})
	assert.NoError(t, err)

	// 2. SwitchMarginMode
	err = client.SwitchMarginMode(context.Background(), "BTC_USDT", domain.MarginModeIsolated, 10, domain.SideOpenLong)
	assert.NoError(t, err)

	// 3. GetOpenPositions
	positions, err := client.GetOpenPositions(context.Background(), "BTC_USDT")
	assert.NoError(t, err)
	assert.Empty(t, positions)

	// 4. ClosePosition
	err = client.ClosePosition(context.Background(), "BTC_USDT", domain.SideCloseLong, 1.0, domain.PositionModeHedge, 10)
	assert.NoError(t, err)

	// 4b. ClosePosition with bad side
	err = client.ClosePosition(context.Background(), "BTC_USDT", domain.SideOpenLong, 1.0, domain.PositionModeHedge, 10)
	assert.Error(t, err)

	// 5. CloseAllPositions
	err = client.CloseAllPositions(context.Background(), "BTC_USDT")
	assert.NoError(t, err)

	// 6. SupportLeverageOnOrder
	assert.False(t, client.SupportLeverageOnOrder())

	// 7. NewClient with log configuration
	clientWithLog := hotcoin.NewClient(nil, "", "key", "secret", config.LoggingConfig{HTTP: true})
	assert.NotNil(t, clientWithLog)
}

func TestClient_PlaceTPSL(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.Contains(r.URL.Path, "/contracts") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"code": 200,
				"msg": "success",
				"data": [
					{
						"tickerId": "BTCUSDT",
						"lastPrice": "95000"
					}
				]
			}`))
		} else if r.Method == "POST" && strings.Contains(r.URL.Path, "/order") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id": "3799035537965136"}`))
		}
	}))
	defer server.Close()

	client := hotcoin.NewClient(server.Client(), server.URL, "api-key", "api-secret", config.LoggingConfig{})
	client.SetClock(mockClock{now: time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)})

	err := client.PlaceTPSL(context.Background(), exchange.TPSLRequest{
		Symbol:          "BTC_USDT",
		Side:            domain.SideOpenLong,
		TakeProfitPrice: 96000.0,
		StopLossPrice:   94000.0,
		Volume:          1.0,
	})
	assert.NoError(t, err)
}

func TestClient_GetOpenPositions_DirectArray(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/perpetual/position/btcusdt/list", r.URL.Path)
		assert.Equal(t, http.MethodGet, r.Method)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[
			{
				"amount": "1.5",
				"contractCode": "btcusdt",
				"side": "long",
				"price": "50000.0",
				"fee": "0.1",
				"lever": "10"
			},
			{
				"amount": "0",
				"contractCode": "btcusdt",
				"side": "short",
				"price": "50000.0",
				"fee": "0.0",
				"lever": "10"
			}
		]`))
	}))
	defer server.Close()

	client := hotcoin.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	client.SetClock(mockClock{now: time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)})

	positions, err := client.GetOpenPositions(context.Background(), "BTC_USDT")
	require.NoError(t, err)
	assert.Len(t, positions, 1)

	pos := positions[0]
	assert.Equal(t, "BTC_USDT", pos.Symbol)
	assert.Equal(t, 1.5, pos.HoldVol)
	assert.Equal(t, exchange.PositionTypeLong, pos.PositionType)
	assert.Equal(t, 50000.0, pos.OpenAvgPrice)
	assert.Equal(t, 50000.0, pos.HoldAvgPrice)
	assert.Equal(t, 10, pos.Leverage)
	assert.Equal(t, 0.1, pos.Fee)
}

func TestClient_GetOpenPositions_WrappedEnvelope(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/perpetual/position/btcusdt/list", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code": 200,
			"msg": "success",
			"data": [
				{
					"amount": "2.0",
					"contractCode": "btcusdt",
					"side": "short",
					"price": "51000.0",
					"fee": "0.2",
					"lever": "20"
				}
			]
		}`))
	}))
	defer server.Close()

	client := hotcoin.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	client.SetClock(mockClock{now: time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)})

	positions, err := client.GetOpenPositions(context.Background(), "BTC_USDT")
	require.NoError(t, err)
	assert.Len(t, positions, 1)

	pos := positions[0]
	assert.Equal(t, "BTC_USDT", pos.Symbol)
	assert.Equal(t, 2.0, pos.HoldVol)
	assert.Equal(t, exchange.PositionTypeShort, pos.PositionType)
	assert.Equal(t, 51000.0, pos.OpenAvgPrice)
	assert.Equal(t, 51000.0, pos.HoldAvgPrice)
	assert.Equal(t, 20, pos.Leverage)
	assert.Equal(t, 0.2, pos.Fee)
}

func TestClient_GetOpenPositions_Errors(t *testing.T) {
	t.Parallel()

	client := hotcoin.NewClient(nil, "", "key", "secret", config.LoggingConfig{})
	_, err := client.GetOpenPositions(context.Background(), "")
	assert.Error(t, err)

	_, err = client.GetOpenPositionsRaw(context.Background(), map[string]string{})
	assert.Error(t, err)
}
