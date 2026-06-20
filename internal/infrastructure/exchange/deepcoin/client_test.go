package deepcoin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/deepcoin"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSignRequest(t *testing.T) {
	t.Parallel()

	// Signature verification test using sample inputs
	secret := "secret"
	timestamp := "2026-06-20T12:00:00.000Z"
	method := "GET"
	path := "/deepcoin/market/tickers"
	body := ""

	sig := deepcoin.SignRequest(secret, timestamp, method, path, body)
	assert.NotEmpty(t, sig)
}

func TestClient_GetServerTime(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/deepcoin/market/time", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "0",
			"msg": "",
			"data": {"ts": 1739242026000}
		}`))
	}))
	defer server.Close()

	client := deepcoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	ts, err := client.GetServerTime(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1739242026000), ts)
}

func TestClient_GetContractDetails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/deepcoin/market/instruments", r.URL.Path)
		assert.Equal(t, "SWAP", r.URL.Query().Get("instType"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "0",
			"msg": "",
			"data": [
				{
					"instType": "SWAP",
					"instId": "BTC-USDT-SWAP",
					"baseCcy": "BTC",
					"settleCcy": "USDT",
					"ctVal": "0.001",
					"lever": "125",
					"tickSz": "0.1",
					"lotSz": "1",
					"minSz": "1",
					"state": "live"
				}
			]
		}`))
	}))
	defer server.Close()

	client := deepcoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	details, err := client.GetContractDetails(context.Background())
	require.NoError(t, err)
	require.Len(t, details, 1)
	assert.Equal(t, "BTC-USDT-SWAP", details[0].Symbol)
	assert.Equal(t, 0.001, details[0].ContractSize)
	assert.Equal(t, 125, details[0].MaxLeverage)
}

func TestClient_GetTickers(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/deepcoin/market/tickers", r.URL.Path)
		assert.Equal(t, "SWAP", r.URL.Query().Get("instType"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "0",
			"msg": "",
			"data": [
				{
					"instType": "SWAP",
					"instId": "BTC-USDT-SWAP",
					"last": "96127.5",
					"askPx": "96127.8",
					"bidPx": "96127.3",
					"vol24h": "5350671",
					"volCcy24h": "55.814169",
					"ts": "1739242026000"
				}
			]
		}`))
	}))
	defer server.Close()

	client := deepcoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	tickers, err := client.GetTickers(context.Background(), "")
	require.NoError(t, err)
	require.Len(t, tickers, 1)
	assert.Equal(t, "BTC-USDT-SWAP", tickers[0].Symbol)
	assert.Equal(t, 96127.5, tickers[0].LastPrice)
	assert.Equal(t, 96127.3, tickers[0].Bid1)
	assert.Equal(t, 96127.8, tickers[0].Ask1)
}

func TestClient_GetFundingRates(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/deepcoin/trade/funding-rate":
			assert.Equal(t, "SwapU", r.URL.Query().Get("instType"))
			_, _ = w.Write([]byte(`{
				"code": "0",
				"msg": "",
				"data": [
					{
						"settleInterval": 28800,
						"instrumentID": "BTC-USDT-SWAP",
						"nextSettleTime": 1739289600
					}
				]
			}`))
		case "/deepcoin/trade/fund-rate/current-funding-rate":
			assert.Equal(t, "SwapU", r.URL.Query().Get("instType"))
			_, _ = w.Write([]byte(`{
				"code": "0",
				"msg": "",
				"data": {
					"current_fund_rates": [
						{
							"instrumentId": "BTCUSDT",
							"fundingRate": 0.0001
						}
					]
				}
			}`))
		default:
			t.Fatalf("unexpected call to path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := deepcoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	rates, err := client.GetFundingRates(context.Background(), []string{"BTC-USDT-SWAP"})
	require.NoError(t, err)
	require.Len(t, rates, 1)
	assert.Equal(t, "BTC-USDT-SWAP", rates[0].Symbol)
	assert.Equal(t, 0.0001, rates[0].Rate)
	assert.Equal(t, int64(1739289600000), rates[0].SettleTime)
}

func TestClient_GetOpenPositions(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/deepcoin/account/positions" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"code": "0",
				"msg": "",
				"data": [
					{
						"instId": "BTC-USDT-SWAP",
						"posSide": "long",
						"pos": "12",
						"avgPx": "95000",
						"lever": "10",
						"ccy": "USDT"
					}
				]
			}`))
		}
	}))
	defer server.Close()

	client := deepcoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	positions, err := client.GetOpenPositions(context.Background(), "BTC-USDT-SWAP")
	assert.NoError(t, err)
	assert.Len(t, positions, 1)
	assert.Equal(t, "BTC-USDT-SWAP", positions[0].Symbol)
	assert.Equal(t, exchange.PositionTypeLong, positions[0].PositionType)
	assert.Equal(t, 12.0, positions[0].HoldVol)
	assert.Equal(t, 95000.0, positions[0].HoldAvgPrice)
}

func TestClient_OrderLifecycle(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		switch {
		case r.URL.Path == "/deepcoin/trade/order" && r.Method == http.MethodPost:
			var bodyMap map[string]any
			_ = json.NewDecoder(r.Body).Decode(&bodyMap)
			assert.Equal(t, "96000", bodyMap["tpTriggerPx"])
			assert.Equal(t, "94000", bodyMap["slTriggerPx"])
			assert.Equal(t, "cross", bodyMap["tdMode"])
			_, _ = w.Write([]byte(`{"code":"0","msg":"","data":{"ordId":"10001","clOrdId":"cl-1","sCode":"0","sMsg":""}}`))
		case r.URL.Path == "/deepcoin/trade/cancel-order":
			_, _ = w.Write([]byte(`{"code":"0","msg":"","data":{"ordId":"10001","sCode":"0"}}`))
		case r.URL.Path == "/deepcoin/trade/order" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{
				"code": "0",
				"data": [{
					"instId": "BTC-USDT-SWAP",
					"ordId": "10001",
					"px": "95000",
					"sz": "1",
					"ordType": "limit",
					"side": "buy",
					"posSide": "long",
					"accFillSz": "1",
					"avgPx": "95000",
					"state": "filled"
				}]
			}`))
		}
	}))
	defer server.Close()

	client := deepcoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})

	// Placements
	res, err := client.CreateOrder(context.Background(), exchange.SubmitOrderRequest{
		Symbol:          "BTC-USDT-SWAP",
		Side:            exchange.SideOpenLong,
		Type:            exchange.OrderTypeLimit,
		Vol:             1.0,
		Price:           95000.0,
		TakeProfitPrice: 96000.0,
		StopLossPrice:   94000.0,
		OpenType:        domain.OpenTypeCross,
	})
	assert.NoError(t, err)
	assert.Equal(t, "10001", res.OrderID)
	assert.True(t, res.TPSLSubmitted)

	// Query
	order, err := client.GetOrder(context.Background(), "BTC-USDT-SWAP", "10001")
	assert.NoError(t, err)
	assert.Equal(t, exchange.OrderStateFilled, order.State)

	// Cancel
	err = client.CancelOrder(context.Background(), "BTC-USDT-SWAP", "10001")
	assert.NoError(t, err)
}

func TestClient_SetLeverage(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/deepcoin/account/set-leverage" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"code":"0","msg":"","data":{"sCode":"0"}}`))
		}
	}))
	defer server.Close()

	client := deepcoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	err := client.ChangeLeverage(context.Background(), exchange.ChangeLeverageRequest{
		Symbol:   "BTC-USDT-SWAP",
		Leverage: 10,
	})
	assert.NoError(t, err)
}
