package binance_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/binance"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func startTestWSServer(t *testing.T, handler func(conn *websocket.Conn)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		handler(conn)
	}))
	return srv
}

func wsTestURL(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

func handleTestAuthSession(conn *websocket.Conn, data []byte, expectedKey, expectedSecret string) {
	var req struct {
		ID     string         `json:"id"`
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	if err := json.Unmarshal(data, &req); err != nil || req.Method != "session.logon" {
		return
	}
	k, _ := req.Params["apiKey"].(string)
	sig, _ := req.Params["signature"].(string)

	values := url.Values{}
	for key, val := range req.Params {
		if key != "signature" && val != nil {
			if f, ok := val.(float64); ok {
				values.Set(key, fmt.Sprintf("%.0f", f))
			} else {
				values.Set(key, fmt.Sprintf("%v", val))
			}
		}
	}
	mac := hmac.New(sha256.New, []byte(expectedSecret))
	mac.Write([]byte(values.Encode()))
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	if k == expectedKey && sig == expectedSig {
		resp := map[string]any{
			"id":     req.ID,
			"status": 200,
			"result": map[string]any{
				"apiKey":          expectedKey,
				"authorizedSince": time.Now().UnixMilli(),
			},
		}
		respBytes, _ := json.Marshal(resp)
		_ = conn.WriteMessage(websocket.TextMessage, respBytes)
	}
}

func TestTradeWS_AuthSuccess(t *testing.T) {
	t.Parallel()

	// #nosec G101
	key := "test-binance-key"
	// #nosec G101
	secret := "test-binance-secret"

	server := startTestWSServer(t, func(conn *websocket.Conn) {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			handleTestAuthSession(conn, data, key, secret)
		}
	})
	defer server.Close()

	ctx := t.Context()
	client := binance.NewTradeWSClient(wsTestURL(server), key, secret, exchange.RealClock{}, slog.Default())
	defer client.Close()

	go client.Start(ctx)

	require.Eventually(t, func() bool {
		return client.IsReady()
	}, 2*time.Second, 10*time.Millisecond)
}

func TestTradeWS_AuthFailure(t *testing.T) {
	t.Parallel()

	server := startTestWSServer(t, func(conn *websocket.Conn) {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var req struct {
				ID     string `json:"id"`
				Method string `json:"method"`
			}
			if err := json.Unmarshal(data, &req); err == nil && req.Method == "session.logon" {
				resp := map[string]any{
					"id":     req.ID,
					"status": 401,
					"error": map[string]any{
						"code": -2014,
						"msg":  "API-key format invalid.",
					},
				}
				respBytes, _ := json.Marshal(resp)
				_ = conn.WriteMessage(websocket.TextMessage, respBytes)
			}
		}
	})
	defer server.Close()

	ctx := t.Context()
	client := binance.NewTradeWSClient(wsTestURL(server), "invalid-key", "secret", exchange.RealClock{}, slog.Default())
	defer client.Close()

	go client.Start(ctx)

	time.Sleep(100 * time.Millisecond)
	assert.False(t, client.IsReady())
}

