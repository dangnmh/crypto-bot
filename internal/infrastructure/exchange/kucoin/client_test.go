package kucoin_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange/kucoin"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_GetServerTime(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v1/timestamp", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "200000",
			"msg": "success",
			"data": 1695812285073
		}`))
	}))
	defer server.Close()

	client := kucoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	ts, err := client.GetServerTime(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1695812285073), ts)
}

func TestClient_GetContractDetails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v1/contracts/active", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "200000",
			"msg": "success",
			"data": [
				{
					"symbol": "XBTUSDTM",
					"baseCurrency": "XBT",
					"quoteCurrency": "USDT",
					"settleCurrency": "USDT",
					"lotSize": 1,
					"tickSize": 0.1,
					"multiplier": 1,
					"status": "Open"
				}
			]
		}`))
	}))
	defer server.Close()

	client := kucoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	details, err := client.GetContractDetails(context.Background())
	require.NoError(t, err)
	require.Len(t, details, 1)
	assert.Equal(t, "XBTUSDTM", details[0].Symbol)
	assert.Equal(t, "XBT", details[0].BaseCoin)
	assert.Equal(t, 1, details[0].PriceScale)
	assert.Equal(t, 0.1, details[0].PriceUnit)
}

func TestClient_GetTickers(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/allTickers":
			_, _ = w.Write([]byte(`{
				"code": "200000",
				"msg": "success",
				"data": [
					{
						"symbol": "XBTUSDTM",
						"bestBidPrice": 50000.0,
						"bestAskPrice": 50001.0,
						"price": 50000.5,
						"volume": 1000,
						"ts": 1695812285073
					}
				]
			}`))
		case "/api/v1/contracts/active":
			_, _ = w.Write([]byte(`{
				"code": "200000",
				"msg": "success",
				"data": [
					{
						"symbol": "XBTUSDTM",
						"fundingRate": 0.0001,
						"nextFundingRateTime": 1695841085073
					}
				]
			}`))
		}
	}))
	defer server.Close()

	client := kucoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	tickers, err := client.GetTickers(context.Background(), "")
	require.NoError(t, err)
	require.Len(t, tickers, 1)
	assert.Equal(t, "XBTUSDTM", tickers[0].Symbol)
	assert.Equal(t, 50000.5, tickers[0].LastPrice)
	assert.Equal(t, 50000.0, tickers[0].Bid1)
	assert.Equal(t, 50001.0, tickers[0].Ask1)
}
