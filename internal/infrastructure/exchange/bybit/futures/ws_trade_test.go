package futures_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	futures "crypto-bot/internal/infrastructure/exchange/bybit/futures"

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

func TestTradeWS_AuthSuccess(t *testing.T) {
	t.Parallel()

	apiKey := "my-api-key"
	apiSecret := "my-api-secret"

	server := startTestWSServer(t, func(conn *websocket.Conn) {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var authReq struct {
				Op   string `json:"op"`
				Args []any  `json:"args"`
			}
			if err := json.Unmarshal(data, &authReq); err == nil && authReq.Op == "auth" {
				// Verify auth args
				if len(authReq.Args) == 3 {
					k, _ := authReq.Args[0].(string)
					exp, _ := authReq.Args[1].(float64)
					sig, _ := authReq.Args[2].(string)

					expectedRaw := fmt.Sprintf("GET/realtime%d", int64(exp))
					h := hmac.New(sha256.New, []byte(apiSecret))
					h.Write([]byte(expectedRaw))
					expectedSig := hex.EncodeToString(h.Sum(nil))

					if k == apiKey && sig == expectedSig {
						resp := map[string]any{
							"retCode": 0,
							"retMsg":  "OK",
							"op":      "auth",
							"connId":  "test-conn-1",
						}
						respBytes, _ := json.Marshal(resp)
						_ = conn.WriteMessage(websocket.TextMessage, respBytes)
					}
				}
			}
		}
	})
	defer server.Close()

	ctx := t.Context()

	client := futures.NewTradeWSClient(wsTestURL(server), apiKey, apiSecret, exchange.RealClock{}, slog.Default())
	defer client.Close()

	go client.Start(ctx)

	// Wait until client becomes ready
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
			var authReq struct {
				Op string `json:"op"`
			}
			if err := json.Unmarshal(data, &authReq); err == nil && authReq.Op == "auth" {
				resp := map[string]any{
					"retCode": 10004,
					"retMsg":  "invalid sign",
					"op":      "auth",
				}
				respBytes, _ := json.Marshal(resp)
				_ = conn.WriteMessage(websocket.TextMessage, respBytes)
			}
		}
	})
	defer server.Close()

	ctx := t.Context()

	client := futures.NewTradeWSClient(wsTestURL(server), "key", "secret", exchange.RealClock{}, slog.Default())
	defer client.Close()

	go client.Start(ctx)

	time.Sleep(100 * time.Millisecond)
	assert.False(t, client.IsReady())
}

func TestTradeWS_CreateOrder_Success(t *testing.T) {
	t.Parallel()

	server := startTestWSServer(t, func(conn *websocket.Conn) {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var env struct {
				ReqID  string `json:"reqId"`
				Op     string `json:"op"`
				Header struct {
					Timestamp string `json:"X-BAPI-TIMESTAMP"`
				} `json:"header"`
				Args []struct {
					Symbol string `json:"symbol"`
					Qty    string `json:"qty"`
				} `json:"args"`
			}
			_ = json.Unmarshal(data, &env)
			switch env.Op {
			case "auth":
				resp := map[string]any{"retCode": 0, "retMsg": "OK", "op": "auth", "connId": "conn-1"}
				b, _ := json.Marshal(resp)
				_ = conn.WriteMessage(websocket.TextMessage, b)
			case "order.create":
				assert.NotEmpty(t, env.ReqID)
				assert.NotEmpty(t, env.Header.Timestamp)
				assert.Equal(t, "BTCUSDT", env.Args[0].Symbol)

				resp := map[string]any{
					"reqId":   env.ReqID,
					"retCode": 0,
					"retMsg":  "OK",
					"op":      "order.create",
					"data": map[string]any{
						"orderId":     "ord-bybit-999",
						"orderLinkId": "link-1",
					},
					"header": map[string]any{
						"Timenow": "1711001595209",
					},
				}
				b, _ := json.Marshal(resp)
				_ = conn.WriteMessage(websocket.TextMessage, b)
			}
		}
	})
	defer server.Close()

	ctx := t.Context()

	client := futures.NewTradeWSClient(wsTestURL(server), "k", "s", exchange.RealClock{}, slog.Default())
	defer client.Close()
	go client.Start(ctx)

	require.Eventually(t, func() bool { return client.IsReady() }, 2*time.Second, 10*time.Millisecond)

	req := exchange.SubmitOrderRequest{
		Symbol:       "BTCUSDT",
		Vol:          0.5,
		Price:        60000,
		Side:         exchange.SideOpenLong,
		Type:         exchange.OrderTypeLimit,
		PositionMode: 1,
	}

	res, err := client.CreateOrder(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, "ord-bybit-999", res.OrderID)
	assert.Equal(t, int64(1711001595209), res.Time.UnixMilli())
}

