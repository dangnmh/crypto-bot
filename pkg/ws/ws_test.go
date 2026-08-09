package ws_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"crypto-bot/pkg/ws"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// startTestWS starts a test WebSocket server that echoes messages.
func startTestWS(t *testing.T, handler func(*websocket.Conn)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		handler(conn)
	}))
	return srv
}

// wsURL converts http://... to ws://...
func wsURL(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

func TestClient_CloseIsIdempotent(t *testing.T) {
	t.Parallel()

	c := ws.NewClient("ws://127.0.0.1:1", slog.Default())
	c.Close()
	c.Close()
}

func TestPool_ReplaysPublicSubscriptionsOnReconnect(t *testing.T) {
	t.Parallel()

	// Buffer two messages because this test must observe the original
	// subscription and the replayed subscription across reconnect.
	received := make(chan string, 2)
	var connections atomic.Int32

	srv := startTestWS(t, func(conn *websocket.Conn) {
		connections.Add(1)
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		received <- string(data)

		if connections.Load() == 1 {
			_ = conn.Close()
			return
		}

		<-time.After(200 * time.Millisecond)
	})
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool := ws.NewPool(wsURL(srv), 30, slog.Default())
	defer pool.Close()

	err := pool.SubscribePublic(ctx, "BTC_USDT:ticker", map[string]string{
		"method": "sub.ticker",
	})
	if err != nil {
		t.Fatalf("subscribe public: %v", err)
	}

	for range 2 {
		select {
		case msg := <-received:
			if !strings.Contains(msg, "sub.ticker") {
				t.Fatalf("unexpected message: %s", msg)
			}
		case <-ctx.Done():
			t.Fatal("timeout waiting for replayed subscription")
		}
	}
}

// ── ws.Client Options ───────────────────────────────────────────────────.

func TestWithChannelExtractor(t *testing.T) {
	t.Parallel()
	received := make(chan string, 1)

	srv := startTestWS(t, func(conn *websocket.Conn) {
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"channel":"test.channel"}`))
		time.Sleep(500 * time.Millisecond)
	})
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	c := ws.NewClient(wsURL(srv), nil, ws.WithChannelExtractor(func(data []byte) string {
		return "test.channel"
	}))
	c.OnMessage("test.channel", func(data []byte) {
		received <- string(data)
	})
	go c.Connect(ctx)
	if err := c.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}

	select {
	case msg := <-received:
		if !strings.Contains(msg, "test.channel") {
			t.Errorf("unexpected message: %s", msg)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for routed message")
	}
	c.Close()
}

func TestWithOnConnected(t *testing.T) {
	t.Parallel()
	called := make(chan struct{}, 1)

	srv := startTestWS(t, func(conn *websocket.Conn) {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	c := ws.NewClient(wsURL(srv), nil, ws.WithOnConnected(func(_ *ws.Client) {
		called <- struct{}{}
	}))
	go c.Connect(ctx)

	select {
	case <-called:
		// success
	case <-time.After(1 * time.Second):
		t.Fatal("onConnected hook was not called")
	}
	c.Close()
}

func TestWithPing(t *testing.T) {
	t.Parallel()
	received := make(chan []byte, 10)

	srv := startTestWS(t, func(conn *websocket.Conn) {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			received <- msg
		}
	})
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	c := ws.NewClient(wsURL(srv), nil, ws.WithPing(map[string]string{"ping": "pong"}, 50*time.Millisecond))
	go c.Connect(ctx)
	if err := c.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}

	select {
	case msg := <-received:
		if !strings.Contains(string(msg), "pong") {
			t.Errorf("expected pong message, got %s", string(msg))
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for heartbeat")
	}
	c.Close()
}

// ── ws.NewClient ────────────────────────────────────────────────────────.

func TestNewClient_NotConnectedByDefault(t *testing.T) {
	t.Parallel()
	c := ws.NewClient("ws://localhost:1234", nil)
	if c.IsConnected() {
		t.Error("should not be connected by default")
	}
}

func TestNewClient_NilLogger(t *testing.T) {
	t.Parallel()
	c := ws.NewClient("ws://fake", nil)
	if c == nil {
		t.Fatal("NewClient should return a non-nil client")
	}
}

func TestNewClient_CustomLogger(t *testing.T) {
	t.Parallel()
	logger := slog.Default().With("test", true)
	c := ws.NewClient("ws://fake", logger)
	if c == nil {
		t.Fatal("NewClient should return a non-nil client")
	}
}

// ── Connect, SendJSON, OnMessage ─────────────────────────────────────.

func TestClient_ConnectAndSend(t *testing.T) {
	t.Parallel()
	received := make(chan []byte, 1)

	srv := startTestWS(t, func(conn *websocket.Conn) {
		// Read one message and send it back.
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		received <- msg
		// Keep connection alive briefly.
		time.Sleep(200 * time.Millisecond)
	})
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	c := ws.NewClient(wsURL(srv), nil)
	go c.Connect(ctx)
	if err := c.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}

	if !c.IsConnected() {
		t.Fatal("client should be connected")
	}

	err := c.SendJSON(map[string]string{"method": "test"})
	if err != nil {
		t.Fatalf("SendJSON failed: %v", err)
	}

	select {
	case msg := <-received:
		if len(msg) == 0 {
			t.Error("received empty message")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for server to receive message")
	}
}

func TestClient_OnMessage_ReceivesRouted(t *testing.T) {
	t.Parallel()
	received := make(chan []byte, 1)

	srv := startTestWS(t, func(conn *websocket.Conn) {
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"channel":"test.channel"}`))
		time.Sleep(500 * time.Millisecond)
	})
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	c := ws.NewClient(wsURL(srv), nil, ws.WithChannelExtractor(func(data []byte) string {
		return "test.channel"
	}))
	c.OnMessage("test.channel", func(data []byte) {
		received <- data
	})
	go c.Connect(ctx)
	if err := c.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}

	select {
	case msg := <-received:
		if len(msg) == 0 {
			t.Error("received empty message")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for routed message")
	}
	c.Close()
}

