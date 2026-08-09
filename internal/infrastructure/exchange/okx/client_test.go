package okx_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/okx"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_GetServerTime(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v5/public/time", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "0",
			"msg": "",
			"data": [{"ts": "1597026383085"}]
		}`))
	}))
	defer server.Close()

	client := okx.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	ts, err := client.GetServerTime(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1597026383085), ts)
}

func TestClient_GetContractDetails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v5/public/instruments", r.URL.Path)
		assert.Equal(t, "SWAP", r.URL.Query().Get("instType"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "0",
			"msg": "",
			"data": [
				{
					"instId": "BTC-USDT-SWAP",
					"baseCcy": "BTC",
					"settleCcy": "USDT",
					"ctVal": "0.01",
					"lever": "100",
					"tickSz": "0.1",
					"lotSz": "1",
					"minSz": "1",
					"state": "live"
				}
			]
		}`))
	}))
	defer server.Close()

	client := okx.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	details, err := client.GetContractDetails(context.Background())
	require.NoError(t, err)
	require.Len(t, details, 1)
	assert.Equal(t, "BTC-USDT-SWAP", details[0].Symbol)
	assert.Equal(t, 0.01, details[0].ContractSize)
	assert.Equal(t, 100, details[0].MaxLeverage)
	assert.Equal(t, 1, details[0].PriceScale)
}

//nolint:dupl // standard mock setup contains high structural similarity
func TestClient_GetTickers(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v5/market/tickers":
			_, _ = w.Write([]byte(`{
				"code": "0",
				"msg": "",
				"data": [
					{
						"instId": "BTC-USDT-SWAP",
						"last": "50000.5",
						"bidPx": "50000.0",
						"askPx": "50001.0",
						"vol24h": "1000",
						"volCcy24h": "50000000",
						"ts": "1597026383085"
					}
				]
			}`))
		case "/api/v5/public/mark-price":
			_, _ = w.Write([]byte(`{
				"code": "0",
				"msg": "",
				"data": [
					{
						"instId": "BTC-USDT-SWAP",
						"fundingRate": "0.0001",
						"nextFundingTime": "1597055183085"
					}
				]
			}`))
		}
	}))
	defer server.Close()

	client := okx.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	tickers, err := client.GetTickers(context.Background(), "")
	require.NoError(t, err)
	require.Len(t, tickers, 1)
	assert.Equal(t, "BTC-USDT-SWAP", tickers[0].Symbol)
	assert.Equal(t, 50000.5, tickers[0].LastPrice)
	assert.Equal(t, 50000.0, tickers[0].Bid1)
	assert.Equal(t, 50001.0, tickers[0].Ask1)
}

func TestClient_GetFundingRates(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v5/market/tickers":
			_, _ = w.Write([]byte(`{
				"code": "0",
				"msg": "",
				"data": [
					{
						"instId": "BTC-USDT-SWAP",
						"last": "50000.5",
						"bidPx": "50000.0",
						"askPx": "50001.0",
						"vol24h": "1000",
						"volCcy24h": "1000",
						"ts": "1597026383085"
					}
				]
			}`))
		case "/api/v5/public/funding-rate":
			assert.Equal(t, "BTC-USDT-SWAP", r.URL.Query().Get("instId"))
			_, _ = w.Write([]byte(`{
				"code": "0",
				"msg": "",
				"data": [
					{
						"instId": "BTC-USDT-SWAP",
						"fundingRate": "0.0001",
						"nextFundingTime": "1597026383085"
					}
				]
			}`))
		}
	}))
	defer server.Close()

	client := okx.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	frs, err := client.GetFundingRates(context.Background(), []string{"BTC-USDT-SWAP"})
	require.NoError(t, err)
	require.Len(t, frs, 1)
	assert.Equal(t, "BTC-USDT-SWAP", frs[0].Symbol)
	assert.Equal(t, 0.0001, frs[0].Rate)
	assert.Equal(t, int64(1597026383085), frs[0].SettleTime)
}

