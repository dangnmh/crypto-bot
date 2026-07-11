package blofin_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/blofin"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_FetchKlines(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v1/market/candles", r.URL.Path)

		q := r.URL.Query()
		assert.Equal(t, "BTC-USDT", q.Get("instId"))
		assert.Equal(t, "1m", q.Get("bar"))
		assert.Equal(t, "1783681200000", q.Get("after"))
		assert.Equal(t, "100", q.Get("limit"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "0",
			"msg": "success",
			"data": [
				[
					"1783665240000",
					"63889.5",
					"63889.5",
					"63875.1",
					"63888.7",
					"668",
					"0.6689",
					"42735.07"
				]
			]
		}`))
	}))
	defer server.Close()

	client := blofin.NewClient(server.Client(), server.URL, slog.Default())

	klines, err := client.FetchKlines(
		context.Background(),
		"BTC-USDT",
		exchange.Interval1m,
		time.Time{},
		time.Unix(1783681200, 0),
	)
	require.NoError(t, err)
	require.Len(t, klines, 1)

	assert.Equal(t, int64(1783665240000), klines[0].Timestamp)
	assert.Equal(t, 63889.5, klines[0].Open)
	assert.Equal(t, 63889.5, klines[0].High)
	assert.Equal(t, 63875.1, klines[0].Low)
	assert.Equal(t, 63888.7, klines[0].Close)
	assert.Equal(t, 0.6689, klines[0].Volume)
	assert.Equal(t, 42735.07, klines[0].Amount)
}

func TestClient_FetchKlines_WithStart(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v1/market/candles", r.URL.Path)

		q := r.URL.Query()
		assert.Equal(t, "BTC-USDT", q.Get("instId"))
		assert.Equal(t, "1m", q.Get("bar"))
		assert.Equal(t, "1783665240000", q.Get("before"))
		assert.Equal(t, "1783681200000", q.Get("after"))
		assert.Equal(t, "100", q.Get("limit"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "0",
			"msg": "success",
			"data": [
				[
					"1783665240000",
					"63889.5",
					"63889.5",
					"63875.1",
					"63888.7",
					"668",
					"0.6689",
					"42735.07"
				]
			]
		}`))
	}))
	defer server.Close()

	client := blofin.NewClient(server.Client(), server.URL, slog.Default())

	klines, err := client.FetchKlines(
		context.Background(),
		"BTC-USDT",
		exchange.Interval1m,
		time.Unix(1783665240, 0),
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
			"code": "1",
			"msg": "invalid params"
		}`))
	}))
	defer server.Close()

	client := blofin.NewClient(server.Client(), server.URL, slog.Default())
	_, err := client.FetchKlines(
		context.Background(),
		"INVALID",
		exchange.Interval1m,
		time.Time{},
		time.Time{},
	)
	assert.Error(t, err)
}

func TestClient_FetchKlines_InstIdError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "152002",
			"msg": "Parameter instId error."
		}`))
	}))
	defer server.Close()

	client := blofin.NewClient(server.Client(), server.URL, slog.Default())
	klines, err := client.FetchKlines(
		context.Background(),
		"LABUSDT",
		exchange.Interval1m,
		time.Time{},
		time.Time{},
	)
	assert.NoError(t, err)
	assert.Nil(t, klines)
}
