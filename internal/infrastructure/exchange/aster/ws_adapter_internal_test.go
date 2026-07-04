package aster

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	pkgws "crypto-bot/pkg/ws"

	"github.com/stretchr/testify/assert"
)

func TestWsAdapterParseTicker(t *testing.T) {
	t.Parallel()
	adapter := NewWsAdapter("", "", "", "")

	// 1. Test Book Ticker Message
	bookMsg := []byte(`{
		"e": "bookTicker",
		"u": 400900217,
		"s": "BTCUSDT",
		"b": "25000.50",
		"B": "1.23",
		"a": "25001.50",
		"A": "2.45",
		"T": 1600000000000
	}`)
	sym, pd, err := adapter.ParseTicker(bookMsg)
	if err != nil {
		t.Fatalf("ParseTicker bookTicker failed: %v", err)
	}
	if sym != "BTCUSDT" || pd.BestBid != 25000.5 || pd.BestAsk != 25001.5 {
		t.Errorf("unexpected PriceData from bookTicker: %+v", pd)
	}

	// 2. Test Stats 24h Ticker Message
	statsMsg := []byte(`{
		"e": "24hrTicker",
		"E": 1600000001000,
		"s": "BTCUSDT",
		"c": "25002.50",
		"v": "100.50",
		"q": "2512500.00"
	}`)
	sym, pd, err = adapter.ParseTicker(statsMsg)
	if err != nil {
		t.Fatalf("ParseTicker 24hrTicker failed: %v", err)
	}
	if sym != "BTCUSDT" || pd.LastPrice != 25002.5 || pd.Volume24 != 100.5 {
		t.Errorf("unexpected PriceData from 24hrTicker: %+v", pd)
	}
}

func TestWsAdapterParsePosition(t *testing.T) {
	t.Parallel()
	adapter := NewWsAdapter("", "", "", "")

	posMsg := []byte(`{
		"e": "ACCOUNT_UPDATE",
		"E": 1600000000000,
		"a": {
			"m": "ORDER",
			"P": [
				{
					"s": "BTCUSDT",
					"pa": "1.5",
					"ep": "25000.00",
					"ps": "LONG",
					"mt": "isolated"
				}
			]
		}
	}`)

	update, err := adapter.ParsePosition(posMsg)
	if err != nil {
		t.Fatalf("ParsePosition failed: %v", err)
	}
	if update.Symbol != "BTCUSDT" || update.HoldVol != 1.5 || update.OpenAvgPrice != 25000.0 || update.PositionType != exchange.PositionTypeLong {
		t.Errorf("unexpected PersonalPositionUpdate: %+v", update)
	}

	// Short Position (pa should be negative in stream, absolute value in return)
	shortMsg := []byte(`{
		"e": "ACCOUNT_UPDATE",
		"E": 1600000000000,
		"a": {
			"m": "ORDER",
			"P": [
				{
					"s": "BTCUSDT",
					"pa": "-2.5",
					"ep": "26000.00",
					"ps": "SHORT",
					"mt": "isolated"
				}
			]
		}
	}`)
	update, err = adapter.ParsePosition(shortMsg)
	if err != nil {
		t.Fatalf("ParsePosition short failed: %v", err)
	}
	if update.HoldVol != 2.5 || update.PositionType != exchange.PositionTypeShort {
		t.Errorf("unexpected PersonalPositionUpdate for short: %+v", update)
	}
}

func TestWsAdapterParseOrder(t *testing.T) {
	t.Parallel()
	adapter := NewWsAdapter("", "", "", "")

	orderMsg := []byte(`{
		"e": "ORDER_TRADE_UPDATE",
		"E": 1600000000000,
		"o": {
			"s": "BTCUSDT",
			"c": "client-id-abc",
			"S": "BUY",
			"o": "LIMIT",
			"f": "GTC",
			"q": "1.5",
			"p": "25000",
			"ap": "25000",
			"X": "FILLED",
			"i": 123456789,
			"l": "1.5",
			"z": "1.5",
			"L": "25000",
			"ps": "LONG",
			"T": 1600000000000
		}
	}`)

	deal, err := adapter.ParseOrder(orderMsg)
	if err != nil {
		t.Fatalf("ParseOrder failed: %v", err)
	}
	if deal.Symbol != "BTCUSDT" || deal.OrderID.String() != "123456789" || deal.ExternalOID != "client-id-abc" || deal.Side != domain.SideOpenLong || deal.Price != 25000 || deal.Vol != 1.5 || deal.DealAvgPrice != 25000 || deal.DealVol != 1.5 || deal.State != domain.OrderStateFilled {
		t.Errorf("unexpected WsOrderDeal: %+v", deal)
	}
}

