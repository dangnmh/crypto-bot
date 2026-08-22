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
		{"depth incremental", `{"channel":"push.depth"}`, "depth"},
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
	if position.Symbol != "BTC_USDT" || position.HoldVolContract != 10 {
		t.Fatalf("unexpected position parse: symbol=%s hold=%v", position.Symbol, position.HoldVolContract)
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

func TestWsAdapter_ParseDepth(t *testing.T) {
	t.Parallel()

	adapter := mexc.NewWsAdapter()

	raw := []byte(`{
		"channel": "push.depth.full",
		"symbol": "BTC_USDT",
		"data": {
			"bids": [[60000.5, 1, 1.2], [60000.0, 1, 3.4]],
			"asks": [[60001.0, 1, 0.5], [60001.5, 1, 2.1]],
			"version": 123456
		},
		"ts": 1600000000000
	}`)

	sym, ob, err := adapter.ParseDepth(raw)
	assert.NoError(t, err)
	assert.Equal(t, "BTC_USDT", sym)
	assert.NotNil(t, ob)
	assert.Equal(t, int64(123456), ob.Version)
	assert.Len(t, ob.Bids, 2)
	assert.Equal(t, 60000.5, ob.Bids[0].Price)
	assert.Equal(t, 1.2, ob.Bids[0].Volume)
	assert.Len(t, ob.Asks, 2)
	assert.Equal(t, 60001.0, ob.Asks[0].Price)
	assert.Equal(t, 0.5, ob.Asks[0].Volume)

	// String format numbers
	rawStrings := []byte(`{
		"channel": "push.depth.full",
		"symbol": "ETH_USDT",
		"data": {
			"bids": [["3000.5", "10.0"]],
			"asks": [["3001.0", "5.0"]],
			"version": 7890
		}
	}`)
	sym2, ob2, err := adapter.ParseDepth(rawStrings)
	assert.NoError(t, err)
	assert.Equal(t, "ETH_USDT", sym2)
	assert.NotNil(t, ob2)
	assert.Equal(t, int64(7890), ob2.Version)
	assert.Len(t, ob2.Bids, 1)

	// Delta deletion message with volume 0
	rawZeroVol := []byte(`{
		"channel": "push.depth.step",
		"symbol": "SOL_USDT",
		"data": {
			"bids": [["150.0", "0.0"]],
			"asks": [["151.0", "0.0"]],
			"version": 9999
		}
	}`)
	sym3, ob3, err := adapter.ParseDepth(rawZeroVol)
	assert.NoError(t, err)
	assert.Equal(t, "SOL_USDT", sym3)
	assert.NotNil(t, ob3)
	assert.Len(t, ob3.Bids, 1)
	assert.Equal(t, 150.0, ob3.Bids[0].Price)
	assert.Equal(t, 0.0, ob3.Bids[0].Volume)
	assert.Len(t, ob3.Asks, 1)
	assert.Equal(t, 151.0, ob3.Asks[0].Price)
	assert.Equal(t, 0.0, ob3.Asks[0].Volume)

	// 3-element format [price, orderCount, quantity] with begin and end versions
	raw3Elem := []byte(`{
		"channel": "push.depth",
		"symbol": "BTC_USDT",
		"data": {
			"begin": 40949478001,
			"end": 40949478038,
			"version": 40949478038,
			"cts": 1787301607692,
			"asks": [
				[77515.4, 16284, 2.5],
				[77513.9, 0, 0]
			],
			"bids": [
				[77506.4, 5969, 5.0]
			]
		},
		"ts": 1787301607702
	}`)
	sym4, ob4, err := adapter.ParseDepth(raw3Elem)
	assert.NoError(t, err)
	assert.Equal(t, "BTC_USDT", sym4)
	assert.NotNil(t, ob4)
	assert.Equal(t, int64(40949478001), ob4.FirstVersion)
	assert.Equal(t, int64(40949478038), ob4.Version)
	assert.Len(t, ob4.Bids, 1)
	assert.Equal(t, 77506.4, ob4.Bids[0].Price)
	assert.Equal(t, 5.0, ob4.Bids[0].Volume) // Quantity at index 2, not order count (5969)
	assert.Len(t, ob4.Asks, 2)
	assert.Equal(t, 77515.4, ob4.Asks[0].Price)
	assert.Equal(t, 2.5, ob4.Asks[0].Volume) // Quantity at index 2, not order count (16284)
	assert.Equal(t, 77513.9, ob4.Asks[1].Price)
	assert.Equal(t, 0.0, ob4.Asks[1].Volume) // Deletion preserved

	// Invalid json
	_, _, err = adapter.ParseDepth([]byte(`invalid`))
	assert.Error(t, err)
}

func TestWsAdapter_DepthSubscriptions(t *testing.T) {
	t.Parallel()

	adapter := mexc.NewWsAdapter()
	pool := pkgws.NewPool("ws://127.0.0.1:1", 30, slog.Default())
	adapter.SetPool(pool)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_ = adapter.SubscribeDepth(ctx, "BTC_USDT")
	_ = adapter.UnsubscribeDepth(ctx, "BTC_USDT")
}
