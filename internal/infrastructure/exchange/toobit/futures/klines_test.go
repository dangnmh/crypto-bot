package futures_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/toobit/futures"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_FetchKlines(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/quote/v1/klines", r.URL.Path)

		q := r.URL.Query()
		assert.Equal(t, "BTCUSDT", q.Get("symbol"))
		assert.Equal(t, "1h", q.Get("interval"))
		assert.Equal(t, "1783681200000", q.Get("startTime"))
		assert.Equal(t, "1000", q.Get("limit"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			[
				1783681200000,
				"64375.7",
				"64456",
				"64302.4",
				"64436.1",
				"448016",
				1783684799999,
				"28841508.980000008",
				308,
				"1756.87402397",
				"28.46694368",
				"0"
			],
			[
				1783677600000,
				"64362.2",
				"64478.7",
				"64246.5",
				"64375.7",
				"936968",
				1783681199999,
				"60303388.4182006",
				512,
				"3256.12345",
				"42.12345",
				"0"
			]
		]`))
	}))
	defer server.Close()

	client := futures.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	klines, err := client.FetchKlines(
		context.Background(),
		"BTCUSDT",
		exchange.Interval1h,
		time.UnixMilli(1783681200000),
		time.Time{},
	)
	require.NoError(t, err)
	require.Len(t, klines, 2)

	assert.Equal(t, int64(1783681200000), klines[0].Timestamp)
	assert.Equal(t, 64375.7, klines[0].Open)
	assert.Equal(t, 64456.0, klines[0].High)
	assert.Equal(t, 64302.4, klines[0].Low)
	assert.Equal(t, 64436.1, klines[0].Close)
	assert.Equal(t, 448016.0, klines[0].Volume)

	assert.Equal(t, int64(1783677600000), klines[1].Timestamp)
	assert.Equal(t, 64362.2, klines[1].Open)
	assert.Equal(t, 64478.7, klines[1].High)
	assert.Equal(t, 64246.5, klines[1].Low)
	assert.Equal(t, 64375.7, klines[1].Close)
	assert.Equal(t, 936968.0, klines[1].Volume)
}

func TestClient_FetchKlines_Error(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{
			"code": -1121,
			"msg": "Invalid symbol"
		}`))
	}))
	defer server.Close()

	client := futures.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	_, err := client.FetchKlines(
		context.Background(),
		"INVALID",
		exchange.Interval1h,
		time.Time{},
		time.Time{},
	)
	assert.Error(t, err)
}
