package mexc_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/mexc"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_Post(t *testing.T) {
	t.Parallel()
	resp := exchange.CreateOrderResponse{OrderID: "post123"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.NotEmpty(t, r.Header.Get("Apikey"))
		assert.NotEmpty(t, r.Header.Get("Request-Time"))
		assert.NotEmpty(t, r.Header.Get("Signature"))
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[exchange.CreateOrderResponse]{Success: true, Code: 0, Data: resp}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	body, err := client.Post(context.Background(), "/api/v1/private/order/create", map[string]string{"symbol": "BTC_USDT"})
	require.NoError(t, err)
	require.NotEmpty(t, body)
}

func TestClient_Post_NilBody(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[json.RawMessage]{Success: true, Code: 0}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	body, err := client.Post(context.Background(), "/api/v1/private/test", nil)
	require.NoError(t, err)
	require.NotEmpty(t, body)
}

func TestClient_WarmUp(t *testing.T) {
	t.Parallel()
	pingCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pingCount++
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[int64]{Success: true, Code: 0, Data: time.Now().UnixMilli()}))
	}))
	defer srv.Close()

	client := newTestClient(srv)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	client.WarmUp(ctx, time.Hour) // RunImmediate: fires once then ctx expires
	assert.GreaterOrEqual(t, pingCount, 1, "WarmUp should have pinged at least once")
}

func TestClient_Latency_Error(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`error`))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	_, err := client.Latency(context.Background())
	assert.Error(t, err)
}

func TestClient_GetCtx_CancelledContext(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(time.Second)
		_, _ = w.Write([]byte(`ok`))
	}))
	defer srv.Close()

	client := newTestClient(srv)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancel

	_, err := client.Get(ctx, "/api/v1/contract/ping", nil)
	assert.Error(t, err)
}

