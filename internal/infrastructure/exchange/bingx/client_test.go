package bingx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-bot/internal/infrastructure/config"
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
		assert.Equal(t, "/openApi/swap/v2/quote/klines", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 0,
			"msg": "success",
			"data": [
				[1695812285000, "50000.0", "50001.0", "49999.0", "50000.5", "10"]
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
