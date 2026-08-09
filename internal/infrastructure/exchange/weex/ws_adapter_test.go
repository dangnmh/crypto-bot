package weex_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/exchange/weex"
	pkgws "crypto-bot/pkg/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var NewWsAdapter = weex.NewWsAdapter

func TestWsAdapter_GetPingConfig(t *testing.T) {
	t.Parallel()
	a := NewWsAdapter("", "", "")
	payload, interval := a.GetPingConfig()

	assert.Nil(t, payload)
	assert.Equal(t, 0*time.Second, interval)
}

func TestWsAdapter_GetChannelExtractor(t *testing.T) {
	t.Parallel()
	a := NewWsAdapter("", "", "")
	extractor := a.GetChannelExtractor()
	require.NotNil(t, extractor)

	assert.Equal(t, "ticker", extractor([]byte(`{"channel":"ticker.BTCUSDT"}`)))
	assert.Equal(t, "ticker", extractor([]byte(`{"e":"ticker","s":"BTCUSDT"}`)))
	assert.Equal(t, "ticker", extractor([]byte(`{"e":"depth","s":"BTCUSDT"}`)))
	assert.Equal(t, "personal.position", extractor([]byte(`{"e":"positions"}`)))
	assert.Equal(t, "personal.order", extractor([]byte(`{"e":"orders"}`)))
	assert.Equal(t, "subscribed", extractor([]byte(`{"event":"subscribed"}`)))
	assert.Equal(t, "", extractor([]byte(`{"channel":""}`)))
}

func TestWsAdapter_HandshakeHeaders(t *testing.T) {
	t.Parallel()

	a := NewWsAdapter("my-key", "my-secret", "my-passphrase")
	a.SetClock(mockClock{now: time.UnixMilli(10002000)})

	headers, err := a.HandshakeHeaders()
	require.NoError(t, err)

	assert.Equal(t, "my-key", headers.Get("ACCESS-KEY"))
	assert.Equal(t, "my-passphrase", headers.Get("ACCESS-PASSPHRASE"))
	assert.Equal(t, "10002000", headers.Get("ACCESS-TIMESTAMP"))
	assert.NotEmpty(t, headers.Get("ACCESS-SIGN"))
}

func TestWsAdapter_ParseTicker(t *testing.T) {
	t.Parallel()

	a := NewWsAdapter("", "", "")
	payloadSpec := []byte(`{
		"e": "ticker",
		"E": 1773295738939,
		"s": "BTCUSDT",
		"d": [
			{
				"p": "-2055.6",
				"P": "-1.96",
				"w": "102345.12",
				"c": "102623.90",
				"o": "104679.50",
				"h": "104692.20",
				"l": "100709.60",
				"v": "176145.66489",
				"q": "18115688543.1",
				"O": 1773210000000,
				"C": 1773296400000,
				"n": 28941,
				"m": "102620.00",
				"i": "102615.50"
			}
		]
	}`)

	sym, pd, err := a.ParseTicker(payloadSpec)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", sym)
	assert.Equal(t, 102623.90, pd.LastPrice)
	assert.Equal(t, 102623.90, pd.BestBid)
	assert.Equal(t, 102623.90, pd.BestAsk)
	assert.Equal(t, 102620.00, pd.FairPrice)
	assert.Equal(t, 176145.66489, pd.Volume24)

	// test depth payload
	payloadDepth := []byte(`{
		"e": "depth",
		"E": 1773295701456,
		"s": "BTCUSDT",
		"b": [
			["103435.90", "2.10000"]
		],
		"a": [
			["103436.10", "1.21500"]
		]
	}`)

	sym, pd, err = a.ParseTicker(payloadDepth)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", sym)
	assert.Equal(t, 103435.90, pd.BestBid)
	assert.Equal(t, 103436.10, pd.BestAsk)
	// Cached ticker fields should still be present
	assert.Equal(t, 102623.90, pd.LastPrice)
	assert.Equal(t, 102620.00, pd.FairPrice)
}

func TestWsAdapter_ParsePosition(t *testing.T) {
	t.Parallel()

	a := NewWsAdapter("", "", "")
	payload := []byte(`{
		"e": "positions",
		"E": 1773298805123,
		"d": [
			{
				"symbol": "BTCUSDT",
				"side": "LONG",
				"size": "0.5",
				"leverage": "20",
				"openValue": "34250.0",
				"updatedTime": "1747188961302"
			}
		]
	}`)

	pos, err := a.ParsePosition(payload)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", pos.Symbol)
	assert.Equal(t, 0.5, pos.HoldVolContract)
	assert.Equal(t, 20, pos.Leverage)
	assert.Equal(t, 68500.0, pos.OpenAvgPrice)

	payloadShort := []byte(`{
		"e": "positions",
		"E": 1773298805123,
		"d": [
			{
				"symbol": "ETHUSDT",
				"side": "SHORT",
				"size": "1.5",
				"leverage": "10",
				"openValue": "5175.0",
				"updatedTime": "1747188961302"
			}
		]
	}`)

	pos, err = a.ParsePosition(payloadShort)
	require.NoError(t, err)
	assert.Equal(t, "ETHUSDT", pos.Symbol)
	assert.Equal(t, 1.5, pos.HoldVolContract)
	assert.Equal(t, 10, pos.Leverage)
	assert.Equal(t, 3450.0, pos.OpenAvgPrice)
}

func TestWsAdapter_SubscribeAndUnsubscribe(t *testing.T) {
	t.Parallel()

	a := NewWsAdapter("", "", "")
	pool := pkgws.NewPool("ws://dummy", 30, slog.Default())
	a.SetPool(pool)

	// Just invoke subscription methods to verify they call the pool under test context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := a.SubscribeTicker(ctx, "BTCUSDT")
	assert.Error(t, err) // cancelled context should fail subscription

	err = a.UnsubscribeTicker(ctx, "BTCUSDT")
	assert.NoError(t, err) // untracked topic should succeed (noop)

	err = a.SubscribePersonal(ctx)
	assert.NoError(t, err) // nil privateClient should succeed (noop)

	// test SetClient
	a.SetClient(nil)

	// test GetCustomPingHandler
	pingHandler := a.GetCustomPingHandler()
	assert.NotNil(t, pingHandler)
	assert.False(t, pingHandler(nil, []byte(`{"event":"not-ping"}`)))

	// test GetAuthHook
	authHook := a.GetAuthHook("key", "secret")
	assert.Nil(t, authHook)
}