func TestClient_doRequest_NonOK_NonRateLimit(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"forbidden"}`))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	_, err := client.Get(context.Background(), "/api/v1/contract/ping", nil)
	assert.Error(t, err)
	assert.False(t, exchange.IsRateLimitError(err))
}

func TestClient_Get_WithParams(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.RawQuery, "symbol=BTC_USDT")
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[json.RawMessage]{Success: true, Code: 0}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	_, err := client.Get(context.Background(), "/api/v1/contract/detail", map[string]any{"symbol": "BTC_USDT"})
	assert.NoError(t, err)
}

func TestClient_GetCtx_PrivateEndpoint(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Private endpoints should have auth headers.
		assert.NotEmpty(t, r.Header.Get("Apikey"))
		assert.NotEmpty(t, r.Header.Get("Request-Time"))
		assert.NotEmpty(t, r.Header.Get("Signature"))
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[json.RawMessage]{Success: true, Code: 0}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	_, err := client.GetCtx(context.Background(), "/api/v1/private/account/assets", nil)
	assert.NoError(t, err)
}

func TestClient_NewClient_WithHTTPLogging(t *testing.T) {
	t.Parallel()
	// Ensure no panic when creating client with HTTP logging and nil transport.
	client := mexc.NewClient(&http.Client{}, "http://localhost", "k", "s", config.LoggingConfig{HTTP: true})
	assert.NotNil(t, client)
}

func TestClient_GetRecentClosedPnL(t *testing.T) {
	t.Parallel()

	type mockResponse struct {
		Success bool   `json:"success"`
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    any    `json:"data"`
	}

	tests := []struct {
		name          string
		symbol        string
		targetOrderID string
		mockOrder     mockResponse
		mockHistoryFn func() mockResponse
		expectedErr   string
		expectedInfo  *exchange.ClosedPnLInfo
	}{
		{
			name:          "success fresh closed position",
			symbol:        "ID_USDT",
			targetOrderID: "ord-123",
			mockOrder: mockResponse{
				Success: true,
				Code:    0,
				Data: map[string]any{
					"orderId":    "ord-123",
					"symbol":     "ID_USDT",
					"positionId": 1397401616,
				},
			},
			mockHistoryFn: func() mockResponse {
				currentTime := time.Now().UnixMilli()
				return mockResponse{
					Success: true,
					Code:    0,
					Data: []map[string]any{
						{
							"positionId":      1397401616,
							"symbol":          "ID_USDT",
							"openAvgPrice":    0.0384,
							"closeAvgPrice":   0.03832,
							"closeVol":        39,
							"closeProfitLoss": 0.0312,
							"totalFee":        0.0089,
							"holdFee":         0.0,
							"oim":             3.0059,
							"createTime":      currentTime - 10000,
							"updateTime":      currentTime - 2000, // closed 2 seconds ago (fresh!)
							"profitRatio":     (0.0312 - 0.0089) / 3.0059,
						},
					},
				}
			},
			expectedInfo: &exchange.ClosedPnLInfo{
				Exchange:   "mexc",
				Symbol:     "ID_USDT",
				EntryPrice: 0.0384,
				ExitPrice:  0.03832,
				ClosedSize: 39,
				GrossPnL:   0.0312,
				Fee:        0.0089,
				FundingFee: 0,
				DurationMs: 8000,
				NetPnl:     0.0312 - 0.0089,
				PnLRate:    ((0.0312 - 0.0089) / 3.0059) * 100,
			},
		},
		{
			name:          "error stale closed position",
			symbol:        "ID_USDT",
			targetOrderID: "ord-123",
			mockOrder: mockResponse{
				Success: true,
				Code:    0,
				Data: map[string]any{
					"orderId":    "ord-123",
					"symbol":     "ID_USDT",
					"positionId": 1397401616,
				},
			},
			mockHistoryFn: func() mockResponse {
				currentTime := time.Now().UnixMilli()
				return mockResponse{
					Success: true,
					Code:    0,
					Data: []map[string]any{
						{
							"positionId":      1397401616,
							"symbol":          "ID_USDT",
							"openAvgPrice":    0.0384,
							"closeAvgPrice":   0.03832,
							"closeVol":        39,
							"closeProfitLoss": 0.0312,
							"totalFee":        0.0089,
							"holdFee":         0.0,
							"oim":             3.0059,
							"createTime":      currentTime - 30000,
							"updateTime":      currentTime - 20000, // closed 20 seconds ago (stale!)
							"profitRatio":     (0.0312 - 0.0089) / 3.0059,
						},
					},
				}
			},
			expectedErr: "query closed pnl failed: found stale closed position record",
		},
		{
			name:          "error position not found",
			symbol:        "ID_USDT",
			targetOrderID: "ord-123",
			mockOrder: mockResponse{
				Success: true,
				Code:    0,
				Data: map[string]any{
					"orderId":    "ord-123",
					"symbol":     "ID_USDT",
					"positionId": 1397401616,
				},
			},
			mockHistoryFn: func() mockResponse {
				return mockResponse{
					Success: true,
					Code:    0,
					Data:    []map[string]any{},
				}
			},
			expectedErr: "query closed pnl failed: position record for ID 1397401616 not yet closed",
		},
		{
			name:          "error empty orderID",
			symbol:        "ID_USDT",
			targetOrderID: "",
			expectedErr:   "orderID is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == fmt.Sprintf("/api/v1/private/order/external/%s/%s", tt.symbol, tt.targetOrderID) {
					_, _ = w.Write(mustJSON(t, tt.mockOrder))
					return
				}
				if r.URL.Path == "/api/v1/private/position/list/history_positions" {
					if tt.mockHistoryFn != nil {
						_, _ = w.Write(mustJSON(t, tt.mockHistoryFn()))
					}
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))
			defer srv.Close()

			client := newTestClient(srv)
			info, err := client.GetRecentClosedPnL(context.Background(), tt.symbol, tt.targetOrderID, time.Time{})

			if tt.expectedErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedErr)
				assert.Nil(t, info)
			} else {
				require.NoError(t, err)
				require.NotNil(t, info)
				assert.Equal(t, tt.expectedInfo.Symbol, info.Symbol)
				assert.InDelta(t, tt.expectedInfo.EntryPrice, info.EntryPrice, 0.0001)
				assert.InDelta(t, tt.expectedInfo.ExitPrice, info.ExitPrice, 0.0001)
				assert.InDelta(t, tt.expectedInfo.ClosedSize, info.ClosedSize, 0.0001)
				assert.InDelta(t, tt.expectedInfo.GrossPnL, info.GrossPnL, 0.0001)
				assert.InDelta(t, tt.expectedInfo.Fee, info.Fee, 0.0001)
				assert.InDelta(t, tt.expectedInfo.FundingFee, info.FundingFee, 0.0001)
				assert.Equal(t, tt.expectedInfo.DurationMs, info.DurationMs)
				assert.InDelta(t, tt.expectedInfo.NetPnl, info.NetPnl, 0.0001)
				assert.InDelta(t, tt.expectedInfo.PnLRate, info.PnLRate, 0.0001)
			}
		})
	}
}
