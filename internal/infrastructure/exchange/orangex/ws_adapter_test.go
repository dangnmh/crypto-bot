package orangex_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/orangex"
	pkgws "crypto-bot/pkg/ws"
)

func TestWsAdapter_ConfigAndExtraction(t *testing.T) {
	t.Parallel()
	adapter := orangex.NewWsAdapter(nil)

	// Ping config
	pingPayload, pingInt := adapter.GetPingConfig()
	if pingPayload != "PING" || pingInt != 5*time.Second {
		t.Errorf("unexpected ping config: %v %v", pingPayload, pingInt)
	}

	// Auth hook
	hook := adapter.GetAuthHook("key", "secret")
	if hook != nil {
		t.Error("expected auth hook to be nil")
	}

	// ParseOrder
	orderDeal, err := adapter.ParseOrder(nil)
	if orderDeal != nil || err != nil {
		t.Error("expected ParseOrder to return nil, nil")
	}

	extractor := adapter.GetChannelExtractor()
	ch := extractor([]byte(`{"method":"subscription","params":{"channel":"ticker.BTC-USDT-PERPETUAL.raw"}}`))
	if ch != "ticker.BTC-USDT-PERPETUAL.raw" {
		t.Errorf("expected ticker.BTC-USDT-PERPETUAL.raw, got %s", ch)
	}

	chEmpty := extractor([]byte(`invalid`))
	if chEmpty != "" {
		t.Errorf("expected empty string for invalid json, got %s", chEmpty)
	}
}

func TestWsAdapter_ParseTicker(t *testing.T) {
	t.Parallel()
	adapter := orangex.NewWsAdapter(nil)

	// ParseTicker
	msg := []byte(`{"method":"subscription","params":{"channel":"ticker.BTC-USDT-PERPETUAL.raw","data":{"instrument_name":"BTC-USDT-PERPETUAL","best_bid_price":"60000","best_ask_price":"60005","last_price":"60002","stats":{"volume":"123.45"}}}}`)
	sym, pdata, err := adapter.ParseTicker(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sym != "BTC-USDT-PERPETUAL" {
		t.Errorf("expected BTC-USDT-PERPETUAL, got %s", sym)
	}
	if pdata.LastPrice != 60002.0 {
		t.Errorf("expected 60002.0, got %f", pdata.LastPrice)
	}

	// ParseTicker errors
	_, _, err = adapter.ParseTicker([]byte(`invalid`))
	if err == nil {
		t.Error("expected error for invalid json")
	}
	_, _, err = adapter.ParseTicker([]byte(`{"method":"not_subscription"}`))
	if err == nil {
		t.Error("expected error for non-subscription method")
	}
}

func TestWsAdapter_ParsePosition(t *testing.T) {
	t.Parallel()
	adapter := orangex.NewWsAdapter(nil)

	// ParsePosition
	posMsg := []byte(`{"method":"subscription","params":{"channel":"user.changes.perpetual.PERPETUAL.raw","data":{"positions":[{"instrument_name":"BTC-USDT-PERPETUAL","side":"buy","size":"1.5","entry_price":"59000"}]}}}`)
	update, err := adapter.ParsePosition(posMsg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if update.Symbol != "BTC-USDT-PERPETUAL" || update.HoldVol != 1.5 || update.HoldAvgPrice != 59000 || update.PositionType != exchange.PositionTypeLong {
		t.Errorf("unexpected position update values: %+v", update)
	}

	// ParsePosition errors
	_, err = adapter.ParsePosition([]byte(`invalid`))
	if err == nil {
		t.Error("expected error for invalid json")
	}
	posEmpty, err := adapter.ParsePosition([]byte(`{"method":"not_subscription"}`))
	if err != nil || posEmpty != nil {
		t.Errorf("expected nil result for non-subscription position message, got: %v", posEmpty)
	}
}

func TestWsAdapter_SubscribeAndUnsubscribe(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/public/auth" {
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":{"access_token":"mock_tok","expires_in":3600}}`))
		}
	}))
	defer srv.Close()

	client := orangex.NewClient(nil, srv.URL, "key", "secret", config.LoggingConfig{})
	adapter := orangex.NewWsAdapter(client)
	pool := pkgws.NewPool("ws://dummy", 30, slog.Default())
	adapter.SetPool(pool)

	ctx := context.Background()
	cancelledCtx, cancel := context.WithCancel(ctx)
	cancel()

	err := adapter.SubscribeTicker(cancelledCtx, "BTC-USDT-PERPETUAL")
	if err == nil {
		t.Error("expected error for cancelled context")
	}

	err = adapter.UnsubscribeTicker(cancelledCtx, "BTC-USDT-PERPETUAL")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	err = adapter.SubscribePersonal(ctx)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
