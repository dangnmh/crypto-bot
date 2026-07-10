package bitget_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/bitget"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_FetchKlines(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v2/mix/market/candles", r.URL.Path)

		q := r.URL.Query()
		assert.Equal(t, "BTCUSDT", q.Get("symbol"))
		assert.Equal(t, "USDT-FUTURES", q.Get("productType"))
		assert.Equal(t, "1H", q.Get("granularity"))
		assert.Equal(t, "1783674000000", q.Get("startTime"))
		assert.Equal(t, "1783681200000", q.Get("endTime"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "00000",
			"msg": "success",
			"data": [
				[
					"1783674000000",
					"64199.4",
					"64438",
					"64060.6",
					"64340.5",
					"1804.7384",
					"116077022.09106"
				],
				[
					"1783677600000",
					"64340.5",
					"64455.5",
					"64220.1",
					"64359.8",
					"1122.0411",
					"72199775.3255"
				]
			]
		}`))
	}))
	defer server.Close()

	client := bitget.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	klines, err := client.FetchKlines(
		context.Background(),
		"BTCUSDT",
		exchange.Interval1h,
		time.UnixMilli(1783674000000),
		time.UnixMilli(1783681200000),
	)
	require.NoError(t, err)
	require.Len(t, klines, 2)

	assert.Equal(t, int64(1783674000000), klines[0].Timestamp)
	assert.Equal(t, 64199.4, klines[0].Open)
	assert.Equal(t, 64438.0, klines[0].High)
	assert.Equal(t, 64060.6, klines[0].Low)
	assert.Equal(t, 64340.5, klines[0].Close)
	assert.Equal(t, 1804.7384, klines[0].Volume)

	assert.Equal(t, int64(1783677600000), klines[1].Timestamp)
	assert.Equal(t, 64340.5, klines[1].Open)
	assert.Equal(t, 64455.5, klines[1].High)
	assert.Equal(t, 64220.1, klines[1].Low)
	assert.Equal(t, 64359.8, klines[1].Close)
	assert.Equal(t, 1122.0411, klines[1].Volume)
}

func TestClient_FetchKlines_Error(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "400001",
			"msg": "invalid symbol"
		}`))
	}))
	defer server.Close()

	client := bitget.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	_, err := client.FetchKlines(
		context.Background(),
		"INVALID",
		exchange.Interval1h,
		time.Time{},
		time.Time{},
	)
	assert.Error(t, err)
}
