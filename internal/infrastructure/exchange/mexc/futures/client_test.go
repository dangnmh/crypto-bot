package futures_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/mexc/futures"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFuturesClient_GetDepth(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/contract/depth/BTC_USDT", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"success": true,
			"code": 0,
			"data": {
				"asks": [["50000.5", "1.2"]],
				"bids": [["49999.5", "2.5"]],
				"version": 123456
			}
		}`))
	}))
	defer server.Close()

	client := futures.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	assert.True(t, client.IsFutures())

	depth, err := client.GetDepth(context.Background(), "BTC_USDT")
	require.NoError(t, err)
	require.NotNil(t, depth)
	assert.Equal(t, "BTC_USDT", depth.Symbol)
	assert.Equal(t, int64(123456), depth.Version)
	require.Len(t, depth.Bids, 1)
	assert.Equal(t, 49999.5, depth.Bids[0].Price)
	assert.Equal(t, 2.5, depth.Bids[0].Volume)
}

func TestFuturesClient_GetTopGainer(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/contract/ticker", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"success": true,
			"code": 0,
			"data": [
				{
					"symbol": "BTC_USDT",
					"lastPrice": 50000.0,
					"bid1": 49999.0,
					"ask1": 50001.0,
					"volume24": 100.0,
					"amount24": 5000000.0,
					"riseFallRate": 0.05,
					"timestamp": 1670000000000
				}
			]
		}`))
	}))
	defer server.Close()

	client := futures.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	gainers, err := client.GetTopGainer(context.Background(), exchange.TopGainerRequest{Limit: 10})
	require.NoError(t, err)
	require.Len(t, gainers, 1)
	assert.Equal(t, "BTC_USDT", gainers[0].Symbol)
	assert.Equal(t, 5.0, gainers[0].Gain24hPct)
}

func TestFuturesClient_OrderAndPosition(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/private/order/create" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success": true, "code": 0, "data": {"orderId": "12345", "ts": 1670000000000}}`))
			return
		}
		if r.URL.Path == "/api/v1/private/position/open_positions" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success": true, "code": 0, "data": [{"positionId": 99, "symbol": "BTC_USDT", "holdVol": 1.0, "holdAvgPrice": 50000.0}]}`))
			return
		}
		if r.URL.Path == "/api/v1/contract/ping" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success": true, "code": 0, "data": 1670000000000}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := futures.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	client.SetClock(exchange.RealClock{})

	res, err := client.CreateOrder(context.Background(), exchange.SubmitOrderRequest{
		Symbol: "BTC_USDT",
		Price:  50000,
		Vol:    1,
		Side:   exchange.SideOpenLong,
		Type:   exchange.OrderTypeLimit,
	})
	require.NoError(t, err)
	assert.Equal(t, "12345", res.OrderID)

	positions, err := client.GetOpenPositions(context.Background(), "BTC_USDT")
	require.NoError(t, err)
	require.Len(t, positions, 1)
	assert.Equal(t, 1.0, positions[0].HoldVolContract)
	assert.Equal(t, 50000.0, positions[0].HoldAvgPrice)

	st, err := client.GetServerTime(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1670000000000), st)

	err = client.Ping(context.Background())
	require.NoError(t, err)

	warmCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	client.WarmUp(warmCtx, 10*time.Millisecond)
}

func TestFuturesClient_PrepareOrder(t *testing.T) {
	t.Parallel()

	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		assert.Equal(t, "/api/v1/private/order/create", r.URL.Path)
		assert.Equal(t, "POST", r.Method)
		assert.NotEmpty(t, r.Header.Get("ApiKey"))
		assert.NotEmpty(t, r.Header.Get("Signature"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true,"code":0,"data":{"orderId":"mexc-pre-123","ts":1670000000000}}`))
	}))
	defer server.Close()

	client := futures.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})

	dispatch, err := client.PrepareOrder(context.Background(), exchange.SubmitOrderRequest{
		Symbol: "BTC_USDT",
		Price:  50000,
		Vol:    1,
		Side:   exchange.SideOpenLong,
		Type:   exchange.OrderTypeLimit,
	})
	require.NoError(t, err)
	require.NotNil(t, dispatch)
	assert.False(t, called, "PrepareOrder must not make HTTP call during preparation phase")

	res, err := dispatch(context.Background())
	require.NoError(t, err)
	assert.True(t, called, "dispatch must execute the prepared HTTP request")
	assert.Equal(t, "mexc-pre-123", res.OrderID)
}

func TestFuturesClient_GetFundingRates(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/contract/funding_rate", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"success": true,
			"code": 0,
			"data": [
				{
					"symbol": "BTC_USDT",
					"fundingRate": 0.0001,
					"nextSettleTime": 1788192000000
				},
				{
					"symbol": "ETH_USDT",
					"fundingRate": -0.0005,
					"nextSettleTime": 1788192000000
				}
			]
		}`))
	}))
	defer server.Close()

	client := futures.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	rates, err := client.GetFundingRates(context.Background(), []string{"BTC_USDT", "ETH_USDT"})
	require.NoError(t, err)
	require.Len(t, rates, 2)
	assert.Equal(t, "BTC_USDT", rates[0].Symbol)
	assert.Equal(t, 0.0001, rates[0].Rate)
	assert.Equal(t, int64(1788192000000), rates[0].SettleTime)
	assert.Equal(t, "ETH_USDT", rates[1].Symbol)
	assert.Equal(t, -0.0005, rates[1].Rate)
	assert.Equal(t, int64(1788192000000), rates[1].SettleTime)
}

