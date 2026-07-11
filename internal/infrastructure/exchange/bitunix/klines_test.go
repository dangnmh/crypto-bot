package bitunix_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/bitunix"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_FetchKlines_Scenarios(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		responseBody  string
		responseCode  int
		expectedError string
		expectedTime  int64
	}{
		{
			name: "Success code 200",
			responseBody: `{
				"code": 200,
				"data": [
					{
						"open": "64374",
						"high": "64450",
						"low": "64309",
						"close": "64394.2",
						"quoteVol": "542186",
						"baseVol": "2138920",
						"time": 1783681200000
					}
				],
				"msg": "Success"
			}`,
			responseCode: http.StatusOK,
			expectedTime: 1783681200000,
		},
		{
			name: "Success code 0",
			responseBody: `{
				"code": 0,
				"data": [
					{
						"open": "64374",
						"high": "64450",
						"low": "64309",
						"close": "64394.2",
						"quoteVol": "542186",
						"baseVol": "2138920",
						"time": 1783681200000
					}
				],
				"msg": "Success"
			}`,
			responseCode: http.StatusOK,
			expectedTime: 1783681200000,
		},
		{
			name: "TimeString time format",
			responseBody: `{
				"code": 200,
				"data": [
					{
						"open": "64374",
						"high": "64450",
						"low": "64309",
						"close": "64394.2",
						"quoteVol": "542186",
						"baseVol": "2138920",
						"time": "1783681200000"
					}
				],
				"msg": "Success"
			}`,
			responseCode: http.StatusOK,
			expectedTime: 1783681200000,
		},
		{
			name: "HTTP Bad Request 400",
			responseBody: `{
				"code": 800021,
				"msg": "System error"
			}`,
			responseCode:  http.StatusBadRequest,
			expectedError: "HTTP 400",
		},
		{
			name: "API Error code 800021 in JSON",
			responseBody: `{
				"code": 800021,
				"msg": "System error"
			}`,
			responseCode:  http.StatusOK,
			expectedError: "bitunix API error: code=800021 msg=System error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "GET", r.Method)
				assert.Equal(t, "/api/v1/futures/market/kline", r.URL.Path)

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.responseCode)
				_, _ = w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			client := bitunix.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})

			klines, err := client.FetchKlines(
				context.Background(),
				"BTC_USDT",
				exchange.Interval1m,
				time.Time{},
				time.Time{},
			)

			if tt.expectedError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				require.NoError(t, err)
				require.Len(t, klines, 1)
				assert.Equal(t, tt.expectedTime, klines[0].Timestamp)
				assert.Equal(t, 64374.0, klines[0].Open)
				assert.Equal(t, 64450.0, klines[0].High)
				assert.Equal(t, 64309.0, klines[0].Low)
				assert.Equal(t, 64394.2, klines[0].Close)
				assert.Equal(t, 542186.0, klines[0].Volume)
				assert.Equal(t, 2138920.0, klines[0].Amount)
			}
		})
	}
}
