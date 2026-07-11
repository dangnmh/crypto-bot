package krakenfutures_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/krakenfutures"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_FetchKlines(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/charts/v1/trade/PF_XBTUSD/1m", r.URL.Path)

		q := r.URL.Query()
		assert.Equal(t, "1783681200000", q.Get("from"))
		assert.Equal(t, "1783683000000", q.Get("to"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"candles": [
				{
					"time": 1783681200000,
					"open": "64373.4",
					"high": "64448.5",
					"low": "64328.4",
					"close": "64365",
					"volume": "91.882"
				},
				{
					"time": 1783681260000,
					"open": "64365.0",
					"high": "64370.0",
					"low": "64350.0",
					"close": "64360.0",
					"volume": "10.5"
				}
			]
		}`))
	}))
	defer server.Close()

	client := krakenfutures.NewClient(server.Client(), server.URL, config.LoggingConfig{})
	klines, err := client.FetchKlines(
		context.Background(),
		"BTCUSD",
		exchange.Interval1m,
		time.UnixMilli(1783681200000),
		time.UnixMilli(1783683000000),
	)
	require.NoError(t, err)
	require.Len(t, klines, 2)

	assert.Equal(t, int64(1783681200000), klines[0].Timestamp)
	assert.Equal(t, 64373.4, klines[0].Open)
	assert.Equal(t, 64448.5, klines[0].High)
	assert.Equal(t, 64328.4, klines[0].Low)
	assert.Equal(t, 64365.0, klines[0].Close)
	assert.Equal(t, 91.882, klines[0].Volume)

	assert.Equal(t, int64(1783681260000), klines[1].Timestamp)
	assert.Equal(t, 64365.0, klines[1].Open)
	assert.Equal(t, 64370.0, klines[1].High)
	assert.Equal(t, 64350.0, klines[1].Low)
	assert.Equal(t, 64360.0, klines[1].Close)
	assert.Equal(t, 10.5, klines[1].Volume)
}

func TestClient_FetchKlines_Error(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"result":"error","error":"invalid resolution"}`))
	}))
	defer server.Close()

	client := krakenfutures.NewClient(server.Client(), server.URL, config.LoggingConfig{})
	_, err := client.FetchKlines(
		context.Background(),
		"BTCUSD",
		exchange.Interval1m,
		time.Time{},
		time.Time{},
	)
	assert.Error(t, err)
}
