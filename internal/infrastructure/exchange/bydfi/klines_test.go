package bydfi_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/bydfi"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_FetchKlines(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v1/fapi/market/klines", r.URL.Path)

		q := r.URL.Query()
		assert.Equal(t, "BTC-USDT", q.Get("symbol"))
		assert.Equal(t, "1m", q.Get("interval"))
		assert.Equal(t, "1783504800000", q.Get("startTime"))
		assert.Equal(t, "1783508400000", q.Get("endTime"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 200,
			"message": "success",
			"data": [
				{
					"s": "BTC-USDT",
					"t": "1783504800000",
					"c": "62835.3",
					"o": "62982.3",
					"h": "63230.9",
					"l": "62782.7",
					"v": "403039"
				}
			]
		}`))
	}))
	defer server.Close()

	client := bydfi.NewClient(server.Client(), server.URL+"/api", slog.Default())

	klines, err := client.FetchKlines(
		context.Background(),
		"BTC-USDT",
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
			"code": 600,
			"message": "invalid params"
		}`))
	}))
	defer server.Close()

	client := bydfi.NewClient(server.Client(), server.URL+"/api", slog.Default())
	_, err := client.FetchKlines(
		context.Background(),
		"INVALID",
		exchange.Interval1m,
		time.Time{},
		time.Time{},
	)
	assert.Error(t, err)
}