func TestFuturesClient_GetPotentialFundingSymbols(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/contract/ticker" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"success": true,
				"code": 0,
				"data": [
					{
						"symbol": "BTC_USDT",
						"lastPrice": 50000.0,
						"amount24": 5000000.0,
						"fundingRate": 0.0001
					},
					{
						"symbol": "ZORA_USDT",
						"lastPrice": 0.01,
						"amount24": 2000000.0,
						"fundingRate": -0.006
					}
				]
			}`))
			return
		}
		if r.URL.Path == "/api/v1/contract/funding_rate" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"success": true,
				"code": 0,
				"data": [
					{
						"symbol": "BTC_USDT",
						"fundingRate": 0.0001,
						"nextSettleTime": 1788192000000
					},
					{
						"symbol": "ZORA_USDT",
						"fundingRate": -0.0061,
						"nextSettleTime": 1788192000000
					}
				]
			}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := futures.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	res, err := client.GetPotentialFundingSymbols(context.Background(), 1000000.0, 0, nil, nil)
	require.NoError(t, err)
	require.Len(t, res, 2)

	assert.Equal(t, "BTC_USDT", res[0].Symbol)
	assert.Equal(t, 0.0001, res[0].Rate)
	assert.Equal(t, int64(1788192000000), res[0].SettleTime)

	assert.Equal(t, "ZORA_USDT", res[1].Symbol)
	assert.Equal(t, -0.0061, res[1].Rate)
	assert.Equal(t, int64(1788192000000), res[1].SettleTime)
}

func TestFuturesClient_PlaceTPSL_Long(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requests []map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/private/planorder/place/v2", r.URL.Path)
		assert.Equal(t, "POST", r.Method)

		bodyBytes, err := io.ReadAll(r.Body)
		assert.NoError(t, err)

		var reqMap map[string]any
		err = json.Unmarshal(bodyBytes, &reqMap)
		assert.NoError(t, err)

		mu.Lock()
		requests = append(requests, reqMap)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success": true, "code": 0, "data": "739206374277809664"}`))
	}))
	defer server.Close()

	client := futures.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})

	err := client.PlaceTPSL(context.Background(), exchange.TPSLRequest{
		Symbol:          "BTC_USDT",
		PositionMode:    domain.PositionModeHedge,
		Side:            domain.SideOpenLong,
		TakeProfitPrice: 55000.0,
		StopLossPrice:   45000.0,
		Volume:          2.0,
	})
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, requests, 2)

	// In Long position, closing side is 4 (close long)
	for _, req := range requests {
		assert.Equal(t, "BTC_USDT", req["symbol"])
		assert.Equal(t, float64(4), req["side"])
		assert.Equal(t, float64(2), req["vol"])
		assert.Equal(t, float64(5), req["orderType"]) // market
		assert.Equal(t, float64(1), req["trend"])     // latest
		assert.Equal(t, float64(1), req["openType"])  // isolated default
		assert.Equal(t, true, req["reduceOnly"])

		triggerPrice, ok := req["triggerPrice"].(float64)
		require.True(t, ok)
		triggerType, ok := req["triggerType"].(float64)
		require.True(t, ok)
		switch triggerPrice {
		case 55000.0:
			// TP for Long: >=
			assert.Equal(t, float64(1), triggerType)
		case 45000.0:
			// SL for Long: <=
			assert.Equal(t, float64(2), triggerType)
		default:
			t.Fatalf("unexpected trigger price: %v", triggerPrice)
		}
	}
}

