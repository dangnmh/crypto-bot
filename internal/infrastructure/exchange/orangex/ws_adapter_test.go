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
	if hook == nil {
		t.Error("expected auth hook to be non-nil")
	}
	hookNil := adapter.GetAuthHook("", "")
	if hookNil != nil {
		t.Error("expected empty auth hook to be nil")
	}

	// ParseOrder
	orderDeal, err := adapter.ParseOrder(nil)
	if orderDeal != nil || err != nil {
		t.Error("expected ParseOrder to return nil, nil")
	}

	extractor := adapter.GetChannelExtractor()
	ch := extractor([]byte(`{"method":"subscription","params":{"channel":"ticker.BTC-USDT-PERPETUAL.raw"}}`))
	if ch != "ticker" {
		t.Errorf("expected ticker, got %s", ch)
	}

	chReal := extractor([]byte(`{"params":{"data": {"timestamp":"1782806213805", "stats":{"volume":"2931979","price_change":"-0.0747","price_change_1h":"-0.0501","price_change_7d":"-0.2069","price_change_30d":"0.4898","low":"12.76042","turnover":"42342400.98560","high":"15.42788"},"state":"open","last_price":"13.00535","instrument_name":"LAB-USDT-PERPETUAL","best_bid_price":13.00385,"best_bid_amount":1064,"best_ask_price":13.00585,"best_ask_amount":1300,"mark_price":"13.00435","underlying_price":"14.13951","open_interest":"1086554"},"channel":"ticker.LAB-USDT-PERPETUAL.raw"},"method":"subscription","jsonrpc":"2.0"}`))
	if chReal != "ticker" {
		t.Errorf("expected ticker, got %s", chReal)
	}

	chPersonal := extractor([]byte(`{"method":"subscription","params":{"channel":"user.changes.perpetual.PERPETUAL.raw"}}`))
	if chPersonal != "personal.position" {
		t.Errorf("expected personal.position, got %s", chPersonal)
	}

	chPersonalReal := extractor([]byte(`{"jsonrpc":"2.0","method":"subscription","params":{"channel":"user.changes.perpetual.PERPETUAL.raw","data":{"orders":[{"instrId":1375,"currency":"PERPETUAL","kind":"perpetual","direction":"sell","amount":"47","price":"0.521","advanced":"usdt","source":"api","mmp":false,"rpl":"0","version":1,"leverage":5,"marginType":"isolate","perpetualOrderType":"LIMIT","fee":"0","feeReal":"0","feeBonus":"0","feeCoupon":"0","feeActual":"0","reverseOpen":false,"reverseNewOrder":false,"posFullTpsl":false,"order_id":"817885507985297408","custom_order_id":"","order_state":"canceled","instrument_name":"SLX-USDT-PERPETUAL","show_name":"SLXUSDT","filled_amount":"0","average_price":"0","order_type":"limit","time_in_force":"GTC","post_only":false,"reduce_only":false,"condition_type":"NORMAL","trigger_touch":false,"trigger_price_type":1,"stop_loss_price":"0.5357","stop_loss_type":2,"take_profit_price":"0.52","take_profit_type":2,"creation_timestamp":1782829500692,"last_update_timestamp":1782829500692,"show_zero_rpl":false,"cascade_type":0,"first_deal_time":0,"last_deal_time":0,"position_side":"BOTH"}],"positions":[],"trades":[],"instrument_name":"SLX-USDT-PERPETUAL"}}}`))
	if chPersonalReal != "personal.position" {
		t.Errorf("expected personal.position, got %s", chPersonalReal)
	}

	chEmpty := extractor([]byte(`invalid`))
	if chEmpty != "" {
		t.Errorf("expected empty string for invalid json, got %s", chEmpty)
	}

	chAuth := extractor([]byte(`{"id":1000,"jsonrpc":"2.0","result":{"access_token":"mock_tok"}}`))
	if chAuth != "auth" {
		t.Errorf("expected auth, got %s", chAuth)
	}

	chAuthErr := extractor([]byte(`{"id":1000,"jsonrpc":"2.0","error":{"code":10000,"message":"failed"}}`))
	if chAuthErr != "auth" {
		t.Errorf("expected auth, got %s", chAuthErr)
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

	// Test actual server payload
	realMsg := []byte(`{"params":{"data": {"timestamp":"1782806213805", "stats":{"volume":"2931979","price_change":"-0.0747","price_change_1h":"-0.0501","price_change_7d":"-0.2069","price_change_30d":"0.4898","low":"12.76042","turnover":"42342400.98560","high":"15.42788"},"state":"open","last_price":"13.00535","instrument_name":"LAB-USDT-PERPETUAL","best_bid_price":13.00385,"best_bid_amount":1064,"best_ask_price":13.00585,"best_ask_amount":1300,"mark_price":"13.00435","underlying_price":"14.13951","open_interest":"1086554"},"channel":"ticker.LAB-USDT-PERPETUAL.raw"},"method":"subscription","jsonrpc":"2.0"}`)
	symReal, pdataReal, errReal := adapter.ParseTicker(realMsg)
	if errReal != nil {
		t.Fatalf("failed to parse real server ticker payload: %v", errReal)
	}
	if symReal != "LAB-USDT-PERPETUAL" {
		t.Errorf("expected LAB-USDT-PERPETUAL, got %s", symReal)
	}
	if pdataReal.LastPrice != 13.00535 {
		t.Errorf("expected 13.00535, got %f", pdataReal.LastPrice)
	}
	if pdataReal.BestBid != 13.00385 {
		t.Errorf("expected 13.00385, got %f", pdataReal.BestBid)
	}
	if pdataReal.BestAsk != 13.00585 {
		t.Errorf("expected 13.00585, got %f", pdataReal.BestAsk)
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