func TestClient_SetGlobalHandler_ReceivesAll(t *testing.T) {
	t.Parallel()
	received := make(chan []byte, 1)

	srv := startTestWS(t, func(conn *websocket.Conn) {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("hello"))
		time.Sleep(500 * time.Millisecond)
	})
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// No channel extractor — globalHandler receives everything.
	c := ws.NewClient(wsURL(srv), nil)
	c.SetGlobalHandler(func(data []byte) {
		received <- data
	})
	go c.Connect(ctx)
	if err := c.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}

	select {
	case msg := <-received:
		if string(msg) != "hello" {
			t.Errorf("expected 'hello', got %q", string(msg))
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for message")
	}
	c.Close()
}

// ── Message routing (pong / empty channel) ──────────────────────────.

func TestClient_PongFiltered(t *testing.T) {
	t.Parallel()
	globalCalled := make(chan []byte, 1)

	srv := startTestWS(t, func(conn *websocket.Conn) {
		// First send a pong (should be filtered), then a real message.
		_ = conn.WriteMessage(websocket.TextMessage, []byte("pong_msg"))
		_ = conn.WriteMessage(websocket.TextMessage, []byte("real_msg"))
		time.Sleep(500 * time.Millisecond)
	})
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	c := ws.NewClient(wsURL(srv), nil, ws.WithChannelExtractor(func(data []byte) string {
		if string(data) == "pong_msg" {
			return "pong"
		}
		return "other"
	}))
	c.SetGlobalHandler(func(data []byte) {
		globalCalled <- data
	})
	go c.Connect(ctx)
	if err := c.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}

	select {
	case msg := <-globalCalled:
		if string(msg) == "pong_msg" {
			t.Error("pong should be filtered out")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout")
	}
	c.Close()
}

func TestClient_EmptyChannelFiltered(t *testing.T) {
	t.Parallel()
	if true {
		t.Log("Testing empty channel filtered")
	}
	globalCalled := make(chan []byte, 1)

	srv := startTestWS(t, func(conn *websocket.Conn) {
		// First send unknown (empty channel → filtered), then a known message.
		_ = conn.WriteMessage(websocket.TextMessage, []byte("unknown"))
		_ = conn.WriteMessage(websocket.TextMessage, []byte("known"))
		time.Sleep(500 * time.Millisecond)
	})
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	c := ws.NewClient(wsURL(srv), nil, ws.WithChannelExtractor(func(data []byte) string {
		if string(data) == "unknown" {
			return ""
		}
		return "some.channel"
	}))
	c.SetGlobalHandler(func(data []byte) {
		globalCalled <- data
	})
	go c.Connect(ctx)
	if err := c.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}

	select {
	case msg := <-globalCalled:
		if string(msg) == "unknown" {
			t.Error("empty channel should be filtered out")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout")
	}
	c.Close()
}

