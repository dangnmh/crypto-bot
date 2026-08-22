package toobit_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/toobit"
	pkgws "crypto-bot/pkg/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockClock struct {
	now time.Time
}

func (m mockClock) Now() time.Time {
	return m.now
}

func TestWsAdapter_PublicLifecycle(t *testing.T) {
	t.Parallel()

	adapter := toobit.NewWsAdapter("wss://stream.toobit.com")
	pool := pkgws.NewPool("ws://127.0.0.1:1", 30, slog.Default())
	adapter.SetPool(pool)

	// Ping config
	ping, interval := adapter.GetPingConfig()
	assert.NotNil(t, ping)
	assert.Equal(t, 30*time.Second, interval)

	// Auth hook
	hook := adapter.GetAuthHook("key", "secret")
	assert.Nil(t, hook)

	// Channel extractor
	extractor := adapter.GetChannelExtractor()
	require.NotNil(t, extractor)

	assert.Equal(t, "ping", extractor([]byte(`{"ping": 12345}`)))
	assert.Equal(t, "pong", extractor([]byte(`{"pong": 12345}`)))
	assert.Equal(t, "ticker", extractor([]byte(`{"topic": "bookTicker", "symbol": "BTC-SWAP-USDT"}`)))
	assert.Equal(t, "ticker", extractor([]byte(`{"topic": "realtimes", "symbol": "BTC-SWAP-USDT"}`)))
	assert.Equal(t, "personal.position", extractor([]byte(`{"event": "outboundContractPositionInfo"}`)))
	assert.Equal(t, "personal.position", extractor([]byte(`[{"e": "outboundContractPositionInfo"}]`)))
	assert.Equal(t, "", extractor([]byte(`{"topic": "unknown"}`)))
}

func TestWsAdapter_ParseTicker(t *testing.T) {
	t.Parallel()

	clk := mockClock{now: time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)}
	adapter := toobit.NewWsAdapter("wss://stream.toobit.com")
	adapter.SetClock(clk)

	// 1. Send bookTicker update
	bookData := []byte(`{
		"topic": "bookTicker",
		"symbol": "BTC-SWAP-USDT",
		"data": {
			"s": "BTC-SWAP-USDT",
			"b": "60000.5",
			"a": "60001.5"
		}
	}`)
	sym, pd, err := adapter.ParseTicker(bookData)
	require.NoError(t, err)
	assert.Equal(t, "BTC-SWAP-USDT", sym)
	assert.Equal(t, 60000.5, pd.BestBid)
	assert.Equal(t, 60001.5, pd.BestAsk)
	assert.Equal(t, 60001.0, pd.FairPrice)
	assert.Equal(t, 0.0, pd.LastPrice) // not merged yet

	// 1b. Send bookTicker update without root-level symbol but symbol in data
	bookDataNoRoot := []byte(`{
		"topic": "bookTicker",
		"data": {
			"s": "BTC-SWAP-USDT",
			"b": "60001.0",
			"a": "60002.0"
		}
	}`)
	sym, pd, err = adapter.ParseTicker(bookDataNoRoot)
	require.NoError(t, err)
	assert.Equal(t, "BTC-SWAP-USDT", sym)
	assert.Equal(t, 60001.0, pd.BestBid)
	assert.Equal(t, 60002.0, pd.BestAsk)
	assert.Equal(t, 60001.5, pd.FairPrice)

	// 2. Send realtimes update
	realtimesData := []byte(`{
		"topic": "realtimes",
		"symbol": "BTC-SWAP-USDT",
		"data": [
			{
				"s": "BTC-SWAP-USDT",
				"c": "60000.8",
				"v": "100.5"
			}
		]
	}`)
	sym, pd, err = adapter.ParseTicker(realtimesData)
	require.NoError(t, err)
	assert.Equal(t, "BTC-SWAP-USDT", sym)
	assert.Equal(t, 60000.8, pd.LastPrice)
	assert.Equal(t, 100.5, pd.Volume24)
	assert.Equal(t, 60001.0, pd.BestBid) // statefully merged!
	assert.Equal(t, 60002.0, pd.BestAsk)

	// 3. Send bookTicker update as empty array
	emptyBookData := []byte(`{
		"topic": "bookTicker",
		"symbol": "BTC-SWAP-USDT",
		"data": []
	}`)
	sym, pd, err = adapter.ParseTicker(emptyBookData)
	require.NoError(t, err)
	assert.Equal(t, "", sym)
	assert.Nil(t, pd)

	// 3b. Send realtimes update as empty array
	emptyRealtimesData := []byte(`{
		"topic": "realtimes",
		"symbol": "BTC-SWAP-USDT",
		"data": []
	}`)
	sym, pd, err = adapter.ParseTicker(emptyRealtimesData)
	require.NoError(t, err)
	assert.Equal(t, "", sym)
	assert.Nil(t, pd)

	// 4. Send realtimes update for a new symbol without bookTicker data to test fallback
	realtimesNewData := []byte(`{
		"topic": "realtimes",
		"symbol": "ETH-SWAP-USDT",
		"data": [
			{
				"s": "ETH-SWAP-USDT",
				"c": "3000.5",
				"v": "50.2"
			}
		]
	}`)
	sym, pd, err = adapter.ParseTicker(realtimesNewData)
	require.NoError(t, err)
	assert.Equal(t, "ETH-SWAP-USDT", sym)
	assert.Equal(t, 3000.5, pd.LastPrice)
	assert.Equal(t, 3000.5, pd.BestBid) // fallback applied!
	assert.Equal(t, 3000.5, pd.BestAsk)
	assert.Equal(t, 3000.5, pd.FairPrice)
}

