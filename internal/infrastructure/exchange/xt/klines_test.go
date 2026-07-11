package xt_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/xt"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupXTMockServer(t *testing.T, expectedTimeParam, expectedTimeVal string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/future/market/v1/public/q/kline", r.URL.Path)

		q := r.URL.Query()
		assert.Equal(t, "btc_usdt", q.Get("symbol"))
		assert.Equal(t, "1m", q.Get("interval"))
		assert.Equal(t, expectedTimeVal, q.Get(expectedTimeParam))
		assert.Equal(t, "100", q.Get("limit"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"returnCode": 0,
			"msgInfo": "success",
			"error": null,
			"result": [
				{
					"s": "btc_usdt",
					"p": "btc_usdt",
					"t": 1783681200000,
					"o": "64374",
					"c": "64394.2",
					"h": "64450",
					"l": "64309",
					"a": "31.4166",
					"v": "2022077.26553"
				}
			]
		}`))
	}))
}

func TestClient_FetchKlines(t *testing.T) {
	t.Parallel()

	server := setupXTMockServer(t, "startTime", "1783681200000")
	defer server.Close()

	client := xt.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})

	klines, err := client.FetchKlines(
		context.Background(),
		"BTC_USDT",
		exchange.Interval1m,
		time.Unix(1783681200, 0),
		time.Time{},
	)
	require.NoError(t, err)
	require.Len(t, klines, 1)

	assert.Equal(t, int64(1783681200000), klines[0].Timestamp)
	assert.Equal(t, 64374.0, klines[0].Open)
	assert.Equal(t, 64450.0, klines[0].High)
	assert.Equal(t, 64309.0, klines[0].Low)
	assert.Equal(t, 64394.2, klines[0].Close)
	assert.InDelta(t, 31.4018, klines[0].Volume, 0.01)
	assert.Equal(t, 2022077.26553, klines[0].Amount)
}

func TestClient_FetchKlines_WithEnd(t *testing.T) {
	t.Parallel()

	server := setupXTMockServer(t, "endTime", "1783681200000")
	defer server.Close()

	client := xt.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})

	klines, err := client.FetchKlines(
		context.Background(),
		"BTC_USDT",
		exchange.Interval1m,
		time.Time{},
		time.Unix(1783681200, 0),
	)
	require.NoError(t, err)
	require.Len(t, klines, 1)
}

func TestClient_FetchKlines_Error(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{
			"returnCode": 1,
			"msgInfo": "failure",
			"error": {
				"code": "invalid_symbol",
				"msg": "invalid symbol"
			}
		}`))
	}))
	defer server.Close()

	client := xt.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	_, err := client.FetchKlines(
		context.Background(),
		"INVALID",
		exchange.Interval1m,
		time.Time{},
		time.Time{},
	)
	assert.Error(t, err)
}

func TestClient_FetchKlines_SymbolFormat(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		assert.Equal(t, "btc_usdt", q.Get("symbol"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"returnCode": 0,
			"msgInfo": "success",
			"error": null,
			"result": []
		}`))
	}))
	defer server.Close()

	client := xt.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})

	_, err := client.FetchKlines(
		context.Background(),
		"BTCUSDT",
		exchange.Interval1m,
		time.Time{},
		time.Time{},
	)
	require.NoError(t, err)
}
