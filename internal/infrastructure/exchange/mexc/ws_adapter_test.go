package mexc_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/exchange/mexc"
	pkgws "crypto-bot/pkg/ws"

	"github.com/stretchr/testify/assert"
)

// ── WsAdapter — GetPingConfig ────────────────────────────────────────.

func TestWsAdapter_GetPingConfig(t *testing.T) {
	t.Parallel()
	a := mexc.NewWsAdapter()
	payload, interval := a.GetPingConfig()

	if payload == nil {
		t.Fatal("ping payload should not be nil")
	}
	if interval != 15*time.Second {
		t.Errorf("interval: want 15s, got %v", interval)
	}

	m, ok := payload.(map[string]string)
	if !ok {
		t.Fatalf("payload type: want map[string]string, got %T", payload)
	}
	if m["method"] != "ping" {
		t.Errorf("method: want 'ping', got %q", m["method"])
	}
}

// ── WsAdapter — GetAuthHook ──────────────────────────────────────────.

func TestWsAdapter_GetAuthHook_Empty(t *testing.T) {
	t.Parallel()
	a := mexc.NewWsAdapter()
	hook := a.GetAuthHook("", "secret")
	if hook != nil {
		t.Error("hook should be nil for empty apiKey")
	}
}

func TestWsAdapter_GetAuthHook_NonEmpty(t *testing.T) {
	t.Parallel()
	a := mexc.NewWsAdapter()
	hook := a.GetAuthHook("key", "secret")
	if hook == nil {
		t.Fatal("hook should not be nil for non-empty apiKey")
	}

	// Execute the hook to cover the logic (SendJSON handles uninitialized client safely)
	client := pkgws.NewClient("wss://dummy", slog.Default())
	hook(client)
}

// ── WsAdapter — GetChannelExtractor ──────────────────────────────────.

func TestWsAdapter_GetChannelExtractor(t *testing.T) {
	t.Parallel()
	a := mexc.NewWsAdapter()
	extractor := a.GetChannelExtractor()
	if extractor == nil {
		t.Fatal("extractor should not be nil")
	}

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"ticker", `{"channel":"push.ticker"}`, "ticker"},
		{"depth full", `{"channel":"push.depth.full"}`, "depth"},
		{"depth step", `{"channel":"push.depth.step"}`, "depth"},
		{"kline", `{"channel":"push.kline"}`, "kline"},
		{"personal order", `{"channel":"push.personal.order"}`, "personal.order"},
		{"personal order deal", `{"channel":"push.personal.order.deal"}`, "personal.order.deal"},
		{"personal track order", `{"channel":"push.personal.track.order"}`, "personal.track.order"},
		{"personal position", `{"channel":"push.personal.position"}`, "personal.position"},
		{"generic push", `{"channel":"push.something"}`, "something"},
		{"non-push", `{"channel":"pong"}`, "pong"},
		{"empty", `{"channel":""}`, ""},
		{"invalid json", `not json`, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractor([]byte(tt.input))
			if got != tt.want {
				t.Errorf("want %q, got %q", tt.want, got)
			}
		})
	}
}

// ── WsAdapter — ParseTicker ─────────────────────────────────────────.

func TestWsAdapter_ParseTicker(t *testing.T) {
	t.Parallel()
	a := mexc.NewWsAdapter()

	input := `{"channel":"push.ticker","symbol":"BTC_USDT","data":{"symbol":"BTC_USDT","lastPrice":50000,"fairPrice":49999,"indexPrice":50001,"volume24":1000,"amount24":50000000,"maxBidPrice":51000,"minAskPrice":49000,"timestamp":1609459200000,"bid1":49999,"ask1":50001}}`

	sym, pd, err := a.ParseTicker([]byte(input))
	if err != nil {
		t.Fatalf("ParseTicker failed: %v", err)
	}
	if sym != "BTC_USDT" {
		t.Errorf("symbol: want BTC_USDT, got %s", sym)
	}
	if pd.LastPrice != 50000 {
		t.Errorf("LastPrice: want 50000, got %f", pd.LastPrice)
	}
	if pd.BestBid != 49999 {
		t.Errorf("BestBid: want 49999, got %f", pd.BestBid)
	}
	if pd.BestAsk != 50001 {
		t.Errorf("BestAsk: want 50001, got %f", pd.BestAsk)
	}
}

