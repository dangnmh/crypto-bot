package spot_test

import (
	"context"
	"testing"

	"crypto-bot/internal/infrastructure/exchange/kucoin/spot"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpotWsAdapter_ParseDepth(t *testing.T) {
	t.Parallel()
	adapter := spot.NewWsAdapter()
	msg := []byte(`{
		"topic": "/market/level2:BTC-USDT",
		"data": {
			"sequenceStart": 100,
			"sequenceEnd": 105,
			"symbol": "BTC-USDT",
			"changes": {
				"asks": [["50001.00", "1.00"]],
				"bids": [["50000.00", "2.00"]]
			}
		}
	}`)

	sym, depth, err := adapter.ParseDepth(msg)
	require.NoError(t, err)
	require.NotNil(t, depth)
	assert.Equal(t, "BTC-USDT", sym)
	assert.Equal(t, "BTC-USDT", depth.Symbol)
	assert.Equal(t, int64(100), depth.FirstVersion)
	assert.Equal(t, int64(105), depth.Version)
	require.Len(t, depth.Bids, 1)
	require.Len(t, depth.Asks, 1)
}

func TestSpotWsAdapter_Subscriptions(t *testing.T) {
	t.Parallel()
	adapter := spot.NewWsAdapter()
	ctx := context.Background()

	assert.NoError(t, adapter.SubscribeDepth(ctx, "BTC-USDT"))
	assert.NoError(t, adapter.UnsubscribeDepth(ctx, "BTC-USDT"))
	assert.NoError(t, adapter.SubscribeTicker(ctx, "BTC-USDT"))
	assert.NoError(t, adapter.UnsubscribeTicker(ctx, "BTC-USDT"))

	pingPayload, dur := adapter.GetPingConfig()
	assert.NotNil(t, pingPayload)
	assert.Greater(t, dur, int64(0))

	assert.Nil(t, adapter.GetAuthHook("key", "secret"))
}
