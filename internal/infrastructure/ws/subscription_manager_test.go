package ws_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/ws"
	pkgws "crypto-bot/pkg/ws"

	"github.com/gorilla/websocket"
)

func TestSubscriptionManager_ReferenceCounting(t *testing.T) {
	t.Parallel()

	// Test nil pool returns error
	if _, err := ws.NewSubscriptionManager(nil); err == nil {
		t.Errorf("expected error from NewSubscriptionManager(nil)")
	}

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	pool := pkgws.NewPool(wsURL, 30, slog.Default())
	mgr, err := ws.NewSubscriptionManager(pool)
	if err != nil {
		t.Fatalf("NewSubscriptionManager failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	topic := "ticker:BTCUSDT"

	// Flow 1 subscribes
	if err := mgr.Subscribe(ctx, topic, "flow1", map[string]string{"op": "sub"}); err != nil {
		t.Fatalf("flow1 subscribe failed: %v", err)
	}

	if mgr.SubscriberCount(topic) != 1 {
		t.Errorf("expected subscriber count 1, got %d", mgr.SubscriberCount(topic))
	}

	// Flow 2 subscribes to same topic
	if err := mgr.Subscribe(ctx, topic, "flow2", map[string]string{"op": "sub"}); err != nil {
		t.Fatalf("flow2 subscribe failed: %v", err)
	}

	if mgr.SubscriberCount(topic) != 2 {
		t.Errorf("expected subscriber count 2, got %d", mgr.SubscriberCount(topic))
	}

	// Flow 1 unsubscribes -> topic should remain active for Flow 2
	if err := mgr.Unsubscribe(ctx, topic, "flow1", map[string]string{"op": "unsub"}); err != nil {
		t.Fatalf("flow1 unsubscribe failed: %v", err)
	}

	if mgr.SubscriberCount(topic) != 1 {
		t.Errorf("expected subscriber count 1 after flow1 unsub, got %d", mgr.SubscriberCount(topic))
	}

	// Flow 2 unsubscribes -> subscriber count reaches 0
	if err := mgr.Unsubscribe(ctx, topic, "flow2", map[string]string{"op": "unsub"}); err != nil {
		t.Fatalf("flow2 unsubscribe failed: %v", err)
	}

	if mgr.SubscriberCount(topic) != 0 {
		t.Errorf("expected subscriber count 0 after flow2 unsub, got %d", mgr.SubscriberCount(topic))
	}
}