func TestWsAdapterGetPrivateURLFunc(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/listenKey") {
			_, _ = w.Write([]byte(`{"listenKey":"mock-listen-key"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "0x6E2435798939aE93815647457989381564745798", "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80", "0x6E2435798939aE93815647457989381564745798", config.LoggingConfig{})
	client.SetClock(mockClock{})

	adapter := NewWsAdapter("", "", "", "wss://fstream.asterdex.com/ws")
	adapter.SetClient(client)

	urlFunc := adapter.GetPrivateURLFunc(context.Background())
	url, err := urlFunc()
	if err != nil {
		t.Fatalf("GetPrivateURLFunc url resolution failed: %v", err)
	}

	expected := "wss://fstream.asterdex.com/ws/mock-listen-key"
	if url != expected {
		t.Errorf("expected private url: %s, got: %s", expected, url)
	}
}

func TestWsAdapterParsePosition_HedgeModeAndClose(t *testing.T) {
	t.Parallel()
	adapter := NewWsAdapter("", "", "", "")

	// 1. Hedge mode message with BOTH (size 0), LONG (size 0), SHORT (size -32)
	hedgeMsg := []byte(`{
		"e": "ACCOUNT_UPDATE",
		"E": 1783062000388,
		"a": {
			"m": "ORDER",
			"P": [
				{"s":"SLXUSDT","pa":"0","ep":"0.00000000","ps":"BOTH"},
				{"s":"SLXUSDT","pa":"0","ep":"0.00000000","ps":"LONG"},
				{"s":"SLXUSDT","pa":"-32","ep":"0.46041000","ps":"SHORT"}
			]
		}
	}`)

	update, err := adapter.ParsePosition(hedgeMsg)
	if err != nil {
		t.Fatalf("ParsePosition failed: %v", err)
	}
	if update.Symbol != "SLXUSDT" || update.HoldVol != 0 || update.PositionType != exchange.PositionTypeUnknown {
		t.Errorf("unexpected hedge mode position update: %+v", update)
	}

	// 2. Position closed (size 0) for SHORT
	closeMsg := []byte(`{
		"e": "ACCOUNT_UPDATE",
		"E": 1783062000388,
		"a": {
			"m": "ORDER",
			"P": [
				{"s":"SLXUSDT","pa":"0","ep":"0.00000000","ps":"BOTH"},
				{"s":"SLXUSDT","pa":"0","ep":"0.00000000","ps":"SHORT"}
			]
		}
	}`)

	update, err = adapter.ParsePosition(closeMsg)
	if err != nil {
		t.Fatalf("ParsePosition failed: %v", err)
	}
	if update.Symbol != "SLXUSDT" || update.HoldVol != 0 || update.PositionType != exchange.PositionTypeUnknown {
		t.Errorf("unexpected closed position update: %+v", update)
	}
}

func TestWsAdapterGetChannelExtractor(t *testing.T) {
	t.Parallel()
	adapter := NewWsAdapter("", "", "", "")
	extractor := adapter.GetChannelExtractor()

	payload := []byte(`{"e":"ACCOUNT_UPDATE","T":1783094748050,"E":1783094748075,"a":{"B":[{"a":"USDT","wb":"14.97792622","cw":"14.97792622","bc":"0"}],"P":[{"s":"SLXUSDT","pa":"0","ep":"0.00000000","cr":"0","up":"0","mt":"isolated","iw":"0","ps":"BOTH","ma":"USDT"},{"s":"SLXUSDT","pa":"0","ep":"0.00000000","cr":"0","up":"0","mt":"isolated","iw":"0","ps":"LONG","ma":"USDT"},{"s":"SLXUSDT","pa":"0","ep":"0.00000000","cr":"0.01334000","up":"0","mt":"isolated","iw":"0","ps":"SHORT","ma":"USDT"}],"m":"ORDER"}}`)

	channel := extractor(payload)
	t.Logf("channel value is: '%s'", channel)
	if channel != channelPersonalPosition {
		t.Errorf("expected channel '%s', got '%s'", channelPersonalPosition, channel)
	}
}

func TestWsAdapterParsePosition_AccountUpdate_Closed(t *testing.T) {
	t.Parallel()
	adapter := NewWsAdapter("", "", "", "")

	payload := []byte(`{"e":"ACCOUNT_UPDATE","T":1783094748050,"E":1783094748075,"a":{"B":[{"a":"USDT","wb":"14.97792622","cw":"14.97792622","bc":"0"}],"P":[{"s":"SLXUSDT","pa":"0","ep":"0.00000000","cr":"0","up":"0","mt":"isolated","iw":"0","ps":"BOTH","ma":"USDT"},{"s":"SLXUSDT","pa":"0","ep":"0.00000000","cr":"0","up":"0","mt":"isolated","iw":"0","ps":"LONG","ma":"USDT"},{"s":"SLXUSDT","pa":"0","ep":"0.00000000","cr":"0.01334000","up":"0","mt":"isolated","iw":"0","ps":"SHORT","ma":"USDT"}],"m":"ORDER"}}`)

	update, err := adapter.ParsePosition(payload)
	if err != nil {
		t.Fatalf("ParsePosition failed: %v", err)
	}
	if update == nil {
		t.Fatalf("expected non-nil position update")
	}

	if update.Symbol != "SLXUSDT" {
		t.Errorf("expected symbol SLXUSDT, got %s", update.Symbol)
	}
	if update.HoldVol != 0 {
		t.Errorf("expected hold volume 0, got %f", update.HoldVol)
	}
}

func TestWsAdapterRemainingMethods(t *testing.T) {
	t.Parallel()

	adapter := NewWsAdapter("", "", "", "")

	// 1. SetPool & SetClock
	pool := pkgws.NewPool("ws://dummy", 30, slog.Default())
	adapter.SetPool(pool)
	adapter.SetClock(nil)

	// 2. SubscribeTicker & UnsubscribeTicker & SubscribePersonal
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = adapter.SubscribeTicker(ctx, "BTCUSDT")
	_ = adapter.UnsubscribeTicker(ctx, "BTCUSDT")
	_ = adapter.SubscribePersonal(ctx)

	// 3. GetPingConfig & GetCustomPingHandler
	ping, interval := adapter.GetPingConfig()
	assert.Nil(t, ping)
	assert.Equal(t, time.Duration(0), interval)

	pingHandler := adapter.GetCustomPingHandler()
	assert.NotNil(t, pingHandler)

	// 4. GetAuthHook & HandshakeHeaders
	hook := adapter.GetAuthHook("key", "secret")
	assert.Nil(t, hook)

	headers, err := adapter.HandshakeHeaders()
	assert.NoError(t, err)
	assert.NotNil(t, headers)
}
