package fameex_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/fameex"

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
	}{
		{
			name: "Success response",
			responseBody: `{
				"code": 0,
				"msg": "Success",
				"succ": true,
				"data": {
					"klines": [
						{
							"high": 63230.9,
							"vol": 403039.0,
							"low": 62782.7,
							"idx": 1783504800000,
							"close": 62835.3,
							"open": 62982.3
						}
					]
				}
			}`,
			responseCode: http.StatusOK,
		},
		{
			name: "API error response",
			responseBody: `{
				"code": 2,
				"msg": "Invalid parameter",
				"succ": false
			}`,
			responseCode:  http.StatusOK,
			expectedError: "fameex API error: code=2 msg=Invalid parameter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "GET", r.Method)
				assert.Equal(t, "/sapi/v1/klines", r.URL.Path)

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.responseCode)
				_, _ = w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			client := fameex.NewClient(server.Client(), server.URL, config.LoggingConfig{})

			klines, err := client.FetchKlines(
				context.Background(),
				"BTCUSDT",
				exchange.Interval1m,
				time.Unix(1783504800, 0),
				time.Unix(1783508400, 0),
			)

			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
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
		})
	}
}

func TestClient_FetchKlines_Additional(t *testing.T) {
	t.Parallel()

	// Unsupported interval check
	client := fameex.NewClient(nil, "http://localhost", config.LoggingConfig{})
	_, err := client.FetchKlines(
		context.Background(),
		"BTCUSDT",
		exchange.Interval3m, // unsupported
		time.Time{},
		time.Time{},
	)
	assert.Error(t, err)
}
