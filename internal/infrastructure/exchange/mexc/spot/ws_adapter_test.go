package spot_test

import (
	"context"
	"testing"

	"crypto-bot/internal/infrastructure/exchange/mexc/spot"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpotWsAdapter_ParseDepth(t *testing.T) {
	t.Parallel()
	adapter := spot.NewWsAdapter()

	t.Run("Public Aggregated Diff Depth Format", func(t *testing.T) {
		t.Parallel()
		raw := []byte(`{
			"channel": "spot@public.aggre.depth.v3.api.pb@100ms@BTCUSDT",
			"symbol": "BTCUSDT",
			"sendTime": "1778075380657",
			"publicAggreDepths": {
				"asks": [
					{"price": "101.5", "quantity": "0"}
				],
				"bids": [
					{"price": "100.0", "quantity": "4.5"}
				],
				"eventType": "spot@public.aggre.depth.v3.api.pb@100ms",
				"fromVersion": "10023",
				"toVersion": "10025",
				"lastOrderCreateTime": "1778075380572"
			}
		}`)

		sym, depth, err := adapter.ParseDepth(raw)
		require.NoError(t, err)
		require.NotNil(t, depth)
		assert.Equal(t, "BTCUSDT", sym)
		assert.Equal(t, "BTCUSDT", depth.Symbol)
		assert.Equal(t, int64(10023), depth.FirstVersion)
		assert.Equal(t, int64(10025), depth.Version)
		require.Len(t, depth.Bids, 1)
		assert.Equal(t, 100.0, depth.Bids[0].Price)
		assert.Equal(t, 4.5, depth.Bids[0].Volume)
		require.Len(t, depth.Asks, 1)
		assert.Equal(t, 101.5, depth.Asks[0].Price)
		assert.Equal(t, 0.0, depth.Asks[0].Volume) // 0 quantity signals removal/cancellation
	})

	t.Run("Legacy Increase Depth Format", func(t *testing.T) {
		t.Parallel()
		msg := []byte(`{
			"c": "spot@public.increase.depth.v3.api@BTCUSDT",
			"d": {
				"asks": [{"p": "50001.00", "v": "1.00"}],
				"bids": [{"p": "50000.00", "v": "2.00"}],
				"v": "12345"
			},
			"t": 1670000000000
		}`)

		sym, depth, err := adapter.ParseDepth(msg)
		require.NoError(t, err)
		require.NotNil(t, depth)
		assert.Equal(t, "BTCUSDT", sym)
		assert.Equal(t, "BTCUSDT", depth.Symbol)
		assert.Equal(t, int64(12345), depth.Version)
		require.Len(t, depth.Bids, 1)
		require.Len(t, depth.Asks, 1)
	})
}

func TestSpotWsAdapter_ChannelExtractor(t *testing.T) {
	t.Parallel()
	adapter := spot.NewWsAdapter()
	extractor := adapter.GetChannelExtractor()

	assert.Equal(t, "depth", extractor([]byte(`{"channel":"spot@public.aggre.depth.v3.api.pb@100ms@BTCUSDT"}`)))
	assert.Equal(t, "depth", extractor([]byte(`{"c":"spot@public.increase.depth.v3.api@BTCUSDT"}`)))
	assert.Equal(t, "ticker", extractor([]byte(`{"c":"spot@public.bookTicker.v3.api@BTCUSDT"}`)))
}

func TestSpotWsAdapter_Subscriptions(t *testing.T) {
	t.Parallel()
	adapter := spot.NewWsAdapter()
	ctx := context.Background()

	assert.NoError(t, adapter.SubscribeDepth(ctx, "BTCUSDT"))
	assert.NoError(t, adapter.UnsubscribeDepth(ctx, "BTCUSDT"))
	assert.NoError(t, adapter.SubscribeTicker(ctx, "BTCUSDT"))
	assert.NoError(t, adapter.UnsubscribeTicker(ctx, "BTCUSDT"))
	assert.NoError(t, adapter.SubscribePersonal(ctx))
	assert.NoError(t, adapter.UnsubscribePersonal(ctx))

	pingPayload, dur := adapter.GetPingConfig()
	assert.NotNil(t, pingPayload)
	assert.Greater(t, dur, int64(0))

	assert.Nil(t, adapter.GetAuthHook("key", "secret"))
}
