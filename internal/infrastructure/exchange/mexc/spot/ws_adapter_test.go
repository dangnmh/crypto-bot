package spot_test

import (
	"context"
	"testing"

	"crypto-bot/internal/infrastructure/exchange/mexc/spot"
	"crypto-bot/internal/infrastructure/exchange/mexc/spot/proto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	googleproto "google.golang.org/protobuf/proto"
)

func TestSpotWsAdapter_ParseDepth(t *testing.T) {
	t.Parallel()
	adapter := spot.NewWsAdapter()

	t.Run("Protobuf Public Aggregated Depth", func(t *testing.T) {
		t.Parallel()
		sym := "BTCUSDT"
		channel := "spot@public.aggre.depth.v3.api.pb@100ms@BTCUSDT"
		pbWrapper := &proto.PushDataV3ApiWrapper{
			Channel: channel,
			Symbol:  &sym,
			Body: &proto.PushDataV3ApiWrapper_PublicAggreDepths{
				PublicAggreDepths: &proto.PublicAggreDepthsV3Api{
					EventType:   "spot@public.aggre.depth.v3.api.pb@100ms",
					FromVersion: "10023",
					ToVersion:   "10025",
					Bids: []*proto.PublicAggreDepthV3ApiItem{
						{Price: "100.0", Quantity: "4.5"},
					},
					Asks: []*proto.PublicAggreDepthV3ApiItem{
						{Price: "101.5", Quantity: "0"},
					},
				},
			},
		}
		raw, err := googleproto.Marshal(pbWrapper)
		require.NoError(t, err)

		parsedSym, depth, err := adapter.ParseDepth(raw)
		require.NoError(t, err)
		require.NotNil(t, depth)
		assert.Equal(t, "BTCUSDT", parsedSym)
		assert.Equal(t, "BTCUSDT", depth.Symbol)
		assert.Equal(t, int64(10023), depth.FirstVersion)
		assert.Equal(t, int64(10025), depth.Version)
		require.Len(t, depth.Bids, 1)
		assert.Equal(t, 100.0, depth.Bids[0].Price)
		assert.Equal(t, 4.5, depth.Bids[0].Volume)
		require.Len(t, depth.Asks, 1)
		assert.Equal(t, 101.5, depth.Asks[0].Price)
		assert.Equal(t, 0.0, depth.Asks[0].Volume)
	})

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

func TestSpotWsAdapter_ParseTicker(t *testing.T) {
	t.Parallel()
	adapter := spot.NewWsAdapter()

	t.Run("Protobuf BookTicker", func(t *testing.T) {
		t.Parallel()
		sym := "BTCUSDT"
		channel := "spot@public.bookTicker.v3.api.pb@BTCUSDT"
		pbWrapper := &proto.PushDataV3ApiWrapper{
			Channel: channel,
			Symbol:  &sym,
			Body: &proto.PushDataV3ApiWrapper_PublicBookTicker{
				PublicBookTicker: &proto.PublicBookTickerV3Api{
					BidPrice:    "64000.5",
					BidQuantity: "2.5",
					AskPrice:    "64001.0",
					AskQuantity: "1.8",
				},
			},
		}
		raw, err := googleproto.Marshal(pbWrapper)
		require.NoError(t, err)

		parsedSym, pd, err := adapter.ParseTicker(raw)
		require.NoError(t, err)
		require.NotNil(t, pd)
		assert.Equal(t, "BTCUSDT", parsedSym)
		assert.Equal(t, "BTCUSDT", pd.Symbol)
		assert.Equal(t, 64000.5, pd.BestBid)
		assert.Equal(t, 64001.0, pd.BestAsk)
	})

	t.Run("JSON BookTicker", func(t *testing.T) {
		t.Parallel()
		raw := []byte(`{
			"c": "spot@public.bookTicker.v3.api@BTCUSDT",
			"d": {
				"s": "BTCUSDT",
				"b": "64000.5",
				"a": "64001.0"
			}
		}`)
		parsedSym, pd, err := adapter.ParseTicker(raw)
		require.NoError(t, err)
		require.NotNil(t, pd)
		assert.Equal(t, "BTCUSDT", parsedSym)
		assert.Equal(t, 64000.5, pd.BestBid)
		assert.Equal(t, 64001.0, pd.BestAsk)
	})
}

func TestSpotWsAdapter_ChannelExtractor(t *testing.T) {
	t.Parallel()
	adapter := spot.NewWsAdapter()
	extractor := adapter.GetChannelExtractor()

	sym := "BTCUSDT"
	depthWrapper := &proto.PushDataV3ApiWrapper{
		Channel: "spot@public.aggre.depth.v3.api.pb@100ms@BTCUSDT",
		Symbol:  &sym,
		Body: &proto.PushDataV3ApiWrapper_PublicAggreDepths{
			PublicAggreDepths: &proto.PublicAggreDepthsV3Api{},
		},
	}
	depthBytes, err := googleproto.Marshal(depthWrapper)
	require.NoError(t, err)
	assert.Equal(t, "depth", extractor(depthBytes))

	tickerWrapper := &proto.PushDataV3ApiWrapper{
		Channel: "spot@public.bookTicker.v3.api.pb@BTCUSDT",
		Symbol:  &sym,
		Body: &proto.PushDataV3ApiWrapper_PublicBookTicker{
			PublicBookTicker: &proto.PublicBookTickerV3Api{},
		},
	}
	tickerBytes, err := googleproto.Marshal(tickerWrapper)
	require.NoError(t, err)
	assert.Equal(t, "ticker", extractor(tickerBytes))

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

func TestSpotWsAdapter_CustomPingHandler(t *testing.T) {
	t.Parallel()
	adapter := spot.NewWsAdapter()
	handler := adapter.GetCustomPingHandler()
	require.NotNil(t, handler)

	// Pong frame is handled and ignored
	assert.True(t, handler(nil, []byte(`{"id": 0, "code": 0, "msg": "PONG"}`)))
	assert.True(t, handler(nil, []byte(`{"msg": "PONG"}`)))

	// Non-ping frame returns false
	assert.False(t, handler(nil, []byte(`{"channel": "spot@public.aggre.depth.v3.api.pb@100ms@BTCUSDT"}`)))
}
