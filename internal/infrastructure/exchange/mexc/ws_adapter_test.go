package mexc_test

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/mexc"
	pkgws "crypto-bot/pkg/ws"
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

// ── WsAdapter — ParseDepth ──────────────────────────────────────────.

func TestWsAdapter_ParseDepth(t *testing.T) {
	t.Parallel()
	a := mexc.NewWsAdapter()

	input := `{"channel":"push.depth.full","symbol":"BTC_USDT","data":{"version":1,"asks":[[50001,10],[50002,20]],"bids":[[49999,15],[49998,25]]}}`

	sym, ob, err := a.ParseDepth([]byte(input))
	if err != nil {
		t.Fatalf("ParseDepth failed: %v", err)
	}
	if sym != "BTC_USDT" {
		t.Errorf("symbol: want BTC_USDT, got %s", sym)
	}
	if ob.Version != 1 {
		t.Errorf("version: want 1, got %d", ob.Version)
	}
	if len(ob.Asks) != 2 {
		t.Errorf("asks: want 2, got %d", len(ob.Asks))
	}
	if len(ob.Bids) != 2 {
		t.Errorf("bids: want 2, got %d", len(ob.Bids))
	}
	if ob.Asks[0].Price != 50001 {
		t.Errorf("ask[0] price: want 50001, got %f", ob.Asks[0].Price)
	}
	if ob.Bids[0].Volume != 15 {
		t.Errorf("bid[0] volume: want 15, got %f", ob.Bids[0].Volume)
	}
}

func TestWsAdapter_ParseDepth_InvalidJSON(t *testing.T) {
	t.Parallel()
	a := mexc.NewWsAdapter()
	_, _, err := a.ParseDepth([]byte(`{}`))
	if err == nil {
		t.Fatal("expected error for missing symbol")
	}
}

// ── WsAdapter — ParseKline ──────────────────────────────────────────.

func TestWsAdapter_ParseKline(t *testing.T) {
	t.Parallel()
	a := mexc.NewWsAdapter()

	input := `{"channel":"push.kline","symbol":"ETH_USDT","data":{"t":1609459200,"o":3000,"h":3100,"l":2900,"c":3050,"v":500,"a":1500000}}`

	sym, k, err := a.ParseKline([]byte(input))
	if err != nil {
		t.Fatalf("ParseKline failed: %v", err)
	}
	if sym != "ETH_USDT" {
		t.Errorf("symbol: want ETH_USDT, got %s", sym)
	}
	if k.Open != 3000 {
		t.Errorf("open: want 3000, got %f", k.Open)
	}
	if k.Timestamp != 1609459200000 {
		t.Errorf("timestamp: want 1609459200000, got %d", k.Timestamp)
	}
}

func TestWsAdapter_ParseKline_InvalidJSON(t *testing.T) {
	t.Parallel()
	a := mexc.NewWsAdapter()
	_, _, err := a.ParseKline([]byte(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// ── WsAdapter — ParseOrder ──────────────────────────────────────────.

func TestWsAdapter_ParseOrder(t *testing.T) {
	t.Parallel()
	a := mexc.NewWsAdapter()

	deal := exchange.WsOrderDeal{
		Symbol:       "BTC_USDT",
		OrderID:      "12345",
		DealAvgPrice: 50000,
		DealVol:      1,
		State:        3,
	}
	inner, _ := json.Marshal(deal)
	input, _ := json.Marshal(map[string]json.RawMessage{"data": inner})

	order, err := a.ParseOrder(input)
	if err != nil {
		t.Fatalf("ParseOrder failed: %v", err)
	}
	if order.Symbol != "BTC_USDT" {
		t.Errorf("symbol: want BTC_USDT, got %s", order.Symbol)
	}
	if order.DealAvgPrice != 50000 {
		t.Errorf("DealAvgPrice: want 50000, got %f", order.DealAvgPrice)
	}
}

func TestWsAdapter_ParseOrder_InvalidJSON(t *testing.T) {
	t.Parallel()
	a := mexc.NewWsAdapter()
	_, err := a.ParseOrder([]byte(`invalid`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestWsAdapter_ParseFuturePersonalOrderSpec(t *testing.T) {
	t.Parallel()
	a := mexc.NewWsAdapter()

	orderRaw := readFutureWSSpec(t, "order.json")
	order, err := a.ParseOrder(orderRaw)
	if err != nil {
		t.Fatalf("ParseOrder spec failed: %v", err)
	}
	if order.GetOrderID() != "123456789" || order.Symbol != "BTC_USDT" || order.RemainVol != 5 {
		t.Fatalf("unexpected order parse: id=%s symbol=%s remain=%v", order.GetOrderID(), order.Symbol, order.RemainVol)
	}
}

func TestWsAdapter_ParseFuturePersonalOrderDealSpec(t *testing.T) {
	t.Parallel()
	a := mexc.NewWsAdapter()

	dealRaw := readFutureWSSpec(t, "fill.json")
	deal, err := a.ParseOrderDeal(dealRaw)
	if err != nil {
		t.Fatalf("ParseOrderDeal spec failed: %v", err)
	}
	if deal.GetOrderID() != "123456789" || deal.Vol != 10 || deal.Price != 45000.5 {
		t.Fatalf("unexpected deal parse: id=%s vol=%v price=%v", deal.GetOrderID(), deal.Vol, deal.Price)
	}
}

func TestWsAdapter_ParseFuturePersonalTrackOrderSpec(t *testing.T) {
	t.Parallel()
	a := mexc.NewWsAdapter()

	trackRaw := readFutureWSSpec(t, "track.json")
	track, err := a.ParseTrackOrder(trackRaw)
	if err != nil {
		t.Fatalf("ParseTrackOrder spec failed: %v", err)
	}
	if track.GetID() != "987654321" || track.GetOrderID() != "123456789" || track.BackValue != 0.5 {
		t.Fatalf("unexpected track parse: id=%s order=%s back=%v", track.GetID(), track.GetOrderID(), track.BackValue)
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
