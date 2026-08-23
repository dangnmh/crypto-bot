package spot_test

import (
	"context"
	"testing"

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
	require.Len(t, depth.Bids, 1)
	require.Len(t, depth.Asks, 1)
}

func TestSpotWsAdapter_Subscriptions(t *testing.T) {
	t.Parallel()
	adapter := spot.NewWsAdapter()
	ctx := context.Background()

	assert.NoError(t, adapter.SubscribeDepth(ctx, "BTCUSDT"))
	assert.NoError(t, adapter.UnsubscribeDepth(ctx, "BTCUSDT"))
	assert.NoError(t, adapter.SubscribeTicker(ctx, "BTCUSDT"))
	assert.NoError(t, adapter.UnsubscribeTicker(ctx, "BTCUSDT"))

	pingPayload, dur := adapter.GetPingConfig()
	assert.NotNil(t, pingPayload)
	assert.Greater(t, dur, int64(0))

	assert.Nil(t, adapter.GetAuthHook("key", "secret"))
}
