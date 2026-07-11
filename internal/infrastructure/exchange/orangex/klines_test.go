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

func TestClient_FetchKlines_Scenarios(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		responseBody   string
		responseCode   int
		expectedError  string
		expectedTicks  int
		expectedStart  int64
		limitTestCheck bool
	}{
		{
			name: "Success response",
			responseBody: `{
				"jsonrpc": "2.0",
				"id": 1,
				"result": [
					{
						"open": "64374",
						"close": "64394.2",
						"high": "64450",
						"low": "64309",
						"tick": 1783681200,
						"volume": "542186"
					},
					{
						"open": "64399.9",
						"close": "64258.8",
						"high": "64580",
						"low": "64241.4",
						"tick": 1783684800,
						"volume": "2138920"
					}
				]
			}`,
			responseCode:  http.StatusOK,
			expectedTicks: 2,
		},
		{
			name: "API error response",
			responseBody: `{
				"jsonrpc": "2.0",
				"error": {
					"code": 8121,
					"message": "No service found"
				}
			}`,
			responseCode:  http.StatusBadRequest,
			expectedError: "No service found",
		},
		{
			name: "Range 1500 limit check",
			responseBody: `{
				"jsonrpc": "2.0",
				"result": []
			}`,
			responseCode:   http.StatusOK,
			limitTestCheck: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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

					if tt.limitTestCheck {
						// 1h interval. 1500 candles is 1500 hours.
						// Expected start timestamp should be exactly end_timestamp - 1500 * time.Hour
						expectedStart := reqBody.Params.EndTimestamp - (1500 * time.Hour).Milliseconds()
						assert.Equal(t, expectedStart, reqBody.Params.StartTimestamp)
					}

					w.WriteHeader(tt.responseCode)
					_, _ = w.Write([]byte(tt.responseBody))
					return
				}
			}))
			defer server.Close()

			client := orangex.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})

			var startTime, endTime time.Time
			if tt.limitTestCheck {
				endTime = time.Now()
				startTime = endTime.Add(-2000 * time.Hour)
			} else {
				startTime = time.Unix(1783681200, 0)
			}

			klines, err := client.FetchKlines(
				context.Background(),
				"BTC-USDT-PERPETUAL",
				exchange.Interval1h,
				startTime,
				endTime,
			)

			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				require.NoError(t, err)
				if !tt.limitTestCheck {
					require.Len(t, klines, tt.expectedTicks)
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
			}
		})
	}
}
