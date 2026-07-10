package zoomex_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/zoomex"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_FetchKlines(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/cloud/trade/v3/market/kline", r.URL.Path)

		q := r.URL.Query()
		assert.Equal(t, "BTCUSDT", q.Get("symbol"))
		assert.Equal(t, "linear", q.Get("category"))
		assert.Equal(t, "60", q.Get("interval"))
		assert.Equal(t, "1783674000000", q.Get("start"))
		assert.Equal(t, "1783681200000", q.Get("end"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"retCode": 0,
			"retMsg": "OK",
			"result": {
				"symbol": "BTCUSDT",
				"category": "linear",
				"list": [
					[
						"1783681200000",
						"64373.4",
						"64448.5",
						"64328.4",
						"64365",
						"91.882",
						"5916361.7961"
					],
					[
						"1783677600000",
						"64357.9",
						"64470.6",
						"64241.6",
						"64373.4",
						"193.689",
						"12465918.782"
					]
				]
			},
			"retExtInfo": {},
			"time": 1783683127965
		}`))
	}))
	defer server.Close()

	client := zoomex.NewClient(server.Client(), server.URL, config.LoggingConfig{})
	klines, err := client.FetchKlines(
		context.Background(),
		"BTCUSDT",
		exchange.Interval1h,
		time.UnixMilli(1783674000000),
		time.UnixMilli(1783681200000),
	)
	require.NoError(t, err)
	require.Len(t, klines, 2)

	assert.Equal(t, int64(1783681200000), klines[0].Timestamp)
	assert.Equal(t, 64373.4, klines[0].Open)
	assert.Equal(t, 64448.5, klines[0].High)
	assert.Equal(t, 64328.4, klines[0].Low)
	assert.Equal(t, 64365.0, klines[0].Close)
	assert.Equal(t, 91.882, klines[0].Volume)

	assert.Equal(t, int64(1783677600000), klines[1].Timestamp)
	assert.Equal(t, 64357.9, klines[1].Open)
	assert.Equal(t, 64470.6, klines[1].High)
	assert.Equal(t, 64241.6, klines[1].Low)
	assert.Equal(t, 64373.4, klines[1].Close)
	assert.Equal(t, 193.689, klines[1].Volume)
}

func TestClient_FetchKlines_Error(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"retCode": 10001,
			"retMsg": "invalid symbol"
		}`))
	}))
	defer server.Close()

	client := zoomex.NewClient(server.Client(), server.URL, config.LoggingConfig{})
	_, err := client.FetchKlines(
		context.Background(),
		"INVALID",
		exchange.Interval1h,
		time.Time{},
		time.Time{},
	)
	assert.Error(t, err)
}
