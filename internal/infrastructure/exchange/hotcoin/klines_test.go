package hotcoin_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/hotcoin"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_FetchKlines(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v1/perpetual/public/BTCUSDT/candles", r.URL.Path)

		q := r.URL.Query()
		assert.Equal(t, "1min", q.Get("kline"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 200,
			"msg": "success",
			"data": [
				[
					1783687800000,
					"64293.7",
					"64325.8",
					"64305.2",
					"64323.1",
					"9027",
					"580895.15"
				]
			]
		}`))
	}))
	defer server.Close()

	client := hotcoin.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})

	klines, err := client.FetchKlines(
		context.Background(),
		"BTC_USDT",
		exchange.Interval1m,
		time.Time{},
		time.Time{},
	)
	require.NoError(t, err)
	require.Len(t, klines, 1)

	assert.Equal(t, int64(1783687800000), klines[0].Timestamp)
	assert.Equal(t, 64305.2, klines[0].Open)
	assert.Equal(t, 64325.8, klines[0].High)
	assert.Equal(t, 64293.7, klines[0].Low)
	assert.Equal(t, 64323.1, klines[0].Close)
	assert.Equal(t, 9027.0, klines[0].Volume)
	assert.Equal(t, 580895.15, klines[0].Amount)
}

func TestClient_FetchKlines_Error(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{
			"code": 400,
			"msg": "invalid params"
		}`))
	}))
	defer server.Close()

	client := hotcoin.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	_, err := client.FetchKlines(
		context.Background(),
		"INVALID",
		exchange.Interval1m,
		time.Time{},
		time.Time{},
	)
	assert.Error(t, err)
}
