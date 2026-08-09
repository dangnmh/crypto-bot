package xt_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/xt"
	pkgws "crypto-bot/pkg/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWsAdapter_Lifecycle(t *testing.T) {
	t.Parallel()

	adapter := xt.NewWsAdapter()
	pool := pkgws.NewPool("ws://127.0.0.1:1", 30, slog.Default())
	adapter.SetPool(pool)
	adapter.SetClient(nil)

	// Ping config
	ping, interval := adapter.GetPingConfig()
	assert.Equal(t, "ping", ping)
	assert.Equal(t, 20*time.Second, interval)

	// Auth hook
	hook := adapter.GetAuthHook("my-key", "my-secret")
	assert.Nil(t, hook)

	// Channel extractor
	extractor := adapter.GetChannelExtractor()
	require.NotNil(t, extractor)

	assert.Equal(t, "ticker", extractor([]byte(`{"topic": "agg_ticker", "event": "agg_ticker@btc_usdt"}`)))
	assert.Equal(t, "personal.position", extractor([]byte(`{"event": "position@myListenKey"}`)))
	assert.Equal(t, "personal.order", extractor([]byte(`{"event": "order@myListenKey"}`)))

	// Subscriptions with cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_ = adapter.SubscribeTicker(ctx, "BTCUSDT")
	_ = adapter.UnsubscribeTicker(ctx, "BTCUSDT")
}

func TestWsAdapter_ParseTicker(t *testing.T) {
	t.Parallel()

	adapter := xt.NewWsAdapter()

	tickerData := []byte(`{
		"topic": "agg_ticker",
		"event": "agg_ticker@btc_usdt",
		"data": {
			"t": 1782570000000,
			"s": "btc_usdt",
			"c": "60500.5",
			"a": "10.25",
			"bp": "60500.0",
			"ap": "60501.0"
		}
	}`)

	sym, pd, err := adapter.ParseTicker(tickerData)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", sym)
	assert.Equal(t, 60500.5, pd.LastPrice)
	assert.Equal(t, 10.25, pd.Volume24)
	assert.Equal(t, 60500.0, pd.BestBid)
	assert.Equal(t, 60501.0, pd.BestAsk)
}

func TestWsAdapter_ParsePosition(t *testing.T) {
	t.Parallel()

	adapter := xt.NewWsAdapter()

	positionData := []byte(`{
		"topic": "position",
		"event": "position@myListenKey",
		"data": {
			"symbol": "btc_usdt",
			"positionSide": "SHORT",
			"positionSize": "0.15",
			"entryPrice": "61000.0"
		}
	}`)

	p, err := adapter.ParsePosition(positionData)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", p.Symbol)
	assert.Equal(t, 0.15, p.HoldVolContract)
	assert.Equal(t, exchange.PositionTypeShort, p.PositionType)
	assert.Equal(t, 61000.0, p.OpenAvgPrice)
}
