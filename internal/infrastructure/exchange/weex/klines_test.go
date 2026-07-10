package weex_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/weex"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_FetchKlines(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/capi/v3/market/historyKlines", r.URL.Path)

		q := r.URL.Query()
		assert.Equal(t, "BTCUSDT", q.Get("symbol"))
		assert.Equal(t, "1m", q.Get("interval"))
		assert.Equal(t, "1783680060000", q.Get("startTime"))
		assert.Equal(t, "1783681200000", q.Get("endTime"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			[
				1783681140000,
				"64387.6",
				"64387.6",
				"64373.0",
				"64374.1",
				"15.9978",
				1783681200000,
				"1029867.62"
			]
		]`))
	}))
	defer server.Close()

	client := weex.NewClient(server.Client(), server.URL, "key", "secret", "passphrase", config.LoggingConfig{})

	klines, err := client.FetchKlines(
		context.Background(),
		"BTC_USDT",
		exchange.Interval1m,
		time.Unix(1783680060, 0),
		time.Unix(1783681200, 0),
	)
	require.NoError(t, err)
	require.Len(t, klines, 1)

	assert.Equal(t, int64(1783681140000), klines[0].Timestamp)
	assert.Equal(t, 64387.6, klines[0].Open)
	assert.Equal(t, 64387.6, klines[0].High)
	assert.Equal(t, 64373.0, klines[0].Low)
	assert.Equal(t, 64374.1, klines[0].Close)
	assert.Equal(t, 15.9978, klines[0].Volume)
	assert.Equal(t, 1029867.62, klines[0].Amount)
}

func TestClient_FetchKlines_Error(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{
			"code": -1142,
			"msg": "Parameter 'interval' is invalid."
		}`))
	}))
	defer server.Close()

	client := weex.NewClient(server.Client(), server.URL, "key", "secret", "passphrase", config.LoggingConfig{})
	_, err := client.FetchKlines(
		context.Background(),
		"INVALID",
		exchange.Interval1m,
		time.Time{},
		time.Time{},
	)
	assert.Error(t, err)
}
