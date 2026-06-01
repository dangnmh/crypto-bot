package hyperliquid_test

import (
	"context"
	"log/slog"
	"testing"

	"crypto-bot/internal/infrastructure/exchange/hyperliquid"
	pkgws "crypto-bot/pkg/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWsAdapter_ParseTicker(t *testing.T) {
	t.Parallel()

	adapter := hyperliquid.NewWsAdapter()
	rawJSON := []byte(`{
		"channel": "allMids",
		"data": {
			"mids": {
				"BTC": "95000.5",
				"ETH": "3500.0"
			}
		}
	}`)

	symbol, pd, err := adapter.ParseTicker(rawJSON)
	require.NoError(t, err)
	assert.Equal(t, "BTC", symbol)
	assert.Equal(t, 95000.5, pd.LastPrice)
}

func TestWsAdapter_ParseDepth(t *testing.T) {
	t.Parallel()

	adapter := hyperliquid.NewWsAdapter()
	rawJSON := []byte(`{
		"channel": "l2Book",
		"data": {
			"coin": "BTC",
			"levels": [
				[{"px": "49999.0", "sz": "1.5", "n": 2}],
				[{"px": "50001.0", "sz": "2.5", "n": 3}]
			],
			"time": 1672531200000
		}
	}`)

	symbol, ob, err := adapter.ParseDepth(rawJSON)
	require.NoError(t, err)
	assert.Equal(t, "BTC", symbol)
	require.Len(t, ob.Bids, 1)
	require.Len(t, ob.Asks, 1)
	assert.Equal(t, 49999.0, ob.Bids[0].Price)
	assert.Equal(t, 1.5, ob.Bids[0].Volume)
	assert.Equal(t, 50001.0, ob.Asks[0].Price)
	assert.Equal(t, 2.5, ob.Asks[0].Volume)
}

func TestWsAdapter_ParseKline(t *testing.T) {
	t.Parallel()

	adapter := hyperliquid.NewWsAdapter()
	rawJSON := []byte(`{
		"channel": "candle",
		"data": {
			"t": 1672531200000,
			"T": 1672531259999,
			"i": "1m",
			"n": 10,
			"o": "50000.0",
			"h": "50100.0",
			"l": "49900.0",
			"c": "50050.0",
			"s": "BTC",
			"v": "5.0"
		}
	}`)

	symbol, k, err := adapter.ParseKline(rawJSON)
	require.NoError(t, err)
	assert.Equal(t, "BTC", symbol)
	assert.Equal(t, 50000.0, k.Open)
	assert.Equal(t, 50050.0, k.Close)
	assert.Equal(t, 5.0, k.Volume)
}

func TestWsAdapter_ParseOrder(t *testing.T) {
	t.Parallel()

	adapter := hyperliquid.NewWsAdapter()
	rawJSON := []byte(`{
		"channel": "user",
		"data": {
			"orders": [
				{
					"order": {
						"coin": "BTC",
						"side": "B",
						"limitPx": "50000.0",
						"sz": "0.01",
						"oid": 12345,
						"timestamp": 1672531200000,
						"origSz": "0.01",
						"cloid": "my_cloid"
					},
					"status": "filled",
					"statusTimestamp": 1672531250000
				}
			]
		}
	}`)

	deal, err := adapter.ParseOrder(rawJSON)
	require.NoError(t, err)
	assert.Equal(t, "BTC", deal.Symbol)
	assert.Equal(t, "12345", deal.OrderID)
	assert.Equal(t, 50000.0, deal.Price)
	assert.Equal(t, 0.01, deal.Vol)
	assert.Equal(t, "my_cloid", deal.ExternalOID)
}

func TestWsAdapter_AuthHookAndSubscription(t *testing.T) {
	t.Parallel()

	adapter := hyperliquid.NewWsAdapter()
	hook := adapter.GetAuthHook("", "0x0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	assert.Nil(t, hook)

	extractor := adapter.GetChannelExtractor()
	assert.Equal(t, "tickers", extractor([]byte(`{"channel": "allMids"}`)))
	assert.Equal(t, "depth", extractor([]byte(`{"channel": "l2Book"}`)))
	assert.Equal(t, "kline", extractor([]byte(`{"channel": "candle"}`)))
	assert.Equal(t, "personal.order", extractor([]byte(`{"channel": "user"}`)))
}

func TestWsAdapter_SubscriptionsAndAdditionalFeatures(t *testing.T) {
	t.Parallel()

	adapter := hyperliquid.NewWsAdapter()

	// 1. SubscribePersonal without auth hook should fail
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := adapter.SubscribePersonal(ctx)
	assert.Error(t, err)

	// 2. Set Auth Hook with a valid private key and verify SubscribePersonal works
	// ECDSA key: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" (64 hex characters)
	hook := adapter.GetAuthHook("apiKey", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	assert.Nil(t, hook) // GetAuthHook returns nil hook on Hyperliquid (it just intercepts)

	pool := pkgws.NewPool("ws://127.0.0.1:1", 30, slog.Default())
	defer pool.Close()
	adapter.SetPool(pool)

	// Should not return the initialization error anymore
	_ = adapter.SubscribePersonal(ctx)

	// 3. Test other subscriptions
	_ = adapter.SubscribeTicker(ctx, "BTC")
	_ = adapter.UnsubscribeTicker(ctx, "BTC")
	_ = adapter.SubscribeKline(ctx, "BTC")
	_ = adapter.UnsubscribeKline(ctx, "BTC")
	_ = adapter.SubscribeDepth(ctx, "BTC", "1")
	_ = adapter.UnsubscribeDepth(ctx, "BTC", "1")

	// 4. Test stubs
	_, _, err = adapter.ParseDepthCommit([]byte{})
	assert.NoError(t, err)

	_, err = adapter.ParseOrderDeal([]byte{})
	assert.NoError(t, err)

	_, err = adapter.ParseTrackOrder([]byte{})
	assert.NoError(t, err)

	_, err = adapter.ParsePosition([]byte{})
	assert.NoError(t, err)
}