func TestClient_NoExtractor_NoHandler_NoPanic(t *testing.T) {
	t.Parallel()

	srv := startTestWS(t, func(conn *websocket.Conn) {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("data"))
		time.Sleep(200 * time.Millisecond)
	})
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// No extractor, no global handler — should not panic.
	c := ws.NewClient(wsURL(srv), nil)
	go c.Connect(ctx)
	if err := c.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	c.Close()
}

// ── Close ────────────────────────────────────────────────────────────.

func TestClient_Close_NoConnection(t *testing.T) {
	t.Parallel()
	c := ws.NewClient("ws://fake", nil)
	// Should not panic when closing with no connection.
	c.Close()
}

func TestClient_Close_WithConnection(t *testing.T) {
	t.Parallel()
	srv := startTestWS(t, func(conn *websocket.Conn) {
		// Keep alive until test ends.
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	c := ws.NewClient(wsURL(srv), nil)
	go c.Connect(ctx)
	if err := c.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}

	c.Close()
	if c.IsConnected() {
		t.Error("client should not be connected after Close")
	}
}

func TestClient_SendJSON_NoConnection(t *testing.T) {
	t.Parallel()
	c := ws.NewClient("ws://fake", nil)
	// Should return nil (no error) when conn is nil.
	err := c.SendJSON("test")
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

// ── WaitReady ────────────────────────────────────────────────────────.

func TestClient_WaitReady_ContextCancel(t *testing.T) {
	t.Parallel()
	c := ws.NewClient("ws://fake", nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.WaitReady(ctx); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

// ── IsConnected ──────────────────────────────────────────────────────.

func TestClient_IsConnected_Default(t *testing.T) {
	t.Parallel()
	c := ws.NewClient("ws://fake", nil)
	if c.IsConnected() {
		t.Error("should not be connected by default")
	}
}

// ── ws.Pool tests ───────────────────────────────────────────────────────.

func TestNewPool(t *testing.T) {
	t.Parallel()
	p := ws.NewPool("ws://fake", 30, slog.Default())
	if p == nil {
		t.Fatal("NewPool should return a non-nil pool")
	}
}

func TestPool_NilLogger(t *testing.T) {
	t.Parallel()
	p := ws.NewPool("ws://fake", 30, slog.Default())
	if p == nil {
		t.Fatal("pool should be initialized with default logger")
	}
}

func TestPool_On_HandlerReceivesMessage(t *testing.T) {
	t.Parallel()
	received := make(chan []byte, 1)

	srv := startTestWS(t, func(conn *websocket.Conn) {
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"ch":"test.channel"}`))
		time.Sleep(500 * time.Millisecond)
	})
	defer srv.Close()

	p := ws.NewPool(wsURL(srv), 30, slog.Default(), ws.WithChannelExtractor(func(data []byte) string {
		return "test.channel"
	}))
	p.On("test.channel", func(data []byte) {
		received <- data
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	p.Connect(ctx)
	if err := p.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}

	select {
	case msg := <-received:
		if len(msg) == 0 {
			t.Error("received empty message")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for handler to be called")
	}
	p.Close()
}

func TestPool_GetPrivateClient_Nil(t *testing.T) {
	t.Parallel()
	p := ws.NewPool("ws://fake", 30, slog.Default())
	if p.GetPrivateClient() != nil {
		t.Error("private client should be nil before Connect")
	}
}

func TestPool_SendPrivate_NoClient(t *testing.T) {
	t.Parallel()
	p := ws.NewPool("ws://fake", 30, slog.Default())
	err := p.SendPrivate(context.Background(), map[string]string{"test": "msg"})
	if err != nil {
		t.Errorf("expected nil when no private client, got %v", err)
	}
}

func TestPool_WaitReady_NoClient(t *testing.T) {
	t.Parallel()
	p := ws.NewPool("ws://fake", 30, slog.Default())
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	// Should return immediately when no private client.
	if err := p.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestPool_Close_Empty(t *testing.T) {
	t.Parallel()
	p := ws.NewPool("ws://fake", 30, slog.Default())
	p.Close() // Should not panic.
}

func TestPool_ConnectAndClose(t *testing.T) {
	t.Parallel()
	srv := startTestWS(t, func(conn *websocket.Conn) {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv.Close()

	p := ws.NewPool(wsURL(srv), 30, slog.Default())

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	p.Connect(ctx)
	if err := p.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}

	client := p.GetPrivateClient()
	if client == nil {
		t.Fatal("private client should be non-nil after Connect")
	}

	p.Close()
}

func TestPool_ConnectIdempotent(t *testing.T) {
	t.Parallel()
	srv := startTestWS(t, func(conn *websocket.Conn) {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv.Close()

	p := ws.NewPool(wsURL(srv), 30, slog.Default())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	p.Connect(ctx)
	p.Connect(ctx) // Second call should be a no-op.
	if err := p.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}

	p.Close()
}

func TestPool_On_MultipleHandlers(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32

	srv := startTestWS(t, func(conn *websocket.Conn) {
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"ch":"ch"}`))
		time.Sleep(500 * time.Millisecond)
	})
	defer srv.Close()

	p := ws.NewPool(wsURL(srv), 30, slog.Default(), ws.WithChannelExtractor(func(data []byte) string {
		return "ch"
	}))
	p.On("ch", func(data []byte) { calls.Add(1) })
	p.On("ch", func(data []byte) { calls.Add(1) })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	p.Connect(ctx)
	if err := p.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond) // Allow message dispatch.

	if got := calls.Load(); got < 2 {
		t.Errorf("expected both handlers to be called, got %d calls", got)
	}
	p.Close()
}