func TestTradeWS_EmptyCredentials(t *testing.T) {
	t.Parallel()

	server := startTestWSServer(t, func(conn *websocket.Conn) {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer server.Close()

	ctx := t.Context()
	client := binance.NewTradeWSClient(wsTestURL(server), "", "", exchange.RealClock{}, slog.Default())
	defer client.Close()

	go client.Start(ctx)

	require.Eventually(t, func() bool {
		return client.IsReady()
	}, 2*time.Second, 10*time.Millisecond)
}

func TestTradeWS_CreateOrder_Success(t *testing.T) {
	t.Parallel()

	server := startTestWSServer(t, func(conn *websocket.Conn) {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var req struct {
				ID     string         `json:"id"`
				Method string         `json:"method"`
				Params map[string]any `json:"params"`
			}
			if err := json.Unmarshal(data, &req); err == nil && req.Method == "order.place" {
				sym, _ := req.Params["symbol"].(string)
				qty, _ := req.Params["quantity"].(float64)
				side, _ := req.Params["side"].(string)
				posSide, _ := req.Params["positionSide"].(string)

				if sym == "BTCUSDT" && qty == 0.05 && side == "BUY" && posSide == "LONG" {
					resp := map[string]any{
						"id":     req.ID,
						"status": 200,
						"result": map[string]any{
							"orderId":       987654321,
							"symbol":        "BTCUSDT",
							"status":        "NEW",
							"clientOrderId": "test-cl-id",
							"updateTime":    1710000000500,
						},
					}
					respBytes, _ := json.Marshal(resp)
					_ = conn.WriteMessage(websocket.TextMessage, respBytes)
				}
			}
		}
	})
	defer server.Close()

	ctx := t.Context()
	client := binance.NewTradeWSClient(wsTestURL(server), "", "", exchange.RealClock{}, slog.Default())
	defer client.Close()

	go client.Start(ctx)

	require.Eventually(t, func() bool {
		return client.IsReady()
	}, 2*time.Second, 10*time.Millisecond)

	req := exchange.SubmitOrderRequest{
		Symbol:       "BTCUSDT",
		Side:         exchange.SideOpenLong,
		Type:         exchange.OrderTypeLimit,
		Price:        65000,
		Vol:          0.05,
		PositionMode: domain.PositionModeHedge,
		ExternalOID:  "test-cl-id",
	}

	res, err := client.CreateOrder(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, "987654321", res.OrderID)
	assert.Equal(t, time.UnixMilli(1710000000500), res.Time)
}

func TestTradeWS_CreateOrder_Error(t *testing.T) {
	t.Parallel()

	server := startTestWSServer(t, func(conn *websocket.Conn) {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var req struct {
				ID     string `json:"id"`
				Method string `json:"method"`
			}
			if err := json.Unmarshal(data, &req); err == nil && req.Method == "order.place" {
				resp := map[string]any{
					"id":     req.ID,
					"status": 400,
					"error": map[string]any{
						"code": -2010,
						"msg":  "Account has insufficient balance for requested action.",
					},
				}
				respBytes, _ := json.Marshal(resp)
				_ = conn.WriteMessage(websocket.TextMessage, respBytes)
			}
		}
	})
	defer server.Close()

	ctx := t.Context()
	client := binance.NewTradeWSClient(wsTestURL(server), "", "", exchange.RealClock{}, slog.Default())
	defer client.Close()

	go client.Start(ctx)

	require.Eventually(t, func() bool {
		return client.IsReady()
	}, 2*time.Second, 10*time.Millisecond)

	req := exchange.SubmitOrderRequest{
		Symbol: "BTCUSDT",
		Side:   exchange.SideOpenLong,
		Type:   exchange.OrderTypeMarket,
		Vol:    10.0,
	}

	_, err := client.CreateOrder(ctx, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "-2010")
	assert.Contains(t, err.Error(), "insufficient balance")
}

func TestTradeWS_CancelOrder_SuccessAndIdempotency(t *testing.T) {
	t.Parallel()

	server := startTestWSServer(t, func(conn *websocket.Conn) {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var req struct {
				ID     string         `json:"id"`
				Method string         `json:"method"`
				Params map[string]any `json:"params"`
			}
			if err := json.Unmarshal(data, &req); err == nil && req.Method == "order.cancel" {
				orderID, _ := req.Params["orderId"].(float64)
				switch orderID {
				case 12345:
					resp := map[string]any{
						"id":     req.ID,
						"status": 200,
						"result": map[string]any{
							"orderId": 12345,
							"status":  "CANCELED",
						},
					}
					respBytes, _ := json.Marshal(resp)
					_ = conn.WriteMessage(websocket.TextMessage, respBytes)
				case 99999:
					// Already cancelled / filled error (-2011)
					resp := map[string]any{
						"id":     req.ID,
						"status": 400,
						"error": map[string]any{
							"code": -2011,
							"msg":  "Unknown order sent.",
						},
					}
					respBytes, _ := json.Marshal(resp)
					_ = conn.WriteMessage(websocket.TextMessage, respBytes)
				}
			}
		}
	})
	defer server.Close()

	ctx := t.Context()
	client := binance.NewTradeWSClient(wsTestURL(server), "", "", exchange.RealClock{}, slog.Default())
	defer client.Close()

	go client.Start(ctx)

	require.Eventually(t, func() bool {
		return client.IsReady()
	}, 2*time.Second, 10*time.Millisecond)

	// Normal cancel
	err := client.CancelOrder(ctx, "BTCUSDT", "12345")
	assert.NoError(t, err)

	// Idempotent cancel (code -2011)
	err = client.CancelOrder(ctx, "BTCUSDT", "99999")
	assert.NoError(t, err)
}