func TestWsAdapter_ParsePosition(t *testing.T) {
	t.Parallel()

	adapter := toobit.NewWsAdapter("")

	// Test array format (production user stream structure)
	payload1 := []byte(`[{"e":"outboundContractPositionInfo","E":"1782316800655","A":"2243056424560222721","s":"ALICE-SWAP-USDT","S":"SHORT","p":"0.1225","P":"1224","a":"1224","f":"0","m":"2.9677","r":"-0.0089","up":"0","pr":"0","pv":"14.994","v":"5.0","mt":"ISOLATED","mm":"0","mp":"0.122500000000000000"}]`)
	pos, err := adapter.ParsePosition(payload1)
	require.NoError(t, err)
	assert.Equal(t, "ALICE-SWAP-USDT", pos.Symbol)
	assert.Equal(t, 1224.0, pos.HoldVolContract)
	assert.Equal(t, exchange.PositionTypeShort, pos.PositionType)
	assert.Equal(t, 0.1225, pos.HoldAvgPrice)
	assert.Equal(t, 0.0, pos.CloseProfitLoss)
	assert.Equal(t, 5, pos.Leverage)
	assert.Equal(t, int64(1782316800655), pos.UpdateTime)

	// Test batch array format with transition to closed position (last element is 0)
	payloadBatchClose := []byte(`[
		{"e":"outboundContractPositionInfo","E":"1786701060901","A":"2243056424560222721","s":"HOME-SWAP-USDT","S":"SHORT","p":"0.011136","P":"2262","a":"0","f":"0.013208","m":"4.9864","r":"-0.0125","up":"-0.0135","pr":"-0.0026","pv":"25.1896","v":"5.0","mt":"ISOLATED","mm":"0","mp":"0.011142000000000000"},
		{"e":"outboundContractPositionInfo","E":"1786701060901","A":"2243056424560222721","s":"HOME-SWAP-USDT","S":"SHORT","p":"0","P":"0","a":"0","f":"0","m":"0","r":"0","up":"0","pr":"0","pv":"0","v":"5.0","mt":"ISOLATED","mm":"0","mp":"0.011142000000000000"}
	]`)
	posClose, err := adapter.ParsePosition(payloadBatchClose)
	require.NoError(t, err)
	assert.Equal(t, "HOME-SWAP-USDT", posClose.Symbol)
	assert.Equal(t, 0.0, posClose.HoldVolContract)
	assert.Equal(t, exchange.PositionTypeShort, posClose.PositionType)
	assert.Equal(t, 0.0, posClose.HoldAvgPrice)
	assert.Equal(t, 0.0, posClose.CloseProfitLoss)
	assert.Equal(t, 5, posClose.Leverage)
	assert.Equal(t, int64(1786701060901), posClose.UpdateTime)
}

