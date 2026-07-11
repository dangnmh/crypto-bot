package pionex_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/pionex"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_FetchKlines(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v1/market/klines", r.URL.Path)

		q := r.URL.Query()
		assert.Equal(t, "BTC_USDT_PERP", q.Get("symbol"))
		assert.Equal(t, "1M", q.Get("interval"))
		assert.Equal(t, "1783681200000", q.Get("endTime"))
		assert.Equal(t, "100", q.Get("limit"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"result": true,
			"data": {
				"klines": [
					{
						"time": 1783681200000,
						"open": "64374",
						"close": "64394.2",
						"high": "64450",
						"low": "64309",
						"volume": "31.4166"
					}
				]
			},
			"timestamp": 1783690401059
		}`))
	}))
	defer server.Close()

	client := pionex.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})

	klines, err := client.FetchKlines(
		context.Background(),
		"BTC_USDT",
		exchange.Interval1m,
		time.Time{},
		time.Unix(1783681200, 0),
	)
	require.NoError(t, err)
	require.Len(t, klines, 1)

	assert.Equal(t, int64(1783681200000), klines[0].Timestamp)
	assert.Equal(t, 64374.0, klines[0].Open)
	assert.Equal(t, 64450.0, klines[0].High)
	assert.Equal(t, 64309.0, klines[0].Low)
	assert.Equal(t, 64394.2, klines[0].Close)
	assert.Equal(t, 31.4166, klines[0].Volume)
	assert.InDelta(t, 2023046.82, klines[0].Amount, 0.01)
}

func TestClient_FetchKlines_Error(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{
			"result": false,
			"code": "MARKET_PARAMETER_ERROR",
			"message": "interval param error"
		}`))
	}))
	defer server.Close()

	client := pionex.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	_, err := client.FetchKlines(
		context.Background(),
		"INVALID",
		exchange.Interval1m,
		time.Time{},
		time.Time{},
	)
	assert.Error(t, err)
}
