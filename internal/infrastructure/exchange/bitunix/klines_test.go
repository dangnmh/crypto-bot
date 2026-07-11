package bitunix_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/bitunix"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_FetchKlines(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v1/futures/market/kline", r.URL.Path)

		q := r.URL.Query()
		assert.Equal(t, "BTCUSDT", q.Get("symbol"))
		assert.Equal(t, "1m", q.Get("interval"))
		assert.Equal(t, "1783681200000", q.Get("startTime"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 200,
			"data": [
				{
					"open": "64374",
					"high": "64450",
					"low": "64309",
					"close": "64394.2",
					"quoteVol": "542186",
					"baseVol": "2138920",
					"time": 1783681200000
				}
			],
			"msg": "Success"
		}`))
	}))
	defer server.Close()

	client := bitunix.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})

	klines, err := client.FetchKlines(
		context.Background(),
		"BTC_USDT",
		exchange.Interval1m,
		time.Unix(1783681200, 0),
		time.Time{},
	)
	require.NoError(t, err)
	require.Len(t, klines, 1)

	assert.Equal(t, int64(1783681200000), klines[0].Timestamp)
	assert.Equal(t, 64374.0, klines[0].Open)
	assert.Equal(t, 64450.0, klines[0].High)
	assert.Equal(t, 64309.0, klines[0].Low)
	assert.Equal(t, 64394.2, klines[0].Close)
	assert.Equal(t, 542186.0, klines[0].Volume)
	assert.Equal(t, 2138920.0, klines[0].Amount)
}

func TestClient_FetchKlines_Error(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{
			"code": 800021,
			"msg": "System error"
		}`))
	}))
	defer server.Close()

	client := bitunix.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	_, err := client.FetchKlines(
		context.Background(),
		"INVALID",
		exchange.Interval1m,
		time.Time{},
		time.Time{},
	)
	assert.Error(t, err)
}

func TestClient_FetchKlines_TimeString(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v1/futures/market/kline", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 200,
			"data": [
				{
					"open": "64374",
					"high": "64450",
					"low": "64309",
					"close": "64394.2",
					"quoteVol": "542186",
					"baseVol": "2138920",
					"time": "1783681200000"
				}
			],
			"msg": "Success"
		}`))
	}))
	defer server.Close()

	client := bitunix.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})

	klines, err := client.FetchKlines(
		context.Background(),
		"BTC_USDT",
		exchange.Interval1m,
		time.Time{},
		time.Time{},
	)
	require.NoError(t, err)
	require.Len(t, klines, 1)
	assert.Equal(t, int64(1783681200000), klines[0].Timestamp)
}
