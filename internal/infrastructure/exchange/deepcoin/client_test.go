package deepcoin_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-bot/internal/infrastructure/config"
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
			"data": {"ts": "1739242026000"}
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