func TestFuturesClient_PlaceTPSL_Short(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requests []map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/private/planorder/place/v2", r.URL.Path)

		bodyBytes, err := io.ReadAll(r.Body)
		assert.NoError(t, err)

		var reqMap map[string]any
		err = json.Unmarshal(bodyBytes, &reqMap)
		assert.NoError(t, err)

		mu.Lock()
		requests = append(requests, reqMap)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success": true, "code": 0, "data": "739206374277809665"}`))
	}))
	defer server.Close()

	client := futures.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})

	err := client.PlaceTPSL(context.Background(), exchange.TPSLRequest{
		Symbol:          "ETH_USDT",
		PositionMode:    domain.PositionModeHedge,
		Side:            domain.SideOpenShort,
		OpenType:        domain.OpenTypeCross,
		TakeProfitPrice: 2800.0,
		StopLossPrice:   3200.0,
		Volume:          5.0,
	})
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, requests, 2)

	// In Short position, closing side is 2 (close short)
	for _, req := range requests {
		assert.Equal(t, "ETH_USDT", req["symbol"])
		assert.Equal(t, float64(2), req["side"])
		assert.Equal(t, float64(5), req["vol"])
		assert.Equal(t, float64(5), req["orderType"])
		assert.Equal(t, float64(2), req["openType"]) // cross margin
		assert.Equal(t, true, req["reduceOnly"])

		triggerPrice, ok := req["triggerPrice"].(float64)
		require.True(t, ok)
		triggerType, ok := req["triggerType"].(float64)
		require.True(t, ok)
		switch triggerPrice {
		case 2800.0:
			// TP for Short: <=
			assert.Equal(t, float64(2), triggerType)
		case 3200.0:
			// SL for Short: >=
			assert.Equal(t, float64(1), triggerType)
		default:
			t.Fatalf("unexpected trigger price: %v", triggerPrice)
		}
	}
}

func TestFuturesClient_PlaceTPSL_EdgeCases(t *testing.T) {
	t.Parallel()

	client := futures.NewClient(nil, "http://localhost", "key", "secret", config.LoggingConfig{})

	// 1. Both prices <= 0 -> no-op, nil error
	err := client.PlaceTPSL(context.Background(), exchange.TPSLRequest{
		Symbol: "BTC_USDT",
		Side:   domain.SideOpenLong,
	})
	require.NoError(t, err)

	// 2. Invalid side -> error
	err = client.PlaceTPSL(context.Background(), exchange.TPSLRequest{
		Symbol:          "BTC_USDT",
		Side:            domain.SideUnknown,
		TakeProfitPrice: 55000.0,
		Volume:          1.0,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid side")

	// 3. Invalid volume -> error
	err = client.PlaceTPSL(context.Background(), exchange.TPSLRequest{
		Symbol:          "BTC_USDT",
		Side:            domain.SideOpenLong,
		TakeProfitPrice: 55000.0,
		Volume:          0,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid volume")
}

func TestFuturesClient_PrepareOrder_NoInlineTPSL(t *testing.T) {
	t.Parallel()

	var recordedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/private/order/create", r.URL.Path)
		var err error
		recordedBody, err = io.ReadAll(r.Body)
		assert.NoError(t, err)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true,"code":0,"data":{"orderId":"mexc-123","ts":1670000000000}}`))
	}))
	defer server.Close()

	client := futures.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})

	dispatch, err := client.PrepareOrder(context.Background(), exchange.SubmitOrderRequest{
		Symbol:          "BTC_USDT",
		Price:           50000,
		Vol:             1,
		Side:            exchange.SideOpenLong,
		Type:            exchange.OrderTypeLimit,
		TakeProfitPrice: 55000,
		StopLossPrice:   45000,
	})
	require.NoError(t, err)

	res, err := dispatch(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "mexc-123", res.OrderID)
	assert.False(t, res.TPSLSubmitted, "TPSLSubmitted must be false for MEXC standalone TP/SL")

	// Verify the body payload has NO stopLossPrice or takeProfitPrice keys
	var payload map[string]any
	err = json.Unmarshal(recordedBody, &payload)
	require.NoError(t, err)
	_, hasTP := payload["takeProfitPrice"]
	_, hasSL := payload["stopLossPrice"]
	assert.False(t, hasTP, "inline takeProfitPrice must not be sent")
	assert.False(t, hasSL, "inline stopLossPrice must not be sent")
}

func TestFuturesClient_CancelAllOpenOrders_WithPlanOrders(t *testing.T) {
	t.Parallel()

	var cancelAllOrdersCalled, cancelAllPlanOrdersCalled bool
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		mu.Lock()
		switch r.URL.Path {
		case "/api/v1/private/planorder/cancel_all":
			cancelAllPlanOrdersCalled = true
			_, _ = w.Write([]byte(`{"success":true,"code":0,"data":null}`))
		case "/api/v1/private/order/cancel_all":
			cancelAllOrdersCalled = true
			_, _ = w.Write([]byte(`{"success":true,"code":0,"data":null}`))
		}
		mu.Unlock()
	}))
	defer server.Close()

	client := futures.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	err := client.CancelAllOpenOrders(context.Background(), "BTC_USDT")
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	assert.True(t, cancelAllPlanOrdersCalled, "CancelAllOpenOrders must cancel plan orders")
	assert.True(t, cancelAllOrdersCalled, "CancelAllOpenOrders must cancel regular orders")
}

func TestFuturesClient_PreWarm_And_OrderHTTPClient(t *testing.T) {
	t.Parallel()

	var pingCount int
	var orderCount int
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/contract/ping":
			pingCount++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true,"code":0}`))
		case "/api/v1/private/order/create":
			orderCount++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true,"code":0,"data":{"orderId":"mexc-prewarm-123","ts":1670000000000}}`))
		}
	}))
	defer server.Close()

	generalClient := server.Client()
	client := futures.NewClient(generalClient, server.URL, "key", "secret", config.LoggingConfig{})

	orderClient := server.Client()
	client.SetOrderHTTPClient(orderClient)

	err := client.PreWarm(context.Background())
	require.NoError(t, err)

	dispatch, err := client.PrepareOrder(context.Background(), exchange.SubmitOrderRequest{
		Symbol: "BTC_USDT",
		Price:  50000,
		Vol:    1,
		Side:   exchange.SideOpenLong,
		Type:   exchange.OrderTypeLimit,
	})
	require.NoError(t, err)

	res, err := dispatch(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "mexc-prewarm-123", res.OrderID)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, pingCount)
	assert.Equal(t, 1, orderCount)
}

func TestFuturesClient_ClosePosition(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var capturedReq map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/private/order/create", r.URL.Path)
		bodyBytes, err := io.ReadAll(r.Body)
		assert.NoError(t, err)

		var reqMap map[string]any
		assert.NoError(t, json.Unmarshal(bodyBytes, &reqMap))

		mu.Lock()
		capturedReq = reqMap
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true,"code":0,"data":{"orderId":"close-ord-123","ts":1670000000000}}`))
	}))
	defer server.Close()

	client := futures.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})

	err := client.ClosePosition(context.Background(), "BTC_USDT", domain.SideCloseLong, 1.5, domain.PositionModeHedge, 5)
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "BTC_USDT", capturedReq["symbol"])
	assert.Equal(t, float64(4), capturedReq["side"]) // CloseLong = 4
	assert.Equal(t, 1.5, capturedReq["vol"])
	assert.Equal(t, true, capturedReq["reduceOnly"])
	assert.Equal(t, float64(5), capturedReq["leverage"])
}