func TestTradeWS_CreateOrder_ErrorResponse(t *testing.T) {
	t.Parallel()

	server := startTestWSServer(t, func(conn *websocket.Conn) {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var env struct {
				ReqID string `json:"reqId"`
				Op    string `json:"op"`
			}
			_ = json.Unmarshal(data, &env)
			switch env.Op {
			case "auth":
				resp := map[string]any{"retCode": 0, "retMsg": "OK", "op": "auth", "connId": "conn-1"}
				b, _ := json.Marshal(resp)
				_ = conn.WriteMessage(websocket.TextMessage, b)
			case "order.create":
				resp := map[string]any{
					"reqId":   env.ReqID,
					"retCode": 10404,
					"retMsg":  "category is not correct",
					"op":      "order.create",
				}
				b, _ := json.Marshal(resp)
				_ = conn.WriteMessage(websocket.TextMessage, b)
			}
		}
	})
	defer server.Close()

	ctx := t.Context()

	client := futures.NewTradeWSClient(wsTestURL(server), "k", "s", exchange.RealClock{}, slog.Default())
	defer client.Close()
	go client.Start(ctx)

	require.Eventually(t, func() bool { return client.IsReady() }, 2*time.Second, 10*time.Millisecond)

	req := exchange.SubmitOrderRequest{
		Symbol:       "BTCUSDT",
		Vol:          0.5,
		Side:         exchange.SideOpenLong,
		Type:         exchange.OrderTypeMarket,
		PositionMode: 1,
	}

	_, err := client.CreateOrder(ctx, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "10404")
	assert.Contains(t, err.Error(), "category is not correct")
}

func TestTradeWS_CancelOrder(t *testing.T) {
	t.Parallel()

	server := startTestWSServer(t, func(conn *websocket.Conn) {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var env struct {
				ReqID string `json:"reqId"`
				Op    string `json:"op"`
				Args  []struct {
					OrderID string `json:"orderId"`
				} `json:"args"`
			}
			_ = json.Unmarshal(data, &env)
			switch env.Op {
			case "auth":
				b, _ := json.Marshal(map[string]any{"retCode": 0, "retMsg": "OK", "op": "auth", "connId": "conn-1"})
				_ = conn.WriteMessage(websocket.TextMessage, b)
			case "order.cancel":
				if env.Args[0].OrderID == "ord-already-cancelled" {
					b, _ := json.Marshal(map[string]any{
						"reqId":   env.ReqID,
						"retCode": 110001,
						"retMsg":  "order already cancelled",
						"op":      "order.cancel",
					})
					_ = conn.WriteMessage(websocket.TextMessage, b)
				} else {
					b, _ := json.Marshal(map[string]any{
						"reqId":   env.ReqID,
						"retCode": 0,
						"retMsg":  "OK",
						"op":      "order.cancel",
					})
					_ = conn.WriteMessage(websocket.TextMessage, b)
				}
			}
		}
	})
	defer server.Close()

	ctx := t.Context()

	client := futures.NewTradeWSClient(wsTestURL(server), "k", "s", exchange.RealClock{}, slog.Default())
	defer client.Close()
	go client.Start(ctx)

	require.Eventually(t, func() bool { return client.IsReady() }, 2*time.Second, 10*time.Millisecond)

	// Normal cancel
	err := client.CancelOrder(ctx, "BTCUSDT", "ord-normal")
	assert.NoError(t, err)

	// Idempotent cancel
	err = client.CancelOrder(ctx, "BTCUSDT", "ord-already-cancelled")
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
			var env struct {
				ReqID string `json:"reqId"`
				Op    string `json:"op"`
			}
			_ = json.Unmarshal(data, &env)
			switch env.Op {
			case "auth":
				b, _ := json.Marshal(map[string]any{"retCode": 0, "retMsg": "OK", "op": "auth", "connId": "conn-1"})
				_ = conn.WriteMessage(websocket.TextMessage, b)
			case "order.create":
				b, _ := json.Marshal(map[string]any{
					"reqId":   env.ReqID,
					"retCode": 0,
					"retMsg":  "OK",
					"op":      "order.create",
					"data":    map[string]any{"orderId": "presign-ord-1"},
					"header":  map[string]any{"Timenow": "1711001595500"},
				})
				_ = conn.WriteMessage(websocket.TextMessage, b)
			}
		}
	})
	defer server.Close()

	ctx := t.Context()

	client := futures.NewTradeWSClient(wsTestURL(server), "k", "s", exchange.RealClock{}, slog.Default())
	defer client.Close()
	go client.Start(ctx)

	require.Eventually(t, func() bool { return client.IsReady() }, 2*time.Second, 10*time.Millisecond)

	req := exchange.SubmitOrderRequest{
		Symbol:       "BTCUSDT",
		Vol:          1.0,
		Side:         exchange.SideOpenLong,
		Type:         exchange.OrderTypeMarket,
		PositionMode: 1,
	}

	dispatch, err := client.PrepareOrder(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, dispatch)

	res, err := dispatch(ctx)
	require.NoError(t, err)
	assert.Equal(t, "presign-ord-1", res.OrderID)
	assert.Equal(t, int64(1711001595500), res.Time.UnixMilli())
}

