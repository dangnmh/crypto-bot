package futures_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/toobit/futures"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFuturesWsAdapter_ParseDepth(t *testing.T) {
	t.Parallel()
	adapter := futures.NewWsAdapter("wss://stream.toobit.com")

	// Array format
	msg := []byte(`{
		"topic": "diffDepth",
		"symbol": "BTC-SWAP-USDT",
		"data": [{
			"b": [["50000.5", "2.0"]],
			"a": [["50001.5", "1.5"]],
			"v": "100_1",
			"t": 1670000000000
		}]
	}`)

	sym, depth, err := adapter.ParseDepth(msg)
	require.NoError(t, err)
	require.NotNil(t, depth)
	assert.Equal(t, "BTC-SWAP-USDT", sym)
	assert.Equal(t, "BTC-SWAP-USDT", depth.Symbol)
	assert.Equal(t, int64(100), depth.Version)
	assert.Equal(t, time.UnixMilli(1670000000000).UTC(), depth.Timestamp)
	require.Len(t, depth.Bids, 1)
	require.Len(t, depth.Asks, 1)

	// Single object format
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

func TestFuturesWsAdapter_ParseTicker(t *testing.T) {
	t.Parallel()
	adapter := futures.NewWsAdapter("")

	bookTickerData := []byte(`{
		"topic": "bookTicker",
		"symbol": "BTC-SWAP-USDT",
		"data": {
			"s": "BTC-SWAP-USDT",
			"b": "50000.0",
			"a": "50001.0"
		}
	}`)
	sym, pd, err := adapter.ParseTicker(bookTickerData)
	require.NoError(t, err)
	assert.Equal(t, "BTC-SWAP-USDT", sym)
	assert.Equal(t, 50000.0, pd.BestBid)
	assert.Equal(t, 50001.0, pd.BestAsk)

	realtimesData := []byte(`{
		"topic": "realtimes",
		"symbol": "BTC-SWAP-USDT",
		"data": [
			{
				"s": "BTC-SWAP-USDT",
				"c": "50000.5",
				"v": "100.0"
			}
		]
	}`)
	sym, pd, err = adapter.ParseTicker(realtimesData)
	require.NoError(t, err)
	assert.Equal(t, "BTC-SWAP-USDT", sym)
	assert.Equal(t, 50000.5, pd.LastPrice)
	assert.Equal(t, 50000.0, pd.BestBid)
	assert.Equal(t, 50001.0, pd.BestAsk)

	// Errors
	_, _, err = adapter.ParseTicker([]byte(`{invalid`))
	require.Error(t, err)

	symEmpty, pdEmpty, err := adapter.ParseTicker([]byte(`{"topic": "bookTicker"}`))
	require.NoError(t, err)
	assert.Equal(t, "", symEmpty)
	assert.Nil(t, pdEmpty)

	_, _, err = adapter.ParseTicker([]byte(`{"topic": "unknown", "symbol": "BTC-SWAP-USDT"}`))
	require.Error(t, err)
}

func TestFuturesWsAdapter_ParsePosition(t *testing.T) {
	t.Parallel()

	adapter := futures.NewWsAdapter("")

	// Test array format
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

	// Test closed position (last element is 0)
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

	// Errors
	_, err = adapter.ParsePosition([]byte(`{invalid`))
	require.Error(t, err)

	_, err = adapter.ParsePosition([]byte(`[]`))
	require.Error(t, err)

	_, err = adapter.ParsePosition([]byte(`[{"event": "outboundContractPositionInfo"}]`))
	require.Error(t, err)
}