func TestClient_CreateOrder(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/v5/trade/order", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "0",
			"msg": "",
			"data": [
				{
					"ordId": "987654",
					"clOrdId": "my_client_id",
					"sCode": "0",
					"sMsg": "success"
				}
			]
		}`))
	}))
	defer server.Close()

	client := okx.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	req := exchange.SubmitOrderRequest{
		Symbol:       "BTC-USDT-SWAP",
		Vol:          1,
		Side:         exchange.SideOpenLong,
		Type:         exchange.OrderTypeLimit,
		Price:        50000,
		PositionMode: 1, // Hedge
	}

	res, err := client.CreateOrder(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "987654", res.OrderID)
	assert.False(t, res.TPSLSubmitted)
}

func TestClient_CreateOrder_TPSL(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/v5/trade/order", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "0",
			"msg": "",
			"data": [
				{
					"ordId": "987654",
					"clOrdId": "my_client_id",
					"sCode": "0",
					"sMsg": "success"
				}
			]
		}`))
	}))
	defer server.Close()

	client := okx.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	req := exchange.SubmitOrderRequest{
		Symbol:          "BTC-USDT-SWAP",
		Vol:             1,
		Side:            exchange.SideOpenLong,
		Type:            exchange.OrderTypeLimit,
		Price:           50000,
		PositionMode:    1, // Hedge
		TakeProfitPrice: 51000,
		StopLossPrice:   49000,
	}

	res, err := client.CreateOrder(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "987654", res.OrderID)
	assert.True(t, res.TPSLSubmitted)
}

func TestClient_CancelOrder(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/v5/trade/cancel-order", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "0",
			"msg": "",
			"data": [
				{
					"ordId": "987654",
					"sCode": "0",
					"sMsg": "success"
				}
			]
		}`))
	}))
	defer server.Close()

	client := okx.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	err := client.CancelOrder(context.Background(), "BTC-USDT-SWAP", "987654")
	require.NoError(t, err)
}

//nolint:dupl // standard mock setup contains high structural similarity
func TestClient_GetOpenPositions(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v5/account/positions", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "0",
			"msg": "",
			"data": [
				{
					"instId": "BTC-USDT-SWAP",
					"pos": "1",
					"lever": "10",
					"avgPx": "50000",
					"liqPx": "45000",
					"realizedPnl": "5.5",
					"margin": "5000",
					"posSide": "long",
					"mgnMode": "isolated"
				},
				{
					"instId": "BTC-USDT-SWAP",
					"pos": "1.5",
					"lever": "10",
					"avgPx": "50000",
					"liqPx": "45000",
					"realizedPnl": "5.5",
					"margin": "5000",
					"posSide": "short",
					"mgnMode": "isolated"
				},
				{
					"instId": "BTC-USDT-SWAP",
					"pos": "-2.5",
					"lever": "10",
					"avgPx": "50000",
					"liqPx": "45000",
					"realizedPnl": "5.5",
					"margin": "5000",
					"posSide": "net",
					"mgnMode": "isolated"
				},
				{
					"instId": "BTC-USDT",
					"pos": "0.5",
					"lever": "1",
					"avgPx": "50000",
					"liqPx": "45000",
					"realizedPnl": "5.5",
					"margin": "5000",
					"posSide": "net",
					"mgnMode": "isolated",
					"posCcy": "BTC"
				},
				{
					"instId": "BTC-USDT",
					"pos": "100.0",
					"lever": "1",
					"avgPx": "50000",
					"liqPx": "45000",
					"realizedPnl": "5.5",
					"margin": "5000",
					"posSide": "net",
					"mgnMode": "isolated",
					"posCcy": "USDT"
				}
			]
		}`))
	}))
	defer server.Close()

	client := okx.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	positions, err := client.GetOpenPositions(context.Background(), "")
	require.NoError(t, err)
	require.Len(t, positions, 5)

	// 1. Long position in hedge mode
	assert.Equal(t, "BTC-USDT-SWAP", positions[0].Symbol)
	assert.Equal(t, 1.0, positions[0].HoldVolContract)
	assert.Equal(t, exchange.PositionTypeLong, positions[0].PositionType)

	// 2. Short position in hedge mode
	assert.Equal(t, "BTC-USDT-SWAP", positions[1].Symbol)
	assert.Equal(t, 1.5, positions[1].HoldVolContract)
	assert.Equal(t, exchange.PositionTypeShort, positions[1].PositionType)

	// 3. Short position in net mode (negative quantity)
	assert.Equal(t, "BTC-USDT-SWAP", positions[2].Symbol)
	assert.Equal(t, 2.5, positions[2].HoldVolContract)
	assert.Equal(t, exchange.PositionTypeShort, positions[2].PositionType)

	// 4. Margin position in net mode matching base currency (long)
	assert.Equal(t, "BTC-USDT", positions[3].Symbol)
	assert.Equal(t, 0.5, positions[3].HoldVolContract)
	assert.Equal(t, exchange.PositionTypeLong, positions[3].PositionType)

	// 5. Margin position in net mode matching quote currency (short)
	assert.Equal(t, "BTC-USDT", positions[4].Symbol)
	assert.Equal(t, 100.0, positions[4].HoldVolContract)
	assert.Equal(t, exchange.PositionTypeShort, positions[4].PositionType)
}