func TestWsAdapter_ParseTicker_FallbackPrices(t *testing.T) {
	t.Parallel()
	a := mexc.NewWsAdapter()

	// bid1 and ask1 are 0, should fall back to maxBidPrice and minAskPrice.
	input := `{"channel":"push.ticker","symbol":"BTC_USDT","data":{"symbol":"BTC_USDT","lastPrice":50000,"maxBidPrice":51000,"minAskPrice":49000,"bid1":0,"ask1":0}}`

	_, pd, err := a.ParseTicker([]byte(input))
	if err != nil {
		t.Fatalf("ParseTicker failed: %v", err)
	}
	if pd.BestBid != 51000 {
		t.Errorf("BestBid fallback: want 51000, got %f", pd.BestBid)
	}
	if pd.BestAsk != 49000 {
		t.Errorf("BestAsk fallback: want 49000, got %f", pd.BestAsk)
	}
}

func TestWsAdapter_ParseTicker_InvalidJSON(t *testing.T) {
	t.Parallel()
	a := mexc.NewWsAdapter()
	_, _, err := a.ParseTicker([]byte(`invalid`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestWsAdapter_ParseFuturePersonalPositionSpec(t *testing.T) {
	t.Parallel()
	a := mexc.NewWsAdapter()

	positionRaw := readFutureWSSpec(t, "position.json")
	position, err := a.ParsePosition(positionRaw)
	if err != nil {
		t.Fatalf("ParsePosition spec failed: %v", err)
	}
	if position.Symbol != "BTC_USDT" || position.HoldVol != 10 {
		t.Fatalf("unexpected position parse: symbol=%s hold=%v", position.Symbol, position.HoldVol)
	}
}

func readFutureWSSpec(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "..", "..", "specs", "mexc", "ws", "future", "private", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read spec %s: %v", name, err)
	}
	return data
}

func TestWsAdapter_LoginSync(t *testing.T) {
	t.Parallel()

	t.Run("Success Login Closes Authenticated Channel", func(t *testing.T) {
		t.Parallel()
		adapter := mexc.NewWsAdapter()
		extractor := adapter.GetChannelExtractor()

		// GetAuthHook with key returns non-nil hook
		hook := adapter.GetAuthHook("key", "secret")
		assert.NotNil(t, hook)

		// Before running hook or receiving login event, SubscribePersonal with short timeout context should timeout
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		err := adapter.SubscribePersonal(ctx)
		cancel()
		assert.ErrorIs(t, err, context.DeadlineExceeded)

		// Simulate receiving a successful login event response from MEXC
		loginResp := []byte(`{"channel":"rs.login","data":"success"}`)
		channel := extractor(loginResp)
		assert.Equal(t, "login", channel)

		// Now SubscribePersonal should unblock instantly even with an active context
		ctx2, cancel2 := context.WithCancel(context.Background())
		// Prepare a mock private client to avoid panic during SendPrivate
		pool := pkgws.NewPool("ws://127.0.0.1:1", 1, nil)
		adapter.SetPool(pool)

		err = adapter.SubscribePersonal(ctx2)
		cancel2()
		// Since the private client is nil in the pool, it will return nil (success/noop) instead of blocking
		assert.NoError(t, err)
	})

	t.Run("Empty APIKey Closes Authenticated Channel Immediately", func(t *testing.T) {
		t.Parallel()
		adapter := mexc.NewWsAdapter()

		hook := adapter.GetAuthHook("", "")
		assert.Nil(t, hook)

		// Since apiKey is empty, a.authenticated should be closed immediately
		ctx, cancel := context.WithCancel(context.Background())
		pool := pkgws.NewPool("ws://127.0.0.1:1", 1, nil)
		adapter.SetPool(pool)

		err := adapter.SubscribePersonal(ctx)
		cancel()
		assert.NoError(t, err)
	})
}