func TestFuturesWsAdapter_GetPrivateURLFunc(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"listenKey": "test_listen_key"}`))
	}))
	defer server.Close()

	client := futures.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	adapter := futures.NewWsAdapter("wss://stream.toobit.com")
	adapter.SetClient(client)

	urlFunc := adapter.GetPrivateURLFunc(context.Background())
	u, err := urlFunc()
	require.NoError(t, err)
	assert.Equal(t, "wss://stream.toobit.com/api/v1/ws/test_listen_key", u)
}

func TestFuturesWsAdapter_Subscriptions(t *testing.T) {
	t.Parallel()
	adapter := futures.NewWsAdapter("wss://stream.toobit.com")
	ctx := context.Background()

	assert.NoError(t, adapter.SubscribeDepth(ctx, "BTC-SWAP-USDT"))
	assert.NoError(t, adapter.UnsubscribeDepth(ctx, "BTC-SWAP-USDT"))
	assert.NoError(t, adapter.SubscribeTrade(ctx, "BTC-SWAP-USDT"))
	assert.NoError(t, adapter.UnsubscribeTrade(ctx, "BTC-SWAP-USDT"))
	assert.NoError(t, adapter.SubscribeTicker(ctx, "BTC-SWAP-USDT"))
	assert.NoError(t, adapter.UnsubscribeTicker(ctx, "BTC-SWAP-USDT"))
	assert.NoError(t, adapter.SubscribePersonal(ctx))

	pingPayload, dur := adapter.GetPingConfig()
	assert.NotNil(t, pingPayload)
	assert.Greater(t, dur, int64(0))

	assert.Nil(t, adapter.GetAuthHook("key", "secret"))
}

func TestFuturesWsAdapter_ParseTrade(t *testing.T) {
	t.Parallel()
	adapter := futures.NewWsAdapter("wss://stream.toobit.com")

	// Array format with isBuyerMaker (m=true => Taker Sell, m=false => Taker Buy)
	msg := []byte(`{
		"topic": "trade",
		"symbol": "BTC-SWAP-USDT",
		"data": [
			{
				"p": "50000.5",
				"q": "2.0",
				"v": "4900197869782446621",
				"t": 1670000000000,
				"m": true
			},
			{
				"p": "50001.0",
				"q": "1.5",
				"v": "4900197869782446622",
				"t": 1670000001000,
				"m": false
			}
		]
	}`)

	sym, trades, err := adapter.ParseTrade(msg)
	require.NoError(t, err)
	assert.Equal(t, "BTC-SWAP-USDT", sym)
	require.Len(t, trades, 2)

	assert.Equal(t, 50000.5, trades[0].Price)
	assert.Equal(t, 2.0, trades[0].Volume)
	assert.Equal(t, false, trades[0].Side.IsLong()) // m=true => Taker Sell => SideOpenShort

	assert.Equal(t, 50001.0, trades[1].Price)
	assert.Equal(t, 1.5, trades[1].Volume)
	assert.Equal(t, true, trades[1].Side.IsLong()) // m=false => Taker Buy => SideOpenLong

	// Single object format with Side string
	singleMsg := []byte(`{
		"topic": "trade",
		"symbol": "ETH-SWAP-USDT",
		"data": {
			"p": "3000.0",
			"q": "10.0",
			"v": "4900197869782446623",
			"m": true,
			"t": 1670000002000
		}
	}`)
	sym2, trades2, err := adapter.ParseTrade(singleMsg)
	require.NoError(t, err)
	assert.Equal(t, "ETH-SWAP-USDT", sym2)
	require.Len(t, trades2, 1)
	assert.Equal(t, 3000.0, trades2[0].Price)
	assert.Equal(t, 10.0, trades2[0].Volume)
	assert.Equal(t, false, trades2[0].Side.IsLong())

	// Empty data
	symEmpty, tradesEmpty, err := adapter.ParseTrade([]byte(`{"topic": "trade", "symbol": "BTC-SWAP-USDT", "data": []}`))
	require.NoError(t, err)
	assert.Equal(t, "BTC-SWAP-USDT", symEmpty)
	assert.Nil(t, tradesEmpty)

	// Invalid JSON
	_, _, err = adapter.ParseTrade([]byte(`invalid`))
	require.Error(t, err)
}