func TestWsAdapter_GetPrivateURLFunc(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"listenKey": "test_listen_key"}`))
	}))
	defer server.Close()

	client := toobit.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	adapter := toobit.NewWsAdapter("wss://stream.toobit.com")
	adapter.SetClient(client)

	urlFunc := adapter.GetPrivateURLFunc(context.Background())
	u, err := urlFunc()
	require.NoError(t, err)
	assert.Equal(t, "wss://stream.toobit.com/api/v1/ws/test_listen_key", u)
}

func TestWsAdapter_Subscriptions(t *testing.T) {
	t.Parallel()

	adapter := toobit.NewWsAdapter("wss://stream.toobit.com")
	pool := pkgws.NewPool("ws://127.0.0.1:1", 30, slog.Default())
	adapter.SetPool(pool)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_ = adapter.SubscribeTicker(ctx, "BTC-SWAP-USDT")
	_ = adapter.UnsubscribeTicker(ctx, "BTC-SWAP-USDT")
	_ = adapter.SubscribePersonal(ctx)
}

func TestWsAdapter_ParseTicker_Errors(t *testing.T) {
	t.Parallel()

	adapter := toobit.NewWsAdapter("")

	// invalid JSON
	_, _, err := adapter.ParseTicker([]byte(`{invalid`))
	require.Error(t, err)

	// empty bookTicker data returns no error, just empty fields
	sym, pd, err := adapter.ParseTicker([]byte(`{"topic": "bookTicker"}`))
	require.NoError(t, err)
	assert.Equal(t, "", sym)
	assert.Nil(t, pd)

	// unknown topic
	_, _, err = adapter.ParseTicker([]byte(`{"topic": "unknown", "symbol": "BTC-SWAP-USDT"}`))
	require.Error(t, err)

	// invalid bookTicker data
	_, _, err = adapter.ParseTicker([]byte(`{"topic": "bookTicker", "symbol": "BTC-SWAP-USDT", "data": "invalid"}`))
	require.Error(t, err)

	// invalid realtimes data
	_, _, err = adapter.ParseTicker([]byte(`{"topic": "realtimes", "symbol": "BTC-SWAP-USDT", "data": "invalid"}`))
	require.Error(t, err)
}

func TestWsAdapter_ParsePosition_Errors(t *testing.T) {
	t.Parallel()

	adapter := toobit.NewWsAdapter("")

	// invalid JSON
	_, err := adapter.ParsePosition([]byte(`{invalid`))
	require.Error(t, err)

	// empty position array
	_, err = adapter.ParsePosition([]byte(`[]`))
	require.Error(t, err)

	// missing symbol
	_, err = adapter.ParsePosition([]byte(`[{"event": "outboundContractPositionInfo"}]`))
	require.Error(t, err)
}

func TestWsAdapter_ParseDepth(t *testing.T) {
	t.Parallel()

	adapter := toobit.NewWsAdapter("")

	// Valid depth payload array with string version ("112801745_18")
	raw := []byte(`{
		"topic": "depth",
		"symbol": "BTC-SWAP-USDT",
		"data": [{
			"b": [["60000.5", "1.2"], ["60000.0", "3.4"]],
			"a": [["60001.0", "0.5"], ["60001.5", "2.1"]],
			"v": "112801745_18",
			"t": 1600000000000
		}],
		"f": true
	}`)

	sym, ob, err := adapter.ParseDepth(raw)
	require.NoError(t, err)
	assert.Equal(t, "BTC-SWAP-USDT", sym)
	require.NotNil(t, ob)
	assert.Equal(t, int64(112801745), ob.Version)
	require.Len(t, ob.Bids, 2)
	assert.Equal(t, 60000.5, ob.Bids[0].Price)
	assert.Equal(t, 1.2, ob.Bids[0].Volume)
	require.Len(t, ob.Asks, 2)
	assert.Equal(t, 60001.0, ob.Asks[0].Price)
	assert.Equal(t, 0.5, ob.Asks[0].Volume)

	// Valid depth payload single object
	rawSingle := []byte(`{
		"topic": "depth",
		"symbol": "ETH-SWAP-USDT",
		"data": {
			"b": [["3000.0", "10.0"]],
			"a": [["3001.0", "5.0"]],
			"v": 67890
		}
	}`)

	sym2, ob2, err := adapter.ParseDepth(rawSingle)
	require.NoError(t, err)
	assert.Equal(t, "ETH-SWAP-USDT", sym2)
	require.NotNil(t, ob2)
	assert.Equal(t, int64(67890), ob2.Version)
	require.Len(t, ob2.Bids, 1)

	// Invalid JSON
	_, _, err = adapter.ParseDepth([]byte(`invalid`))
	require.Error(t, err)

	// Empty data
	symEmpty, obEmpty, err := adapter.ParseDepth([]byte(`{"topic": "depth", "symbol": "BTC-SWAP-USDT", "data": []}`))
	require.NoError(t, err)
	assert.Equal(t, "BTC-SWAP-USDT", symEmpty)
	assert.Nil(t, obEmpty)
}

func TestWsAdapter_DepthSubscriptions(t *testing.T) {
	t.Parallel()

	adapter := toobit.NewWsAdapter("wss://stream.toobit.com")
	pool := pkgws.NewPool("ws://127.0.0.1:1", 30, slog.Default())
	adapter.SetPool(pool)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_ = adapter.SubscribeDepth(ctx, "BTC-SWAP-USDT")
	_ = adapter.UnsubscribeDepth(ctx, "BTC-SWAP-USDT")
}