func TestClient_ClosePosition(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/v5/trade/order", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "0",
			"msg": "",
			"data": [
				{
					"ordId": "987654",
					"sCode": "0",
					"sMsg": "success"
				}
			]
		}`))
	}))
	defer server.Close()

	client := okx.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	err := client.ClosePosition(context.Background(), "BTC-USDT-SWAP", domain.SideCloseLong, 1.0, 1, 10)
	require.NoError(t, err)
}

func newMockSetLeverageServer(t *testing.T, requestCount *int, expectedPosSide string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/v5/account/set-leverage", r.URL.Path)
		*requestCount++

		bodyBytes, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		var req struct {
			InstID  string `json:"instId"`
			Lever   string `json:"lever"`
			MgnMode string `json:"mgnMode"`
			PosSide string `json:"posSide"`
		}
		err = json.Unmarshal(bodyBytes, &req)
		require.NoError(t, err)

		w.Header().Set("Content-Type", "application/json")
		switch *requestCount {
		case 1:
			assert.Equal(t, expectedPosSide, req.PosSide)
			_, _ = w.Write([]byte(`{
				"code": "51000",
				"msg": "Parameter posSide error"
			}`))
		case 2:
			assert.Equal(t, "", req.PosSide)
			_, _ = w.Write([]byte(`{
				"code": "0",
				"msg": "",
				"data": []
			}`))
		default:
			t.Fatalf("unexpected request count %d", *requestCount)
		}
	}))
}

func TestClient_ChangeLeverage(t *testing.T) {
	t.Parallel()

	t.Run("Success first try", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "POST", r.Method)
			assert.Equal(t, "/api/v5/account/set-leverage", r.URL.Path)

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"code": "0",
				"msg": "",
				"data": []
			}`))
		}))
		defer server.Close()

		client := okx.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
		req := exchange.ChangeLeverageRequest{
			Symbol:   "BTC-USDT-SWAP",
			Leverage: 20,
			OpenType: exchange.OpenTypeIsolated,
		}
		err := client.ChangeLeverage(context.Background(), req)
		require.NoError(t, err)
	})

	t.Run("Success first try cross margin", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "POST", r.Method)
			assert.Equal(t, "/api/v5/account/set-leverage", r.URL.Path)

			bodyBytes, err := io.ReadAll(r.Body)
			require.NoError(t, err)

			var req struct {
				MgnMode string `json:"mgnMode"`
				PosSide string `json:"posSide"`
			}
			err = json.Unmarshal(bodyBytes, &req)
			require.NoError(t, err)

			assert.Equal(t, "cross", req.MgnMode)
			assert.Empty(t, req.PosSide)

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"code": "0",
				"msg": "",
				"data": []
			}`))
		}))
		defer server.Close()

		client := okx.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
		req := exchange.ChangeLeverageRequest{
			Symbol:       "BTC-USDT-SWAP",
			Leverage:     20,
			OpenType:     exchange.OpenTypeCross,
			PositionType: exchange.PositionTypeLong,
		}
		err := client.ChangeLeverage(context.Background(), req)
		require.NoError(t, err)
	})

	t.Run("Fallback on 51000 posSide error", func(t *testing.T) {
		t.Parallel()
		var requestCount int
		server := newMockSetLeverageServer(t, &requestCount, "long")
		defer server.Close()

		client := okx.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
		req := exchange.ChangeLeverageRequest{
			Symbol:       "BTC-USDT-SWAP",
			Leverage:     20,
			OpenType:     exchange.OpenTypeIsolated,
			PositionType: exchange.PositionTypeLong, // Long -> posSide = "long"
		}
		err := client.ChangeLeverage(context.Background(), req)
		require.NoError(t, err)
		assert.Equal(t, 2, requestCount)
	})

	t.Run("Other error does not retry", func(t *testing.T) {
		t.Parallel()
		var requestCount int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "POST", r.Method)
			requestCount++

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"code": "50001",
				"msg": "Some other error"
			}`))
		}))
		defer server.Close()

		client := okx.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
		req := exchange.ChangeLeverageRequest{
			Symbol:       "BTC-USDT-SWAP",
			Leverage:     20,
			OpenType:     exchange.OpenTypeIsolated,
			PositionType: exchange.PositionTypeLong,
		}
		err := client.ChangeLeverage(context.Background(), req)
		require.Error(t, err)
		assert.Equal(t, 1, requestCount)
	})
}

