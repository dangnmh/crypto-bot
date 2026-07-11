package orangex_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/orangex"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_FetchKlines(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/public/auth" {
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":{"access_token":"mock_tok","expires_in":3600}}`))
			return
		}
		if r.URL.Path == "/private/get_tradingview_chart_data" {
			assert.Equal(t, "POST", r.Method)

			var reqBody struct {
				Method string `json:"method"`
				Params struct {
					InstrumentName string `json:"instrument_name"`
					Resolution     string `json:"resolution"`
					StartTimestamp int64  `json:"start_timestamp"`
					EndTimestamp   int64  `json:"end_timestamp"`
				} `json:"params"`
			}
			err := json.NewDecoder(r.Body).Decode(&reqBody)
			require.NoError(t, err)

			assert.Equal(t, "/private/get_tradingview_chart_data", reqBody.Method)
			assert.Equal(t, "BTC-USDT-PERPETUAL", reqBody.Params.InstrumentName)
			assert.Equal(t, "60", reqBody.Params.Resolution)
			assert.Equal(t, int64(1783681200000), reqBody.Params.StartTimestamp)

			_, _ = w.Write([]byte(`{
				"jsonrpc": "2.0",
				"id": 1,
				"result": {
					"status": "ok",
					"ticks": [1783681200000, 1783684800000],
					"open": ["64374", "64399.9"],
					"high": ["64450", "64580"],
					"low": ["64309", "64241.4"],
					"close": ["64394.2", "64258.8"],
					"volume": ["542186", "2138920"]
				}
			}`))
			return
		}
	}))
	defer server.Close()

	client := orangex.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})

	klines, err := client.FetchKlines(
		context.Background(),
		"BTC-USDT-PERPETUAL",
		exchange.Interval1h,
		time.Unix(1783681200, 0),
		time.Time{},
	)
	require.NoError(t, err)
	require.Len(t, klines, 2)

	assert.Equal(t, int64(1783681200000), klines[0].Timestamp)
	assert.Equal(t, 64374.0, klines[0].Open)
	assert.Equal(t, 64450.0, klines[0].High)
	assert.Equal(t, 64309.0, klines[0].Low)
	assert.Equal(t, 64394.2, klines[0].Close)
	assert.Equal(t, 542186.0, klines[0].Volume)

	assert.Equal(t, int64(1783684800000), klines[1].Timestamp)
	assert.Equal(t, 64399.9, klines[1].Open)
	assert.Equal(t, 64580.0, klines[1].High)
	assert.Equal(t, 64241.4, klines[1].Low)
	assert.Equal(t, 64258.8, klines[1].Close)
	assert.Equal(t, 2138920.0, klines[1].Volume)
}

func TestClient_FetchKlines_Error(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/public/auth" {
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":{"access_token":"mock_tok","expires_in":3600}}`))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{
			"jsonrpc": "2.0",
			"error": {
				"code": 8121,
				"message": "No service found"
			}
		}`))
	}))
	defer server.Close()

	client := orangex.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	_, err := client.FetchKlines(
		context.Background(),
		"INVALID",
		exchange.Interval1h,
		time.Time{},
		time.Time{},
	)
	assert.Error(t, err)
}

func TestClient_FetchKlines_Limit1500(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/public/auth" {
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":{"access_token":"mock_tok","expires_in":3600}}`))
			return
		}
		if r.URL.Path == "/private/get_tradingview_chart_data" {
			var reqBody struct {
				Method string `json:"method"`
				Params struct {
					StartTimestamp int64 `json:"start_timestamp"`
					EndTimestamp   int64 `json:"end_timestamp"`
				} `json:"params"`
			}
			err := json.NewDecoder(r.Body).Decode(&reqBody)
			require.NoError(t, err)

			// 1h interval. 1500 candles is 1500 hours.
			// Expected start timestamp should be exactly end_timestamp - 1500 * time.Hour
			expectedStart := reqBody.Params.EndTimestamp - (1500 * time.Hour).Milliseconds()
			assert.Equal(t, expectedStart, reqBody.Params.StartTimestamp)

			_, _ = w.Write([]byte(`{
				"jsonrpc": "2.0",
				"id": 1,
				"result": {
					"status": "ok",
					"ticks": [],
					"open": [],
					"high": [],
					"low": [],
					"close": [],
					"volume": []
				}
			}`))
			return
		}
	}))
	defer server.Close()

	client := orangex.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})

	// Requesting 2000 hours range (from 2000 hours ago to now)
	endTime := time.Now()
	startTime := endTime.Add(-2000 * time.Hour)

	_, err := client.FetchKlines(
		context.Background(),
		"BTC-USDT-PERPETUAL",
		exchange.Interval1h,
		startTime,
		endTime,
	)
	require.NoError(t, err)
}
