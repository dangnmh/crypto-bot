package futures_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/kucoin/futures"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_FetchKlines(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v1/kline/query", r.URL.Path)

		q := r.URL.Query()
		assert.Equal(t, "XBTUSDM", q.Get("symbol"))
		assert.Equal(t, "60", q.Get("granularity"))
		assert.Equal(t, "1783447200000", q.Get("from"))
		assert.Equal(t, "1783458000000", q.Get("to"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "200000",
			"msg": "success",
			"data": [
				[
					1783447200000,
					64085.7,
					64089.0,
					63618.5,
					63618.5,
					7282,
					7282.0
				],
				[
					1783450800000,
					63600.0,
					63790.4,
					63302.2,
					63756.5,
					87269,
					87269.0
				]
			]
		}`))
	}))
	defer server.Close()

	client := futures.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	klines, err := client.FetchKlines(
		context.Background(),
		"XBTUSDM",
		exchange.Interval1h,
		time.UnixMilli(1783447200000),
		time.UnixMilli(1783458000000),
	)
	require.NoError(t, err)
	require.Len(t, klines, 2)

	assert.Equal(t, int64(1783447200000), klines[0].Timestamp)
	assert.Equal(t, 64085.7, klines[0].Open)
	assert.Equal(t, 64089.0, klines[0].High)
	assert.Equal(t, 63618.5, klines[0].Low)
	assert.Equal(t, 63618.5, klines[0].Close)
	assert.Equal(t, 7282.0, klines[0].Volume)

	assert.Equal(t, int64(1783450800000), klines[1].Timestamp)
	assert.Equal(t, 63600.0, klines[1].Open)
	assert.Equal(t, 63790.4, klines[1].High)
	assert.Equal(t, 63302.2, klines[1].Low)
	assert.Equal(t, 63756.5, klines[1].Close)
	assert.Equal(t, 87269.0, klines[1].Volume)
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

	client := futures.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	_, err := client.FetchKlines(
		context.Background(),
		"INVALID",
		exchange.Interval1h,
		time.Time{},
		time.Time{},
	)
	assert.Error(t, err)
}