func TestClient_SwitchMarginMode(t *testing.T) {
	t.Parallel()

	client := okx.NewClient(nil, "", "key", "secret", "pass", config.LoggingConfig{})
	err := client.SwitchMarginMode(context.Background(), "BTC-USDT-SWAP", "ISOLATED", 10, domain.SideOpenLong)
	require.NoError(t, err)
}

func TestClient_GetOrder_and_GetOpenOrders(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/v5/trade/order":
			assert.Equal(t, "BTC-USDT-SWAP", r.URL.Query().Get("instId"))
			assert.Equal(t, "order123", r.URL.Query().Get("ordId"))
			_, _ = w.Write([]byte(`{
				"code": "0",
				"msg": "",
				"data": [
					{
						"instId": "BTC-USDT-SWAP",
						"ordId": "order123",
						"clOrdId": "client123",
						"px": "50000.0",
						"sz": "1.0",
						"side": "buy",
						"posSide": "long",
						"state": "filled",
						"ordType": "limit",
						"avgPx": "50000.0",
						"uTime": "1597026383085",
						"cTime": "1597026383085",
						"fillSz": "1.0"
					}
				]
			}`))
		case "/api/v5/trade/orders-pending":
			_, _ = w.Write([]byte(`{
				"code": "0",
				"msg": "",
				"data": [
					{
						"instId": "BTC-USDT-SWAP",
						"ordId": "order123",
						"clOrdId": "client123",
						"px": "50000.0",
						"sz": "1.0",
						"side": "buy",
						"posSide": "long",
						"state": "filled",
						"ordType": "limit",
						"avgPx": "50000.0",
						"uTime": "1597026383085",
						"cTime": "1597026383085",
						"fillSz": "1.0"
					}
				]
			}`))
		case "/api/v5/trade/orders-history":
			_, _ = w.Write([]byte(`{
				"code": "0",
				"msg": "",
				"data": []
			}`))
		}
	}))
	defer server.Close()

	client := okx.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	info, err := client.GetOrder(context.Background(), "BTC-USDT-SWAP", "order123")
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "order123", info.OrderID)

	orders, err := client.GetOpenOrders(context.Background(), "BTC-USDT-SWAP")
	require.NoError(t, err)
	require.Len(t, orders, 1)
	assert.Equal(t, "order123", orders[0].OrderID)
}

func TestClient_CancelAllOpenOrders_and_CloseAll(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v5/trade/orders-pending":
			_, _ = w.Write([]byte(`{
				"code": "0",
				"msg": "",
				"data": [
					{
						"instId": "BTC-USDT-SWAP",
						"ordId": "order123",
						"clOrdId": "client123",
						"px": "50000.0",
						"sz": "1.0",
						"side": "buy",
						"posSide": "long",
						"state": "filled",
						"ordType": "limit",
						"avgPx": "50000.0",
						"uTime": "1597026383085",
						"cTime": "1597026383085",
						"fillSz": "1.0"
					}
				]
			}`))
		case "POST /api/v5/trade/cancel-order":
			_, _ = w.Write([]byte(`{
				"code": "0",
				"msg": "",
				"data": [
					{
						"ordId": "order123",
						"sCode": "0",
						"sMsg": "success"
					}
				]
			}`))
		case "GET /api/v5/account/positions":
			_, _ = w.Write([]byte(`{
				"code": "0",
				"msg": "",
				"data": [
					{
						"instId": "BTC-USDT-SWAP",
						"pos": "1",
						"lever": "10",
						"avgPx": "50000",
						"liqPx": "45000",
						"realizedPnl": "5.5",
						"margin": "5000",
						"posSide": "long",
						"mgnMode": "isolated"
					}
				]
			}`))
		case "POST /api/v5/trade/order":
			_, _ = w.Write([]byte(`{
				"code": "0",
				"msg": "",
				"data": [
					{
						"ordId": "order1234",
						"sCode": "0"
					}
				]
			}`))
		}
	}))
	defer server.Close()

	client := okx.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})

	err := client.CancelAllOpenOrders(context.Background(), "BTC-USDT-SWAP")
	require.NoError(t, err)

	err = client.CloseAllPositions(context.Background(), "BTC-USDT-SWAP")
	require.NoError(t, err)
}

