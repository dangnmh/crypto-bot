package spot_test

import (
	"context"
	"testing"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange/toobit/spot"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpotWsAdapter_ParseDepth(t *testing.T) {
	t.Parallel()
	adapter := spot.NewWsAdapter()
	msg := []byte(`{
		"topic": "depth",
		"symbol": "BTCUSDT",
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
	assert.Equal(t, "BTCUSDT", sym)
	assert.Equal(t, "BTCUSDT", depth.Symbol)
	assert.Equal(t, int64(100), depth.Version)
	assert.Equal(t, time.UnixMilli(1670000000000).UTC(), depth.Timestamp)
	require.Len(t, depth.Bids, 1)
	require.Len(t, depth.Asks, 1)
}

func TestSpotWsAdapter_Subscriptions(t *testing.T) {
	t.Parallel()
	adapter := spot.NewWsAdapter()
	ctx := context.Background()

	assert.NoError(t, adapter.SubscribeDepth(ctx, "BTCUSDT"))
	assert.NoError(t, adapter.UnsubscribeDepth(ctx, "BTCUSDT"))
	assert.NoError(t, adapter.SubscribeTrade(ctx, "BTCUSDT"))
	assert.NoError(t, adapter.UnsubscribeTrade(ctx, "BTCUSDT"))
	assert.NoError(t, adapter.SubscribeTicker(ctx, "BTCUSDT"))
	assert.NoError(t, adapter.UnsubscribeTicker(ctx, "BTCUSDT"))

	pingPayload, dur := adapter.GetPingConfig()
	assert.NotNil(t, pingPayload)
	assert.Greater(t, dur, int64(0))

	assert.Nil(t, adapter.GetAuthHook("key", "secret"))

	extractor := adapter.GetChannelExtractor()
	assert.Equal(t, "depth", extractor([]byte(`{"topic":"depth"}`)))
	assert.Equal(t, "trade", extractor([]byte(`{"topic":"trade"}`)))
}

func TestSpotWsAdapter_ParseTrade(t *testing.T) {
	t.Parallel()
	adapter := spot.NewWsAdapter()

	msg := []byte(`{
		"symbol": "BTCUSDT",
		"symbolName": "BTCUSDT",
		"topic": "trade",
		"params": {
			"realtimeInterval": "24h",
			"binary": "false"
		},
		"data": [
			{
				"v": "1291465821801168896", 
				"t": 1668690723096,
				"p": "399",
				"q": "1",
				"m": false
			},
			{
				"v": "1291465842546196481",
				"t": 1668690725569,
				"p": "399",
				"q": "2",
				"m": true
			}
		],
		"f": true,
		"sendTime": 1668753154192
	}`)

	sym, trades, err := adapter.ParseTrade(msg)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", sym)
	require.Len(t, trades, 2)

	assert.Equal(t, 399.0, trades[0].Price)
	assert.Equal(t, 1.0, trades[0].Volume)
	assert.Equal(t, domain.SideOpenLong, trades[0].Side)
	assert.True(t, trades[0].Side.IsLong())
	assert.Equal(t, time.UnixMilli(1668690723096).UTC(), trades[0].Timestamp)

	assert.Equal(t, 399.0, trades[1].Price)
	assert.Equal(t, 2.0, trades[1].Volume)
	assert.Equal(t, domain.SideOpenShort, trades[1].Side)
	assert.False(t, trades[1].Side.IsLong())
	assert.Equal(t, time.UnixMilli(1668690725569).UTC(), trades[1].Timestamp)
}