func TestTradeWS_PrepareOrder(t *testing.T) {
	t.Parallel()

	server := startTestWSServer(t, func(conn *websocket.Conn) {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var req struct {
				ID     string `json:"id"`
				Method string `json:"method"`
			}
			if err := json.Unmarshal(data, &req); err == nil && req.Method == "order.place" {
				resp := map[string]any{
					"id":     req.ID,
					"status": 200,
					"result": map[string]any{
						"orderId":    778899,
						"symbol":     "ETHUSDT",
						"updateTime": 1710000000888,
					},
				}
				respBytes, _ := json.Marshal(resp)
				_ = conn.WriteMessage(websocket.TextMessage, respBytes)
			}
		}
	})
	defer server.Close()

	ctx := t.Context()
	client := binance.NewTradeWSClient(wsTestURL(server), "", "", exchange.RealClock{}, slog.Default())
	defer client.Close()

	// Not ready before start
	_, err := client.PrepareOrder(ctx, exchange.SubmitOrderRequest{})
	assert.ErrorIs(t, err, binance.ErrWSTradeNotReady)

	go client.Start(ctx)
	require.Eventually(t, func() bool {
		return client.IsReady()
	}, 2*time.Second, 10*time.Millisecond)

	submitReq := exchange.SubmitOrderRequest{
		Symbol: "ETHUSDT",
		Side:   exchange.SideOpenShort,
		Type:   exchange.OrderTypePostOnly,
		Price:  3500,
		Vol:    1.0,
	}

	execFn, err := client.PrepareOrder(ctx, submitReq)
	require.NoError(t, err)
	require.NotNil(t, execFn)

	res, err := execFn(ctx)
	require.NoError(t, err)
	assert.Equal(t, "778899", res.OrderID)
	assert.Equal(t, time.UnixMilli(1710000000888), res.Time)
}

func TestTradeWS_PingAndLatency(t *testing.T) {
	t.Parallel()

	var pingMu sync.Mutex
	pingReceived := false

	server := startTestWSServer(t, func(conn *websocket.Conn) {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var req struct {
				ID     string `json:"id"`
				Method string `json:"method"`
			}
			if err := json.Unmarshal(data, &req); err == nil && req.Method == "ping" {
				pingMu.Lock()
				pingReceived = true
				pingMu.Unlock()

				resp := map[string]any{
					"id":     req.ID,
					"status": 200,
					"result": map[string]any{},
				}
				respBytes, _ := json.Marshal(resp)
				_ = conn.WriteMessage(websocket.TextMessage, respBytes)
			}
		}
	})
	defer server.Close()

	ctx := t.Context()
	client := binance.NewTradeWSClient(wsTestURL(server), "", "", exchange.RealClock{}, slog.Default())
	defer client.Close()

	go client.Start(ctx)
	require.Eventually(t, func() bool {
		return client.IsReady()
	}, 2*time.Second, 10*time.Millisecond)

	err := client.Ping(ctx)
	require.NoError(t, err)

	pingMu.Lock()
	received := pingReceived
	pingMu.Unlock()
	assert.True(t, received)

	assert.GreaterOrEqual(t, client.LatencyMs(), int64(0))
	assert.GreaterOrEqual(t, client.MinLatencyMs(), int64(0))
	assert.GreaterOrEqual(t, client.MedianLatencyMs(), int64(0))
	assert.GreaterOrEqual(t, client.LastLatencyMs(), int64(0))
}

func TestTradeWS_Helpers(t *testing.T) {
	t.Parallel()

	// 1. DefaultTradeURL
	assert.Equal(t, "wss://ws-fapi.binance.com/ws-fapi/v1", binance.DefaultTradeURL("https://fapi.binance.com"))
	assert.Equal(t, "wss://testnet.binancefuture.com/ws-fapi/v1", binance.DefaultTradeURL("https://testnet.binancefuture.com"))

	// 2. ExtractBinanceReqID
	id, ok := binance.ExtractBinanceReqID([]byte(`{"id":"req-123","status":200}`))
	assert.True(t, ok)
	assert.Equal(t, "req-123", id)

	idNum, ok := binance.ExtractBinanceReqID([]byte(`{"id":456,"status":200}`))
	assert.True(t, ok)
	assert.Equal(t, "456", idNum)

	_, ok = binance.ExtractBinanceReqID([]byte(`{"status":200}`))
	assert.False(t, ok)

	// 3. SignBinanceWSParams
	params := map[string]any{
		"symbol":   "BTCUSDT",
		"quantity": 0.5,
	}
	signed := binance.SignBinanceWSParams(params, "k", "s", 1700000000000)
	assert.Equal(t, "k", signed["apiKey"])
	assert.Equal(t, int64(1700000000000), signed["timestamp"])
	assert.NotEmpty(t, signed["signature"])

	// 4. IsBinancePong
	assert.True(t, binance.IsBinancePong([]byte(`{"id":"ping","status":200,"result":{}}`)))
	assert.True(t, binance.IsBinancePong([]byte(`pong`)))
	assert.False(t, binance.IsBinancePong([]byte(`{"id":"other"}`)))
}