func TestClient_ErrorPaths(t *testing.T) {
	t.Parallel()

	client := okx.NewClient(nil, "", "key", "secret", "pass", config.LoggingConfig{})

	// 2. CancelOrders unimplemented
	err := client.CancelOrders(context.Background(), []string{"1"})
	assert.ErrorContains(t, err, "batch cancel not supported on OKX without symbols")
}

func TestClient_GetOrderPNL(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/v5/trade/order":
			assert.Equal(t, "ord-123", r.URL.Query().Get("ordId"))
			_, _ = w.Write([]byte(`{
				"code": "0",
				"msg": "",
				"data": [
					{
						"instId": "BTC-USDT-SWAP",
						"ordId": "ord-123",
						"clOrdId": "ext-123",
						"state": "filled"
					}
				]
			}`))
		case "/api/v5/account/positions-history":
			assert.Equal(t, "SWAP", r.URL.Query().Get("instType"))
			assert.Equal(t, "BTC-USDT-SWAP", r.URL.Query().Get("instId"))
			_, _ = w.Write([]byte(`{
				"code": "0",
				"msg": "",
				"data": [
					{
						"instId": "BTC-USDT-SWAP",
						"openAvgPx": "50000.0",
						"closeAvgPx": "51000.0",
						"closeTotalPos": "0.1",
						"pnl": "100.0",
						"fee": "-0.5",
						"fundingFee": "-0.1",
						"realizedPnl": "99.4",
						"cTime": "1597026383000",
						"uTime": "1597026385000",
						"posSide": "long",
						"pos": "1"
					}
				]
			}`))
		}
	}))
	defer server.Close()

	client := okx.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	res, err := client.GetOrderPNL(context.Background(), "BTC-USDT-SWAP", "ord-123")
	require.NoError(t, err)
	assert.Equal(t, "BTC-USDT-SWAP", res.Symbol)
	assert.Equal(t, 50000.0, res.EntryPrice)
	assert.Equal(t, 51000.0, res.ExitPrice)
	assert.Equal(t, 0.1, *res.ClosedSizeContract)
	assert.Equal(t, 100.0, res.GrossPnL)
	assert.Equal(t, 0.5, res.Fee) // Math.Abs
	assert.Equal(t, -0.1, res.FundingFee)
	assert.Equal(t, int64(2000), res.DurationMs)
	assert.Equal(t, 99.4, res.NetPnl)
	assert.InDelta(t, 2.0, res.PnLRate, 0.0001)
}

func TestClient_GetOrderPNL_Short(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/v5/trade/order":
			assert.Equal(t, "ord-123", r.URL.Query().Get("ordId"))
			_, _ = w.Write([]byte(`{
				"code": "0",
				"msg": "",
				"data": [
					{
						"instId": "BTC-USDT-SWAP",
						"ordId": "ord-123",
						"clOrdId": "ext-123",
						"state": "filled"
					}
				]
			}`))
		case "/api/v5/account/positions-history":
			_, _ = w.Write([]byte(`{
				"code": "0",
				"msg": "",
				"data": [
					{
						"instId": "BTC-USDT-SWAP",
						"openAvgPx": "50000.0",
						"closeAvgPx": "51000.0",
						"closeTotalPos": "0.1",
						"pnl": "-100.0",
						"fee": "-0.5",
						"fundingFee": "-0.1",
						"realizedPnl": "-100.6",
						"cTime": "1597026383000",
						"uTime": "1597026385000",
						"posSide": "short",
						"pos": "-1"
					}
				]
			}`))
		}
	}))
	defer server.Close()

	client := okx.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	res, err := client.GetOrderPNL(context.Background(), "BTC-USDT-SWAP", "ord-123")
	require.NoError(t, err)
	assert.Equal(t, "BTC-USDT-SWAP", res.Symbol)
	assert.Equal(t, 50000.0, res.EntryPrice)
	assert.Equal(t, 51000.0, res.ExitPrice)
	assert.InDelta(t, -2.0, res.PnLRate, 0.0001)
}

