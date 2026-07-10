package lbank_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/lbank"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_FetchKlines(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/v2/kline.do", r.URL.Path)

		q := r.URL.Query()
		assert.Equal(t, "btc_usdt", q.Get("symbol"))
		assert.Equal(t, "minute1", q.Get("type"))
		assert.Equal(t, "1783504800", q.Get("time"))
		assert.Equal(t, "60", q.Get("size"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"msg": "Success",
			"result": "true",
			"error_code": 0,
			"data": [
				[
					1783504800,
					62982.3,
					63230.9,
					62782.7,
					62835.3,
					403039.0
				]
			]
		}`))
	}))
	defer server.Close()

	client := lbank.NewClient(server.Client(), server.URL, config.LoggingConfig{})

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
			"result": "false",
			"msg": "Illegal parameter",
			"error_code": 10003
		}`))
	}))
	defer server.Close()

	client := lbank.NewClient(server.Client(), server.URL, config.LoggingConfig{})
	_, err := client.FetchKlines(
		context.Background(),
		"INVALID",
		exchange.Interval1m,
		time.Time{},
		time.Time{},
	)
	assert.Error(t, err)
}
