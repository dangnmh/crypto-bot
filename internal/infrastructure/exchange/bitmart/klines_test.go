package bitmart_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/bitmart"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_FetchKlines(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/contract/public/kline", r.URL.Path)

		q := r.URL.Query()
		assert.Equal(t, "BTCUSDT", q.Get("symbol"))
		assert.Equal(t, "60", q.Get("step"))
		assert.Equal(t, "1783681200", q.Get("start_time"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 1000,
			"message": "Ok",
			"data": [
				{
					"low_price": "64309",
					"high_price": "64450",
					"open_price": "64374",
					"close_price": "64394.2",
					"volume": "542186",
					"timestamp": 1783681200
				},
				{
					"low_price": "64241.4",
					"high_price": "64580",
					"open_price": "64399.9",
					"close_price": "64258.8",
					"volume": "2138920",
					"timestamp": 1783684800
				}
			]
		}`))
	}))
	defer server.Close()

	client := bitmart.NewClient(server.Client(), server.URL, "key", "secret", "passphrase", config.LoggingConfig{})
	klines, err := client.FetchKlines(
		context.Background(),
		"BTCUSDT",
		exchange.Interval1h,
		time.Unix(1783681200, 0),
		time.Time{},
	)
	require.NoError(t, err)
	require.Len(t, klines, 2)

	assert.Equal(t, int64(1783681200000), klines[0].Timestamp)
	assert.Equal(t, 64374.0, klines[0].Open)
	assert.Equal(t, 64450.0, klines[0].High)
	assert.Equal(t, 64309.0, klines[0].Low)
	assert.Equal(t, 64394.2, klines[0].Close)
	assert.Equal(t, 542186.0, klines[0].Volume)

	assert.Equal(t, int64(1783684800000), klines[1].Timestamp)
	assert.Equal(t, 64399.9, klines[1].Open)
	assert.Equal(t, 64580.0, klines[1].High)
	assert.Equal(t, 64241.4, klines[1].Low)
	assert.Equal(t, 64258.8, klines[1].Close)
	assert.Equal(t, 2138920.0, klines[1].Volume)
}

func TestClient_FetchKlines_Error(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{
			"code": 40039,
			"message": "Invalid Timestamp"
		}`))
	}))
	defer server.Close()

	client := bitmart.NewClient(server.Client(), server.URL, "key", "secret", "passphrase", config.LoggingConfig{})
	_, err := client.FetchKlines(
		context.Background(),
		"INVALID",
		exchange.Interval1h,
		time.Time{},
		time.Time{},
	)
	assert.Error(t, err)
}