func TestTradeWS_ConcurrentOrders(t *testing.T) {
	t.Parallel()

	var srvMu sync.Mutex
	server := startTestWSServer(t, func(conn *websocket.Conn) {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var env struct {
				ReqID string `json:"reqId"`
				Op    string `json:"op"`
			}
			_ = json.Unmarshal(data, &env)
			switch env.Op {
			case "auth":
				b, _ := json.Marshal(map[string]any{"retCode": 0, "retMsg": "OK", "op": "auth", "connId": "conn-1"})
				srvMu.Lock()
				_ = conn.WriteMessage(websocket.TextMessage, b)
				srvMu.Unlock()
			case "order.create":
				reqID := env.ReqID
				go func(id string) {
					b, _ := json.Marshal(map[string]any{
						"reqId":   id,
						"retCode": 0,
						"retMsg":  "OK",
						"op":      "order.create",
						"data":    map[string]any{"orderId": "ord-" + id},
						"header":  map[string]any{"Timenow": "1711001595999"},
					})
					srvMu.Lock()
					_ = conn.WriteMessage(websocket.TextMessage, b)
					srvMu.Unlock()
				}(reqID)
			}
		}
	})

	defer server.Close()

	ctx := t.Context()

	client := futures.NewTradeWSClient(wsTestURL(server), "k", "s", exchange.RealClock{}, slog.Default())
	defer client.Close()
	go client.Start(ctx)

	require.Eventually(t, func() bool { return client.IsReady() }, 2*time.Second, 10*time.Millisecond)

	var wg sync.WaitGroup
	const total = 20

	for i := range total {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := exchange.SubmitOrderRequest{
				Symbol:       "BTCUSDT",
				Vol:          0.1,
				Side:         exchange.SideOpenLong,
				Type:         exchange.OrderTypeMarket,
				PositionMode: 1,
			}
			res, err := client.CreateOrder(ctx, req)
			assert.NoError(t, err)
			assert.NotEmpty(t, res.OrderID)
		}(i)
	}

	wg.Wait()
}

func TestBybitWS_PingPong_And_Latency(t *testing.T) {
	t.Parallel()

	// 1. Verify IsBybitPong and IsBybitPing
	assert.True(t, futures.IsBybitPong([]byte(`{"op":"pong","args":["1711000"]}`)))
	assert.True(t, futures.IsBybitPong([]byte(`{"op": "pong"}`)))
	assert.True(t, futures.IsBybitPong([]byte(`pong`)))
	assert.False(t, futures.IsBybitPong([]byte(`{"op":"ping"}`)))

	assert.True(t, futures.IsBybitPing([]byte(`{"op":"ping"}`)))
	assert.True(t, futures.IsBybitPing([]byte(`ping`)))
	assert.False(t, futures.IsBybitPing([]byte(`{"op":"pong"}`)))

	// 2. Integration with TradeWSClient Ping & LatencyMs
	server := startTestWSServer(t, func(conn *websocket.Conn) {
		for {
			mt, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			str := string(data)
			if strings.Contains(str, `"op":"auth"`) || strings.Contains(str, `"op": "auth"`) {
				_ = conn.WriteMessage(mt, []byte(`{"op":"auth","retCode":0,"retMsg":"OK","connId":"test-conn-1"}`))
			}
			if strings.Contains(str, `"op":"ping"`) || strings.Contains(str, `"op": "ping"`) {
				_ = conn.WriteMessage(mt, []byte(`{"op":"pong","args":["1711000"]}`))
			}
		}
	})
	defer server.Close()

	ctx := t.Context()
	client := futures.NewTradeWSClient(wsTestURL(server), "k", "s", exchange.RealClock{}, slog.Default())
	defer client.Close()
	go client.Start(ctx)

	require.Eventually(t, func() bool { return client.IsReady() }, 2*time.Second, 10*time.Millisecond)

	assert.NoError(t, client.Ping(ctx))
	assert.Eventually(t, func() bool {
		return client.LatencyMs() >= 0
	}, 3*time.Second, 10*time.Millisecond)
}