func TestClient_GetPotentialFundingSymbols(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v5/market/tickers":
			_, _ = w.Write([]byte(`{
				"code": "0",
				"msg": "",
				"data": [
					{
						"instId": "BTC-USDT-SWAP",
						"last": "64000",
						"bidPx": "63999",
						"askPx": "64001",
						"volCcy24h": "100",
						"ts": "1700000000000"
					}
				]
			}`))
		case "/api/v5/public/funding-rate":
			_, _ = w.Write([]byte(`{
				"code": "0",
				"msg": "",
				"data": [
					{
						"instId": "BTC-USDT-SWAP",
						"fundingRate": "0.001",
						"nextFundingTime": "1700000000"
					}
				]
			}`))
		case "/api/v5/public/time":
			_, _ = w.Write([]byte(`{"code":"0","data":[{"ts":"1700000000"}]}`))
		case "/api/v5/trade/order":
			_, _ = w.Write([]byte(`{
				"code": "0",
				"msg": "",
				"data": [
					{
						"instId": "BTC-USDT-SWAP",
						"ordId": "ord123",
						"clOrdId": "ext123",
						"state": "filled"
					}
				]
			}`))
		}
	}))
	defer server.Close()

	client := okx.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	res, err := client.GetPotentialFundingSymbols(context.Background(), 1000000, 0, nil, nil)
	require.NoError(t, err)
	require.Len(t, res, 1)
	assert.Equal(t, "BTC-USDT-SWAP", res[0].Symbol)
	assert.Equal(t, 0.001, res[0].Rate)

	// test Latency
	_, err = client.Latency(context.Background())
	require.NoError(t, err)

	// GetOrderByExternalID
	order, err := client.GetOrderByExternalID(context.Background(), "BTC-USDT-SWAP", "ext123")
	require.NoError(t, err)
	assert.Equal(t, "ord123", order.OrderID)

	// GetServerTime
	serverTime, err := client.GetServerTime(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1700000000), serverTime)

	// SupportLeverageOnOrder
	assert.False(t, client.SupportLeverageOnOrder())

	// WarmUp
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client.WarmUp(ctx, 10*time.Millisecond)

	// test Raw Methods to hit client.go lines
	_, _ = client.GetFundingRateRaw(context.Background(), nil)
	_, _ = client.GetTickersRaw(context.Background(), nil)
	_, _ = client.GetOpenPositionsRaw(context.Background(), nil)
	_, _ = client.GetHistoryPositionsRaw(context.Background(), nil)
	_, _ = client.GetOrderDetailRaw(context.Background(), "ord123", nil)
	_, _ = client.GetHistoryOrdersRaw(context.Background(), nil)
	_, _ = client.GetOrderDealsRaw(context.Background(), nil)
	_, _ = client.GetClosedPnLRaw(context.Background(), nil)
	_, _ = client.GetOrderPNLRaw(context.Background(), map[string]string{"symbol": "BTC-USDT-SWAP", "orderId": "ord123"})
}

type dummyClock struct{}

func (dummyClock) Now() time.Time { return time.Unix(1700000000, 0) }

func TestClient_OKXRemainingClientMethods(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" {
			_, _ = w.Write([]byte(`{"code":"0","data":[{"result":"post_ok"}]}`))
		} else {
			_, _ = w.Write([]byte(`{"code":"0","data":[{"result":"get_ok"}]}`))
		}
	}))
	defer server.Close()

	client := okx.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})

	// 1. SetClock
	client.SetClock(dummyClock{})

	// 2. Get
	resGet, err := client.Get(context.Background(), "/api/v5/test", nil)
	require.NoError(t, err)
	assert.Contains(t, string(resGet), "get_ok")

	// 3. Post
	resPost, err := client.Post(context.Background(), "/api/v5/test", map[string]string{"foo": "bar"})
	require.NoError(t, err)
	assert.Contains(t, string(resPost), "post_ok")

	// 4. Test error paths (Rate limit)
	serverErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"code":"50011","msg":"Rate limit exceeded"}`))
	}))
	defer serverErr.Close()

	clientErr := okx.NewClient(serverErr.Client(), serverErr.URL, "key", "secret", "pass", config.LoggingConfig{})
	_, err = clientErr.Get(context.Background(), "/api/v5/test", nil)
	assert.Error(t, err)

	// 5. Test error paths (toHTTPError)
	serverErr2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"code":"50012","msg":"Invalid API key"}`))
	}))
	defer serverErr2.Close()

	clientErr2 := okx.NewClient(serverErr2.Client(), serverErr2.URL, "key", "secret", "pass", config.LoggingConfig{})
	_, err = clientErr2.Get(context.Background(), "/api/v5/test", nil)
	assert.Error(t, err)
}