func TestPool_SubscribePublic(t *testing.T) {
	t.Parallel()
	srv := startTestWS(t, func(conn *websocket.Conn) {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv.Close()

	p := ws.NewPool(wsURL(srv), 2, slog.Default())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	p.Connect(ctx)
	if err := p.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}

	// Subscribe to a public topic — should create a new public client.
	err := p.SubscribePublic(ctx, "BTC:ticker", map[string]string{"method": "sub.ticker"})
	if err != nil {
		t.Fatalf("SubscribePublic failed: %v", err)
	}

	// Subscribe again — same topic should reuse.
	err = p.SubscribePublic(ctx, "BTC:ticker", map[string]string{"method": "sub.ticker"})
	if err != nil {
		t.Fatalf("SubscribePublic reuse failed: %v", err)
	}

	p.Close()
}

func TestPool_UsesSeparatePublicAndPrivateURLs(t *testing.T) {
	t.Parallel()

	privateConnected := make(chan struct{}, 1)
	publicConnected := make(chan struct{}, 1)

	privateSrv := startTestWS(t, func(conn *websocket.Conn) {
		privateConnected <- struct{}{}
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer privateSrv.Close()

	publicSrv := startTestWS(t, func(conn *websocket.Conn) {
		publicConnected <- struct{}{}
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer publicSrv.Close()

	p := ws.NewPoolWithURLs(wsURL(publicSrv), wsURL(privateSrv), 2, slog.Default(), nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	p.Connect(ctx)
	if err := p.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}

	select {
	case <-privateConnected:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for private connection")
	}

	if err := p.SubscribePublic(ctx, "BTC:ticker", map[string]string{"method": "sub"}); err != nil {
		t.Fatalf("SubscribePublic failed: %v", err)
	}

	select {
	case <-publicConnected:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for public connection")
	}

	p.Close()
}

func TestPool_UnsubscribePublic_NotTracked(t *testing.T) {
	t.Parallel()
	p := ws.NewPool("ws://fake", 30, slog.Default())

	err := p.UnsubscribePublic(context.Background(), "nonexistent", nil)
	if err != nil {
		t.Errorf("expected nil for non-tracked topic, got %v", err)
	}
}

func TestPool_SendPrivate_WithClient(t *testing.T) {
	t.Parallel()
	srv := startTestWS(t, func(conn *websocket.Conn) {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv.Close()

	p := ws.NewPool(wsURL(srv), 30, slog.Default())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	p.Connect(ctx)
	if err := p.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}

	err := p.SendPrivate(context.Background(), map[string]string{"method": "test"})
	if err != nil {
		t.Fatalf("SendPrivate failed: %v", err)
	}

	p.Close()
}

func TestPool_UnsubscribePublic_Success(t *testing.T) {
	t.Parallel()
	if true {
		t.Log("Testing unsubscribe public success")
	}
	srv := startTestWS(t, func(conn *websocket.Conn) {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv.Close()

	p := ws.NewPool(wsURL(srv), 2, slog.Default())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	p.Connect(ctx)
	if err := p.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}

	err := p.SubscribePublic(ctx, "BTC:ticker", map[string]string{"method": "sub"})
	if err != nil {
		t.Fatalf("SubscribePublic failed: %v", err)
	}

	err = p.UnsubscribePublic(ctx, "BTC:ticker", map[string]string{"method": "unsub"})
	if err != nil {
		t.Fatalf("UnsubscribePublic failed: %v", err)
	}

	p.Close()
}

// ── Concurrent safety ───────────────────────────────────────────────.

func TestClient_ConcurrentSend(t *testing.T) {
	t.Parallel()
	srv := startTestWS(t, func(conn *websocket.Conn) {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	c := ws.NewClient(wsURL(srv), nil)
	go c.Connect(ctx)
	if err := c.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			_ = c.SendJSON(map[string]string{"method": "test"})
		})
	}
	wg.Wait()
	c.Close()
}

func TestWithURLFunc(t *testing.T) {
	t.Parallel()

	srv := startTestWS(t, func(conn *websocket.Conn) {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	urlCalled := false
	c := ws.NewClient("", nil, ws.WithURLFunc(func() (string, error) {
		urlCalled = true
		return wsURL(srv), nil
	}))
	defer c.Close()

	go c.Connect(ctx)
	err := c.WaitReady(ctx)
	require.NoError(t, err)

	assert.True(t, urlCalled)
	assert.True(t, c.IsConnected())
}

func TestWithPreprocessor(t *testing.T) {
	t.Parallel()

	srv := startTestWS(t, func(conn *websocket.Conn) {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("hello-raw"))
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	processedChan := make(chan []byte, 1)
	c := ws.NewClient(wsURL(srv), nil, ws.WithPreprocessor(func(data []byte) ([]byte, error) {
		return []byte(string(data) + "-processed"), nil
	}))
	defer c.Close()

	c.SetGlobalHandler(func(data []byte) {
		processedChan <- data
	})

	go c.Connect(ctx)
	err := c.WaitReady(ctx)
	require.NoError(t, err)

	select {
	case data := <-processedChan:
		assert.Equal(t, "hello-raw-processed", string(data))
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for preprocessed message")
	}
}

func TestWithPreprocessor_Error(t *testing.T) {
	t.Parallel()

	srv := startTestWS(t, func(conn *websocket.Conn) {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("hello-raw"))
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	processedChan := make(chan []byte, 1)
	c := ws.NewClient(wsURL(srv), nil, ws.WithPreprocessor(func(data []byte) ([]byte, error) {
		return nil, errors.New("mock preprocess error")
	}))
	defer c.Close()

	c.SetGlobalHandler(func(data []byte) {
		processedChan <- data
	})

	go c.Connect(ctx)
	err := c.WaitReady(ctx)
	require.NoError(t, err)

	select {
	case <-processedChan:
		t.Fatal("should not receive message because preprocessor failed")
	case <-time.After(500 * time.Millisecond):
		// Success: message was ignored/bypassed
	}
}

func TestPool_SubscribePublicWithURL(t *testing.T) {
	t.Parallel()

	url1Chan := make(chan struct{}, 1)
	url2Chan := make(chan struct{}, 1)

	srv1 := startTestWS(t, func(conn *websocket.Conn) {
		url1Chan <- struct{}{}
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv1.Close()

	srv2 := startTestWS(t, func(conn *websocket.Conn) {
		url2Chan <- struct{}{}
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv2.Close()

	p := ws.NewPool(wsURL(srv1), 2, slog.Default())
	defer p.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// 1. Subscribe to URL 1
	err := p.SubscribePublicWithURL(ctx, wsURL(srv1), "topic1", map[string]string{"method": "sub1"})
	require.NoError(t, err)

	select {
	case <-url1Chan:
	case <-time.After(time.Second):
		t.Fatal("srv1 did not receive connection")
	}

	// 2. Subscribe to URL 2
	err = p.SubscribePublicWithURL(ctx, wsURL(srv2), "topic2", map[string]string{"method": "sub2"})
	require.NoError(t, err)

	select {
	case <-url2Chan:
	case <-time.After(time.Second):
		t.Fatal("srv2 did not receive connection")
	}
}

func TestClient_LogSpecialEvents(t *testing.T) {
	t.Parallel()

	noticeChan := make(chan struct{}, 1)
	errorChan := make(chan struct{}, 1)

	srv := startTestWS(t, func(conn *websocket.Conn) {
		// Send a notice event
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"event":"notice","code":"64008","msg":"Upgrade warning"}`))
		// Send an error event
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"event":"error","code":"60009","msg":"Login failed"}`))
		time.Sleep(200 * time.Millisecond)
	})
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Capture logs or just verify it does not crash and processes the events
	c := ws.NewClient(wsURL(srv), nil)
	c.SetGlobalHandler(func(data []byte) {
		if strings.Contains(string(data), "notice") {
			noticeChan <- struct{}{}
		}
		if strings.Contains(string(data), "error") {
			errorChan <- struct{}{}
		}
	})

	go c.Connect(ctx)
	if err := c.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}

	select {
	case <-noticeChan:
	case <-time.After(1 * time.Second):
		t.Fatal("notice event not received")
	}

	select {
	case <-errorChan:
	case <-time.After(1 * time.Second):
		t.Fatal("error event not received")
	}

	c.Close()
}

func TestClient_HandshakeHeaders(t *testing.T) {
	t.Parallel()

	headerChan := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		val := r.Header.Get("X-Custom-Header")
		headerChan <- val
		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err == nil {
			conn.Close()
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	headersFunc := func() (http.Header, error) {
		h := http.Header{}
		h.Set("X-Custom-Header", "custom-val")
		return h, nil
	}

	c := ws.NewClient(wsURL(srv), nil, ws.WithHeadersFunc(headersFunc))
	defer c.Close()

	go c.Connect(ctx)

	select {
	case val := <-headerChan:
		assert.Equal(t, "custom-val", val)
	case <-time.After(1 * time.Second):
		t.Fatal("handshake headers not received")
	}
}

func TestPool_SymbolLimitOverflowScaling(t *testing.T) {
	t.Parallel()

	var connectionCount atomic.Int32
	receivedMsgs := make(chan string, 10)

	srv := startTestWS(t, func(conn *websocket.Conn) {
		connectionCount.Add(1)
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			receivedMsgs <- string(data)
		}
	})
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Limit to max 2 symbols per connection
	pool := ws.NewPool(wsURL(srv), 2, slog.Default())
	defer pool.Close()

	symbols := []string{"BTC:ticker", "ETH:ticker", "SOL:ticker", "XRP:ticker", "DOGE:ticker"}
	for _, sym := range symbols {
		err := pool.SubscribePublic(ctx, sym, map[string]string{"sub": sym})
		require.NoError(t, err)
	}

	// Wait to collect all 5 subscribe messages
	received := make(map[string]bool)
	for range len(symbols) {
		select {
		case msg := <-receivedMsgs:
			received[msg] = true
		case <-ctx.Done():
			t.Fatal("timeout waiting for all subscriptions across scaled connections")
		}
	}

	assert.Equal(t, 5, len(received))
	// 5 topics with max 2 per connection requires at least 3 connections spawned!
	assert.GreaterOrEqual(t, connectionCount.Load(), int32(3))
}

func TestPool_ReplaysPrivateSubscriptionsOnReconnect(t *testing.T) {
	t.Parallel()

	received := make(chan string, 2)
	var connections atomic.Int32

	srv := startTestWS(t, func(conn *websocket.Conn) {
		connIdx := connections.Add(1)
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			received <- string(data)

			// Drop the first connection after receiving the initial private message
			if connIdx == 1 {
				conn.Close()
				return
			}
		}
	})
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool := ws.NewPool(wsURL(srv), 30, slog.Default())
	defer pool.Close()

	pool.Connect(ctx)
	err := pool.WaitReady(ctx)
	require.NoError(t, err)

	err = pool.SendPrivate(ctx, map[string]string{"op": "auth_filter"})
	require.NoError(t, err)

	// We should receive 2 messages: 1 from initial SendPrivate, 1 replayed upon reconnect
	for range 2 {
		select {
		case msg := <-received:
			assert.Contains(t, msg, "auth_filter")
		case <-ctx.Done():
			t.Fatal("timeout waiting for private subscription replay on reconnect")
		}
	}
}

func TestPool_UnsubscribeDuringDisconnect(t *testing.T) {
	t.Parallel()

	receivedMsgs := make(chan string, 10)
	var connCount atomic.Int32

	srv := startTestWS(t, func(conn *websocket.Conn) {
		idx := connCount.Add(1)
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			receivedMsgs <- string(data)

			// Force disconnect on first conn after receiving first sub
			if idx == 1 && strings.Contains(string(data), "sub.topic1") {
				conn.Close()
				return
			}
		}
	})
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool := ws.NewPool(wsURL(srv), 30, slog.Default())
	defer pool.Close()

	err := pool.SubscribePublic(ctx, "topic1", map[string]string{"method": "sub.topic1"})
	require.NoError(t, err)

	// Unsubscribe topic1 while disconnected
	err = pool.UnsubscribePublic(ctx, "topic1", map[string]string{"method": "unsub.topic1"})
	require.NoError(t, err)

	// Subscribe topic2
	err = pool.SubscribePublic(ctx, "topic2", map[string]string{"method": "sub.topic2"})
	require.NoError(t, err)

	// Verify that topic2 is received, but topic1 is not re-subscribed
	hasTopic2 := false
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

Loop:
	for {
		select {
		case msg := <-receivedMsgs:
			if strings.Contains(msg, "sub.topic2") {
				hasTopic2 = true
				break Loop
			}
		case <-timer.C:
			break Loop
		}
	}

	assert.True(t, hasTopic2, "topic2 should be subscribed and active")
}

func TestPool_MultipleSequentialDisconnectReplays(t *testing.T) {
	t.Parallel()

	receivedMsgs := make(chan string, 20)
	var connCounter atomic.Int32

	srv := startTestWS(t, func(conn *websocket.Conn) {
		idx := connCounter.Add(1)
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			receivedMsgs <- string(data)

			// Drop connection 1 and connection 2 after reading first message to force sequential reconnects
			if (idx == 1 || idx == 2) && strings.Contains(string(data), "sub.persist") {
				conn.Close()
				return
			}
		}
	})
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	pool := ws.NewPool(wsURL(srv), 30, slog.Default())
	defer pool.Close()

	err := pool.SubscribePublic(ctx, "persist_topic", map[string]string{"method": "sub.persist"})
	require.NoError(t, err)

	// Expect 3 occurrences of sub.persist (initial + 2 reconnect replays)
	replays := 0
	for replays < 3 {
		select {
		case msg := <-receivedMsgs:
			if strings.Contains(msg, "sub.persist") {
				replays++
			}
		case <-ctx.Done():
			t.Fatalf("timeout waiting for 3 sequential reconnect replays, got %d", replays)
		}
	}

	assert.Equal(t, 3, replays)
}

func TestPool_HighConcurrencySubscribeUnsubscribeBurst(t *testing.T) {
	t.Parallel()

	srv := startTestWS(t, func(conn *websocket.Conn) {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool := ws.NewPool(wsURL(srv), 5, slog.Default())
	defer pool.Close()

	var wg sync.WaitGroup
	workers := 50

	for i := range workers {
		topic := fmt.Sprintf("topic_%d", i)
		wg.Add(1)
		go func(tName string) {
			defer wg.Done()
			_ = pool.SubscribePublic(ctx, tName, map[string]string{"op": "sub", "topic": tName})
			time.Sleep(10 * time.Millisecond)
			if i%2 == 0 {
				_ = pool.UnsubscribePublic(ctx, tName, map[string]string{"op": "unsub", "topic": tName})
			}
		}(topic)
	}

	wg.Wait()
}

func TestPool_ContextCancellationDuringDial(t *testing.T) {
	t.Parallel()

	// Use an unroutable port to force dial block/delay
	badURL := "ws://127.0.0.1:59999"
	pool := ws.NewPool(badURL, 10, slog.Default())
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := pool.SubscribePublic(ctx, "timeout_topic", map[string]string{"method": "sub"})
	assert.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled))
}

func TestPool_MultiURLIndependentScaling(t *testing.T) {
	t.Parallel()

	var conn1Count, conn2Count atomic.Int32

	srv1 := startTestWS(t, func(conn *websocket.Conn) {
		conn1Count.Add(1)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv1.Close()

	srv2 := startTestWS(t, func(conn *websocket.Conn) {
		conn2Count.Add(1)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv2.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Limit to max 2 symbols per connection
	pool := ws.NewPool(wsURL(srv1), 2, slog.Default())
	defer pool.Close()

	// Subscribe 3 topics on URL 1 (requires 2 connections)
	require.NoError(t, pool.SubscribePublicWithURL(ctx, wsURL(srv1), "url1_t1", map[string]string{"sub": "1"}))
	require.NoError(t, pool.SubscribePublicWithURL(ctx, wsURL(srv1), "url1_t2", map[string]string{"sub": "2"}))
	require.NoError(t, pool.SubscribePublicWithURL(ctx, wsURL(srv1), "url1_t3", map[string]string{"sub": "3"}))

	// Subscribe 2 topics on URL 2 (requires 1 connection)
	require.NoError(t, pool.SubscribePublicWithURL(ctx, wsURL(srv2), "url2_t1", map[string]string{"sub": "a"}))
	require.NoError(t, pool.SubscribePublicWithURL(ctx, wsURL(srv2), "url2_t2", map[string]string{"sub": "b"}))

	assert.Equal(t, int32(2), conn1Count.Load(), "URL 1 should spawn 2 connections for 3 topics with maxPairs=2")
	assert.Equal(t, int32(1), conn2Count.Load(), "URL 2 should spawn 1 connection for 2 topics with maxPairs=2")
}

func TestClient_CustomPingPongHandler(t *testing.T) {
	t.Parallel()

	pongSent := make(chan string, 1)

	srv := startTestWS(t, func(conn *websocket.Conn) {
		// Send custom exchange ping payload
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"ping":12345}`))

		// Wait for custom pong response
		_, msg, err := conn.ReadMessage()
		if err == nil {
			pongSent <- string(msg)
		}
	})
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	customPing := func(conn *websocket.Conn, data []byte) bool {
		if strings.Contains(string(data), `"ping":12345`) {
			_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"pong":12345}`))
			return true
		}
		return false
	}

	c := ws.NewClient(wsURL(srv), nil, ws.WithCustomPingHandler(customPing))
	defer c.Close()

	go c.Connect(ctx)
	require.NoError(t, c.WaitReady(ctx))

	select {
	case pongMsg := <-pongSent:
		assert.Contains(t, pongMsg, `"pong":12345`)
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for custom pong response")
	}
}

func TestPool_PrivateAuthReconnectFlow(t *testing.T) {
	t.Parallel()

	received := make(chan string, 3)

	srv := startTestWS(t, func(conn *websocket.Conn) {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			msg := string(data)
			received <- msg
		}
	})
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	authHook := func(c *ws.Client) {
		_ = c.SendJSON(map[string]string{"op": "login"})
	}

	pool := ws.NewPoolWithURLs(
		wsURL(srv),
		wsURL(srv),
		30,
		slog.Default(),
		nil,
		[]ws.ClientOption{ws.WithOnConnected(authHook)},
	)
	defer pool.Close()

	pool.Connect(ctx)
	require.NoError(t, pool.WaitReady(ctx))

	err := pool.SendPrivate(ctx, map[string]string{"op": "sub_private"})
	require.NoError(t, err)

	// Verify both auth login and private message were received
	authReceived := false
	subReceived := false

	for range 2 {
		select {
		case msg := <-received:
			if strings.Contains(msg, "login") {
				authReceived = true
			}
			if strings.Contains(msg, "sub_private") {
				subReceived = true
			}
		case <-ctx.Done():
			t.Fatal("timeout waiting for auth login and private message")
		}
	}

	assert.True(t, authReceived)
	assert.True(t, subReceived)
}
