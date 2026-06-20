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

	_ = adapter.SubscribeTicker(ctx, "BTC")
	_ = adapter.UnsubscribeTicker(ctx, "BTC")

	// 4. Test stubs
	_, _, err = adapter.ParseDepthCommit([]byte{})
	assert.NoError(t, err)

	_, err = adapter.ParsePosition([]byte{})
	assert.NoError(t, err)
}
