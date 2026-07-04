package deepcoin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
							"instrumentId": "BTC-USDT-SWAP",
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
		case r.URL.Path == "/deepcoin/trade/orderByID" && r.Method == http.MethodGet:
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
					"state": "filled",
					"cTime": "1739263130000"
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

func TestClient_GetOrderPNL(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/deepcoin/trade/orderByID":
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
					"state": "filled",
					"cTime": "1739263130000"
				}]
			}`))
		case "/deepcoin/account/positions-history":
			_, _ = w.Write([]byte(`{
				"code": "0",
				"data": [{
					"instId": "BTC-USDT-SWAP",
					"closeAvgPx": "96000",
					"openAvgPx": "95000",
					"pnl": "1000",
					"closeTotalPos": "1",
					"cTime": "1739263130000",
					"uTime": "1739263150000",
					"fee": "10",
					"fundingFee": "5",
					"realizedPnl": "985",
					"posSide": "long",
					"direction": "long"
				}]
			}`))
		default:
			t.Fatalf("unexpected call to path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := deepcoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	pnl, err := client.GetOrderPNL(context.Background(), "BTC-USDT-SWAP", "10001")
	require.NoError(t, err)
	assert.Equal(t, "BTC-USDT-SWAP", pnl.Symbol)
	assert.Equal(t, 95000.0, pnl.EntryPrice)
	assert.Equal(t, 96000.0, pnl.ExitPrice)
	assert.Equal(t, 1.0, pnl.ClosedSize)
	assert.Equal(t, 1000.0, pnl.GrossPnL)
	assert.Equal(t, 10.0, pnl.Fee)
	assert.Equal(t, 5.0, pnl.FundingFee)
	assert.Equal(t, 985.0, pnl.NetPnl)
	assert.Equal(t, int64(20000), pnl.DurationMs)
}

func TestClient_CloseAllPositions(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		assert.Equal(t, "/deepcoin/trade/batch-close-position", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)
		_, _ = w.Write([]byte(`{"code":"0","msg":"","data":{"errorList":[]}}`))
	}))
	defer server.Close()

	client := deepcoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	err := client.CloseAllPositions(context.Background(), "BTC-USDT-SWAP")
	assert.NoError(t, err)
}

func TestClient_CloseAllPositions_Failure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		assert.Equal(t, "/deepcoin/trade/batch-close-position", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)
		_, _ = w.Write([]byte(`{
			"code": "0",
			"msg": "",
			"data": {
				"errorList": [
					{
						"memberId": "10001",
						"accountId": "100001234",
						"tradeUnitId": "TU001",
						"instId": "BTC-USDT-SWAP",
						"posiDirection": "long",
						"errorCode": 51020,
						"errorMsg": "Insufficient position"
					}
				]
			}
		}`))
	}))
	defer server.Close()

	client := deepcoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	err := client.CloseAllPositions(context.Background(), "BTC-USDT-SWAP")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to close long position for BTC-USDT-SWAP: Insufficient position (code 51020)")
}

func TestClient_RawRequestMethods(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"0","data":"raw-data"}`))
	}))
	defer server.Close()

	client := deepcoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})

	bytes, err := client.GetFundingRateRaw(context.Background(), nil)
	assert.NoError(t, err)
	assert.Contains(t, string(bytes), "raw-data")

	bytes, err = client.GetTickersRaw(context.Background(), nil)
	assert.NoError(t, err)
	assert.Contains(t, string(bytes), "raw-data")

	bytes, err = client.GetOpenPositionsRaw(context.Background(), nil)
	assert.NoError(t, err)
	assert.Contains(t, string(bytes), "raw-data")

	bytes, err = client.GetHistoryPositionsRaw(context.Background(), nil)
	assert.NoError(t, err)
	assert.Contains(t, string(bytes), "raw-data")

	bytes, err = client.GetOrderDetailRaw(context.Background(), "123", nil)
	assert.NoError(t, err)
	assert.Contains(t, string(bytes), "raw-data")

	bytes, err = client.GetHistoryOrdersRaw(context.Background(), nil)
	assert.NoError(t, err)
	assert.Contains(t, string(bytes), "raw-data")

	bytes, err = client.GetOrderPNLRaw(context.Background(), nil)
	assert.NoError(t, err)
	assert.Contains(t, string(bytes), "raw-data")
}

func TestClient_GetPotentialFundingSymbols(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/deepcoin/market/tickers":
			_, _ = w.Write([]byte(`{
				"code": "0",
				"msg": "",
				"data": [
					{
						"instType": "SWAP",
						"instId": "BTC-USDT-SWAP",
						"last": "64000",
						"volCcy24h": "15000000"
					}
				]
			}`))
		case "/deepcoin/trade/funding-rate":
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
			_, _ = w.Write([]byte(`{
				"code": "0",
				"msg": "",
				"data": {
					"current_fund_rates": [
						{
							"instrumentId": "BTC-USDT-SWAP",
							"fundingRate": 0.0001
						}
					]
				}
			}`))
		case "/deepcoin/market/time":
			_, _ = w.Write([]byte(`{"code":"0","data":[{"ts":"1700000000"}]}`))
		default:
			t.Fatalf("unexpected call to path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := deepcoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	res, err := client.GetPotentialFundingSymbols(context.Background(), 10000000, 0, nil, nil)
	require.NoError(t, err)
	require.Len(t, res, 1)
	assert.Equal(t, "BTC-USDT-SWAP", res[0].Symbol)
	assert.Equal(t, 0.0001, res[0].Rate)
	assert.Equal(t, int64(1739289600000), res[0].SettleTime)

	// test Latency
	_, err = client.Latency(context.Background())
	require.NoError(t, err)

	// test SupportLeverageOnOrder
	assert.False(t, client.SupportLeverageOnOrder())

	// test SwitchMarginMode
	err = client.SwitchMarginMode(context.Background(), "BTC-USDT-SWAP", domain.MarginModeIsolated, 10, domain.SideOpenLong)
	require.NoError(t, err)

	// test WarmUp
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client.WarmUp(ctx, 10*time.Millisecond)
}
