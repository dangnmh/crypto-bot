package whitebit_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/whitebit"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_FetchKlines(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v1/public/kline", r.URL.Path)

		q := r.URL.Query()
		assert.Equal(t, "BTC_USDT", q.Get("market"))
		assert.Equal(t, "1m", q.Get("interval"))
		assert.Equal(t, "1783504800", q.Get("start"))
		assert.Equal(t, "1783508400", q.Get("end"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"message": null,
			"result": [
				[
					1783504800,
					"62059.54",
					"62186.33",
					"62196.45",
					"61894.24",
					"27.717789",
					"1719616.32"
				]
			]
		}`))
	}))
	defer server.Close()

	client := whitebit.NewClient(server.Client(), server.URL, config.LoggingConfig{})

	klines, err := client.FetchKlines(
		context.Background(),
		"BTC_USDT",
		exchange.Interval1m,
		time.Unix(1783504800, 0),
		time.Unix(1783508400, 0),
	)
	require.NoError(t, err)
	require.Len(t, klines, 1)

	assert.Equal(t, int64(1783504800000), klines[0].Timestamp)
	assert.Equal(t, 62059.54, klines[0].Open)
	assert.Equal(t, 62196.45, klines[0].High)
	assert.Equal(t, 61894.24, klines[0].Low)
	assert.Equal(t, 62186.33, klines[0].Close)
	assert.Equal(t, 27.717789, klines[0].Volume)
	assert.Equal(t, 1719616.32, klines[0].Amount)
}

func TestClient_FetchKlines_Error(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{
			"success": false,
			"message": "invalid params"
		}`))
	}))
	defer server.Close()

	client := whitebit.NewClient(server.Client(), server.URL, config.LoggingConfig{})
	_, err := client.FetchKlines(
		context.Background(),
		"INVALID",
		exchange.Interval1m,
		time.Time{},
		time.Time{},
	)
	assert.Error(t, err)
}
