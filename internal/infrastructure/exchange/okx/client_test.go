package okx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

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
			"data": [{"epoch": "1597026383085"}]
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

func TestClient_GetFundingRate(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v5/public/funding-rate", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
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
	}))
	defer server.Close()

	client := okx.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	fr, err := client.GetFundingRate(context.Background(), "BTC-USDT-SWAP")
	require.NoError(t, err)
	assert.Equal(t, 0.0001, fr.FundingRate)
	assert.Equal(t, int64(1597026383085), fr.NextSettleTime)
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

	orderID, err := client.CreateOrder(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "987654", orderID)
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

func TestClient_GetAssets(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v5/account/balance", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "0",
			"msg": "",
			"data": [
				{
					"details": [
						{
							"ccy": "USDT",
							"eq": "1000.5",
							"availBal": "800.0",
							"frozenBal": "200.5",
							"upl": "10.0"
						}
					]
				}
			]
		}`))
	}))
	defer server.Close()

	client := okx.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	assets, err := client.GetAssets(context.Background())
	require.NoError(t, err)
	require.Len(t, assets, 1)
	assert.Equal(t, "USDT", assets[0].Currency)
	assert.Equal(t, 1000.5, assets[0].Equity)
	assert.Equal(t, 800.0, assets[0].AvailableBalance)
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
				}
			]
		}`))
	}))
	defer server.Close()

	client := okx.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	positions, err := client.GetOpenPositions(context.Background(), "")
	require.NoError(t, err)
	require.Len(t, positions, 1)
	assert.Equal(t, "BTC-USDT-SWAP", positions[0].Symbol)
	assert.Equal(t, 1.0, positions[0].HoldVol)
	assert.Equal(t, 10, positions[0].Leverage)
	assert.Equal(t, 1, positions[0].PositionType)
}

func TestClient_GetKlines(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v5/market/candles", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "0",
			"msg": "",
			"data": [
				["1597026300000", "50000.0", "50001.0", "49999.0", "50000.5", "10", "500000"]
			]
		}`))
	}))
	defer server.Close()

	client := okx.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	klines, err := client.GetKlines(context.Background(), "BTC-USDT-SWAP", "1m", 0, 0)
	require.NoError(t, err)
	require.Len(t, klines, 1)
	assert.Equal(t, int64(1597026300000), klines[0].Timestamp)
	assert.Equal(t, 50000.0, klines[0].Open)
	assert.Equal(t, 50000.5, klines[0].Close)
}

func TestClient_GetDepthSnapshot(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v5/market/books", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "0",
			"msg": "",
			"data": [
				{
					"asks": [["50001.0", "1.5"]],
					"bids": [["50000.0", "2.0"]],
					"ts": "1597026383085"
				}
			]
		}`))
	}))
	defer server.Close()

	client := okx.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	ob, err := client.GetDepthSnapshot(context.Background(), "BTC-USDT-SWAP", 5)
	require.NoError(t, err)
	assert.Equal(t, "BTC-USDT-SWAP", ob.Symbol)
	require.Len(t, ob.Asks, 1)
	require.Len(t, ob.Bids, 1)
	assert.Equal(t, 50001.0, ob.Asks[0].Price)
	assert.Equal(t, 1.5, ob.Asks[0].Volume)
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
	err := client.ClosePosition(context.Background(), "BTC-USDT-SWAP", domain.SideCloseLong, 1.0, 1)
	require.NoError(t, err)
}

func TestClient_ChangeLeverage(t *testing.T) {
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
}

func TestClient_GetAssetByCurrency(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v5/account/balance", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "0",
			"msg": "",
			"data": [
				{
					"details": [
						{
							"ccy": "USDT",
							"eq": "1000.5",
							"availBal": "800.0",
							"frozenBal": "200.5",
							"upl": "10.0"
						}
					]
				}
			]
		}`))
	}))
	defer server.Close()

	client := okx.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	asset, err := client.GetAssetByCurrency(context.Background(), "USDT")
	require.NoError(t, err)
	assert.Equal(t, "USDT", asset.Currency)
}

func TestClient_GetOrder_and_GetOpenOrders(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
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
	info, err := client.GetOrder(context.Background(), "order123")
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

	// 1. CreateTrackOrder unimplemented
	client := okx.NewClient(nil, "", "key", "secret", "pass", config.LoggingConfig{})
	_, err := client.CreateTrackOrder(context.Background(), exchange.SubmitTrackOrderRequest{})
	assert.ErrorContains(t, err, "CreateTrackOrder not implemented")

	// 2. CancelOrders unimplemented
	err = client.CancelOrders(context.Background(), []string{"1"})
	assert.ErrorContains(t, err, "order not found")
}