func TestFuturesClient_CloseAllPositions(t *testing.T) {
	t.Parallel()

	setupMockServer := func() (*httptest.Server, *[]map[string]any) {
		var mu sync.Mutex
		closedOrders := make([]map[string]any, 0)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/api/v1/private/planorder/cancel_all", "/api/v1/private/order/cancel_all":
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"success":true,"code":0}`))
			case "/api/v1/private/position/open_positions":
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{
					"success": true,
					"code": 0,
					"data": [
						{
							"positionId": 12345,
							"symbol": "BTC_USDT",
							"positionType": 1,
							"holdVol": 2.0,
							"leverage": 10
						},
						{
							"positionId": 67890,
							"symbol": "ETH_USDT",
							"positionType": 2,
							"holdVol": 10.0,
							"leverage": 5
						}
					]
				}`))
			case "/api/v1/private/order/create":
				bodyBytes, err := io.ReadAll(r.Body)
				assert.NoError(t, err)

				var reqMap map[string]any
				assert.NoError(t, json.Unmarshal(bodyBytes, &reqMap))

				mu.Lock()
				closedOrders = append(closedOrders, reqMap)
				mu.Unlock()

				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"success":true,"code":0,"data":{"orderId":"close-123","ts":1670000000000}}`))
			default:
				t.Fatalf("unexpected request to %s", r.URL.Path)
			}
		}))
		return server, &closedOrders
	}

	t.Run("all symbols", func(t *testing.T) {
		t.Parallel()
		server, closedOrders := setupMockServer()
		defer server.Close()

		client := futures.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
		err := client.CloseAllPositions(context.Background(), "")
		require.NoError(t, err)
		require.Len(t, *closedOrders, 2)
	})

	t.Run("targeted symbol", func(t *testing.T) {
		t.Parallel()
		server, closedOrders := setupMockServer()
		defer server.Close()

		client := futures.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
		err := client.CloseAllPositions(context.Background(), "BTC_USDT")
		require.NoError(t, err)
		require.Len(t, *closedOrders, 1)
		assert.Equal(t, "BTC_USDT", (*closedOrders)[0]["symbol"])
		assert.Equal(t, float64(4), (*closedOrders)[0]["side"])
		assert.Equal(t, 2.0, (*closedOrders)[0]["vol"])
		assert.Equal(t, true, (*closedOrders)[0]["reduceOnly"])
	})
}
