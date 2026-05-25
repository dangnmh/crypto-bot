package gate_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/gate"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_CreateOrder_Mapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		req       exchange.SubmitOrderRequest
		wantSize  int64
		wantPrice string
		wantTif   string
		wantClose bool
		wantAuto  string
	}{
		{
			name: "Open Long Limit (Hedge)",
			req: exchange.SubmitOrderRequest{
				Symbol:       "BTC_USDT",
				Vol:          2,
				Side:         exchange.SideOpenLong,
				Type:         exchange.OrderTypeLimit,
				Price:        50000.0,
				PositionMode: 1, // Hedge
			},
			wantSize:  2,
			wantPrice: "50000",
			wantTif:   "gtc",
		},
		{
			name: "Open Short Market (Hedge)",
			req: exchange.SubmitOrderRequest{
				Symbol:       "BTC_USDT",
				Vol:          2,
				Side:         exchange.SideOpenShort,
				Type:         exchange.OrderTypeMarket,
				PositionMode: 1, // Hedge
			},
			wantSize:  -2,
			wantPrice: "0",
			wantTif:   "ioc",
		},
		{
			name: "Close Long (Hedge)",
			req: exchange.SubmitOrderRequest{
				Symbol:       "BTC_USDT",
				Vol:          2,
				Side:         exchange.SideCloseLong,
				Type:         exchange.OrderTypeMarket,
				PositionMode: 1, // Hedge
			},
			wantSize:  0,
			wantPrice: "0",
			wantTif:   "ioc",
			wantAuto:  "close_long",
		},
		{
			name: "Close Short (Hedge)",
			req: exchange.SubmitOrderRequest{
				Symbol:       "BTC_USDT",
				Vol:          2,
				Side:         exchange.SideCloseShort,
				Type:         exchange.OrderTypeMarket,
				PositionMode: 1, // Hedge
			},
			wantSize:  0,
			wantPrice: "0",
			wantTif:   "ioc",
			wantAuto:  "close_short",
		},
		{
			name: "Open Long Market (OneWay)",
			req: exchange.SubmitOrderRequest{
				Symbol:       "BTC_USDT",
				Vol:          3,
				Side:         exchange.SideOpenLong,
				Type:         exchange.OrderTypeMarket,
				PositionMode: 2, // OneWay
				ReduceOnly:   true,
			},
			wantSize:  3,
			wantPrice: "0",
			wantTif:   "ioc",
			wantClose: true,
		},
		{
			name: "Open Short Market (OneWay)",
			req: exchange.SubmitOrderRequest{
				Symbol:       "BTC_USDT",
				Vol:          3,
				Side:         exchange.SideOpenShort,
				Type:         exchange.OrderTypeMarket,
				PositionMode: 2, // OneWay
			},
			wantSize:  -3,
			wantPrice: "0",
			wantTif:   "ioc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Spin up a mock HTTP server to intercept the POST request to Gate.io API.
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "POST", r.Method)
				assert.Contains(t, r.URL.Path, "/futures/usdt/orders")

				// Decode the request body (which is gateapi.FuturesOrder).
				var body map[string]any
				err := json.NewDecoder(r.Body).Decode(&body)
				require.NoError(t, err)

				// Verify fields inside the serialized request.
				assert.Equal(t, tt.req.Symbol, body["contract"])

				// Assert price and size.
				assert.Equal(t, tt.wantPrice, body["price"])

				sizeVal, exists := body["size"]
				if tt.wantSize != 0 {
					require.True(t, exists)
					val, ok := sizeVal.(float64)
					require.True(t, ok, "size must be a float64")
					assert.Equal(t, float64(tt.wantSize), val)
				} else if exists && sizeVal != nil {
					val, ok := sizeVal.(float64)
					require.True(t, ok, "size must be a float64")
					assert.Equal(t, float64(0), val)
				}

				if tt.wantTif != "" {
					assert.Equal(t, tt.wantTif, body["tif"])
				}

				if tt.wantClose {
					assert.Equal(t, true, body["close"])
				}

				if tt.wantAuto != "" {
					assert.Equal(t, tt.wantAuto, body["auto_size"])
				}

				// Return a successful order response with ID 987654.
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id": 987654}`))
			}))
			defer server.Close()

			// Create client pointed to our mock server.
			client := gate.NewClient(server.Client(), server.URL, "api_key", "api_secret", config.LoggingConfig{})

			orderID, err := client.CreateOrder(context.Background(), tt.req)
			require.NoError(t, err)
			assert.Equal(t, "987654", orderID)
		})
	}
}
