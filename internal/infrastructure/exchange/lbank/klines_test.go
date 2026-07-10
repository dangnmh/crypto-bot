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

func TestClient_FetchKlines_AdditionalCases(t *testing.T) {
	t.Parallel()

	// Case 1: Unsupported interval
	client := lbank.NewClient(nil, "http://localhost", config.LoggingConfig{})
	_, err := client.FetchKlines(
		context.Background(),
		"BTC_USDT",
		exchange.Interval3m, // unsupported
		time.Time{},
		time.Time{},
	)
	assert.Error(t, err)

	// Case 2: String parsing in response fields, size clamping
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		assert.Equal(t, "2000", q.Get("size"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"msg": "Success",
			"result": "true",
			"error_code": 0,
			"data": [
				[
					"1783504800",
					"62982.3",
					"63230.9",
					"62782.7",
					"62835.3",
					"403039.0"
				],
				[
					1783504800,
					62982.3,
					63230.9,
					62782.7,
					62835.3,
					"invalid"
				],
				[
					1783504800
				]
			]
		}`))
	}))
	defer server.Close()

	client = lbank.NewClient(server.Client(), server.URL, config.LoggingConfig{})
	klines, err := client.FetchKlines(
		context.Background(),
		"BTC_USDT",
		exchange.Interval1m,
		time.Unix(1783504800, 0),
		time.Unix(1783504800+3000*60, 0), // 3000 minutes -> clamped to 2000
	)
	require.NoError(t, err)
	// Second row vol parsing fails (ignored), third row too short, 2 rows returned
	assert.Len(t, klines, 2)
	assert.Equal(t, 62982.3, klines[0].Open)
	assert.Equal(t, 0.0, klines[1].Volume)
}

func TestClient_FetchKlines_Intervals(t *testing.T) {
	t.Parallel()

	intervals := []exchange.Interval{
		exchange.Interval5m,
		exchange.Interval15m,
		exchange.Interval30m,
		exchange.Interval1h,
		exchange.Interval4h,
		exchange.Interval8h,
		exchange.Interval12h,
		exchange.Interval1d,
		exchange.Interval1w,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"msg": "Success",
			"result": "true",
			"error_code": 0,
			"data": []
		}`))
	}))
	defer server.Close()

	client := lbank.NewClient(server.Client(), server.URL, config.LoggingConfig{})

	for _, interval := range intervals {
		_, err := client.FetchKlines(
			context.Background(),
			"btc-usdt",
			interval,
			time.Now().Add(-10*time.Hour),
			time.Now(),
		)
		assert.NoError(t, err)
	}
}
