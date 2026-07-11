package fameex_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/fameex"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_FetchKlines(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/sapi/v1/klines", r.URL.Path)

		q := r.URL.Query()
		assert.Equal(t, "BTCUSDT", q.Get("symbol"))
		assert.Equal(t, "1min", q.Get("interval"))
		assert.Equal(t, "1783504800000", q.Get("startTime"))
		assert.Equal(t, "1783508400000", q.Get("endTime"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 0,
			"msg": "Success",
			"succ": true,
			"data": [
				{
					"high": 63230.9,
					"vol": 403039.0,
					"low": 62782.7,
					"idx": 1783504800000,
					"close": 62835.3,
					"open": 62982.3
				}
			]
		}`))
	}))
	defer server.Close()

	client := fameex.NewClient(server.Client(), server.URL, config.LoggingConfig{})

	klines, err := client.FetchKlines(
		context.Background(),
		"BTCUSDT",
		exchange.Interval1m,
		time.Unix(1783504800, 0),
		time.Unix(1783508400, 0),
	)
	require.NoError(t, err)
	require.Len(t, klines, 1)

	assert.Equal(t, int64(1783504800000), klines[0].Timestamp)
	assert.Equal(t, 62982.3, klines[0].Open)
	assert.Equal(t, 63230.9, klines[0].High)
	assert.Equal(t, 62782.7, klines[0].Low)
	assert.Equal(t, 62835.3, klines[0].Close)
	assert.Equal(t, 403039.0, klines[0].Volume)
	assert.Equal(t, 403039.0*62835.3, klines[0].Amount)
}

func TestClient_FetchKlines_Error(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{
			"code": 2,
			"msg": "Invalid parameter",
			"succ": false
		}`))
	}))
	defer server.Close()

	client := fameex.NewClient(server.Client(), server.URL, config.LoggingConfig{})
	_, err := client.FetchKlines(
		context.Background(),
		"INVALID",
		exchange.Interval1m,
		time.Time{},
		time.Time{},
	)
	assert.Error(t, err)
}
