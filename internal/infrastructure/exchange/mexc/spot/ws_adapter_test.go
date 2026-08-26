package spot_test

import (
	"context"
	"testing"
	"time"

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
		sendTime := int64(1670000000000)
		pbWrapper := &proto.PushDataV3ApiWrapper{
			Channel:  channel,
			Symbol:   &sym,
			SendTime: &sendTime,
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
		assert.Equal(t, time.UnixMilli(1670000000000).UTC(), depth.Timestamp)
		require.Len(t, depth.Bids, 1)
		assert.Equal(t, 100.0, depth.Bids[0].Price)
		assert.Equal(t, 4.5, depth.Bids[0].Volume)
		require.Len(t, depth.Asks, 1)
		assert.Equal(t, 101.5, depth.Asks[0].Price)
		assert.Equal(t, 0.0, depth.Asks[0].Volume)
	})

	t.Run("Protobuf Public Limit Depth", func(t *testing.T) {
		t.Parallel()
		sym := "BTCUSDT"
		channel := "spot@public.limit.depth.v3.api.pb@BTCUSDT"
		sendTime := int64(1778075380657)
		pbWrapper := &proto.PushDataV3ApiWrapper{
			Channel:  channel,
			Symbol:   &sym,
			SendTime: &sendTime,
			Body: &proto.PushDataV3ApiWrapper_PublicLimitDepths{
				PublicLimitDepths: &proto.PublicLimitDepthsV3Api{
					EventType: "spot@public.limit.depth.v3.api.pb",
					Version:   "10025",
					Bids: []*proto.PublicLimitDepthV3ApiItem{
						{Price: "100.0", Quantity: "4.5"},
					},
					Asks: []*proto.PublicLimitDepthV3ApiItem{
						{Price: "101.5", Quantity: "0"},
					},
				},
			},
		}
		raw, err := googleproto.Marshal(pbWrapper)
		require.NoError(t, err)

		symRes, depth, err := adapter.ParseDepth(raw)
		require.NoError(t, err)
		require.NotNil(t, depth)
		assert.Equal(t, "BTCUSDT", symRes)
		assert.Equal(t, "BTCUSDT", depth.Symbol)
		assert.Equal(t, int64(10025), depth.Version)
		assert.Equal(t, time.UnixMilli(1778075380657).UTC(), depth.Timestamp)
		require.Len(t, depth.Bids, 1)
		assert.Equal(t, 100.0, depth.Bids[0].Price)
		assert.Equal(t, 4.5, depth.Bids[0].Volume)
		require.Len(t, depth.Asks, 1)
		assert.Equal(t, 101.5, depth.Asks[0].Price)
	})

	t.Run("Protobuf Public Increase Depth", func(t *testing.T) {
		t.Parallel()
		sym := "BTCUSDT"
		channel := "spot@public.increase.depth.v3.api.pb@BTCUSDT"
		sendTime := int64(1670000000000)
		pbWrapper := &proto.PushDataV3ApiWrapper{
			Channel:  channel,
			Symbol:   &sym,
			SendTime: &sendTime,
			Body: &proto.PushDataV3ApiWrapper_PublicIncreaseDepths{
				PublicIncreaseDepths: &proto.PublicIncreaseDepthsV3Api{
					EventType: "spot@public.increase.depth.v3.api.pb",
					Version:   "12345",
					Bids: []*proto.PublicIncreaseDepthV3ApiItem{
						{Price: "50000.00", Quantity: "2.00"},
					},
					Asks: []*proto.PublicIncreaseDepthV3ApiItem{
						{Price: "50001.00", Quantity: "1.00"},
					},
				},
			},
		}
		raw, err := googleproto.Marshal(pbWrapper)
		require.NoError(t, err)

		symRes, depth, err := adapter.ParseDepth(raw)
		require.NoError(t, err)
		require.NotNil(t, depth)
		assert.Equal(t, "BTCUSDT", symRes)
		assert.Equal(t, "BTCUSDT", depth.Symbol)
		assert.Equal(t, int64(12345), depth.Version)
		assert.Equal(t, time.UnixMilli(1670000000000).UTC(), depth.Timestamp)
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

	tradeWrapper := &proto.PushDataV3ApiWrapper{
		Channel: "spot@public.aggre.deals.v3.api.pb@100ms@BTCUSDT",
		Symbol:  &sym,
		Body: &proto.PushDataV3ApiWrapper_PublicAggreDeals{
			PublicAggreDeals: &proto.PublicAggreDealsV3Api{},
		},
	}
	tradeBytes, err := googleproto.Marshal(tradeWrapper)
	require.NoError(t, err)
	assert.Equal(t, "trade", extractor(tradeBytes))

	assert.Equal(t, "depth", extractor([]byte(`{"channel":"spot@public.aggre.depth.v3.api.pb@100ms@BTCUSDT"}`)))
	assert.Equal(t, "depth", extractor([]byte(`{"c":"spot@public.increase.depth.v3.api@BTCUSDT"}`)))
	assert.Equal(t, "ticker", extractor([]byte(`{"c":"spot@public.bookTicker.v3.api@BTCUSDT"}`)))
	assert.Equal(t, "trade", extractor([]byte(`{"channel":"spot@public.aggre.deals.v3.api.pb@100ms@BTCUSDT"}`)))
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
	assert.NoError(t, adapter.SubscribePersonal(ctx))
	assert.NoError(t, adapter.UnsubscribePersonal(ctx))

	pingPayload, dur := adapter.GetPingConfig()
	assert.NotNil(t, pingPayload)
	assert.Greater(t, dur, int64(0))

	assert.Nil(t, adapter.GetAuthHook("key", "secret"))
}

func TestSpotWsAdapter_ParseTrade(t *testing.T) {
	t.Parallel()
	adapter := spot.NewWsAdapter()

	t.Run("Protobuf Public Aggregated Deals", func(t *testing.T) {
		t.Parallel()
		sym := "MXUSDT"
		channel := "spot@public.aggre.deals.v3.api.pb@100ms@MXUSDT"
		sendTime := int64(1778158574304)
		pbWrapper := &proto.PushDataV3ApiWrapper{
			Channel:  channel,
			Symbol:   &sym,
			SendTime: &sendTime,
			Body: &proto.PushDataV3ApiWrapper_PublicAggreDeals{
				PublicAggreDeals: &proto.PublicAggreDealsV3Api{
					EventType: "spot@public.aggre.deals.v3.api.pb@100ms",
					Deals: []*proto.PublicAggreDealsV3ApiItem{
						{
							Price:     "94.75",
							Quantity:  "0.1081677",
							TradeType: 2,
							Time:      1778158574194,
							TradeId:   "681055725002145792X1",
						},
						{
							Price:     "94.50",
							Quantity:  "0.5",
							TradeType: 1,
							Time:      1778158574195,
							TradeId:   "681055725002145792X2",
						},
					},
				},
			},
		}
		raw, err := googleproto.Marshal(pbWrapper)
		require.NoError(t, err)

		parsedSym, trades, err := adapter.ParseTrade(raw)
		require.NoError(t, err)
		assert.Equal(t, "MXUSDT", parsedSym)
		require.Len(t, trades, 2)

		assert.Equal(t, 94.75, trades[0].Price)
		assert.Equal(t, 0.1081677, trades[0].Volume)
		assert.False(t, trades[0].Side.IsLong()) // TradeType=2 => SideOpenShort
		assert.Equal(t, time.UnixMilli(1778158574194).UTC(), trades[0].Timestamp)

		assert.Equal(t, 94.50, trades[1].Price)
		assert.Equal(t, 0.5, trades[1].Volume)
		assert.True(t, trades[1].Side.IsLong()) // TradeType=1 => SideOpenLong
		assert.Equal(t, time.UnixMilli(1778158574195).UTC(), trades[1].Timestamp)
	})

	t.Run("Protobuf Public Deals", func(t *testing.T) {
		t.Parallel()
		sym := "BTCUSDT"
		channel := "spot@public.deals.v3.api.pb@BTCUSDT"
		sendTime := int64(1778158574304)
		pbWrapper := &proto.PushDataV3ApiWrapper{
			Channel:  channel,
			Symbol:   &sym,
			SendTime: &sendTime,
			Body: &proto.PushDataV3ApiWrapper_PublicDeals{
				PublicDeals: &proto.PublicDealsV3Api{
					EventType: "spot@public.deals.v3.api.pb",
					Deals: []*proto.PublicDealsV3ApiItem{
						{
							Price:     "65000.0",
							Quantity:  "0.25",
							TradeType: 1,
							Time:      1778158574200,
						},
					},
				},
			},
		}
		raw, err := googleproto.Marshal(pbWrapper)
		require.NoError(t, err)

		parsedSym, trades, err := adapter.ParseTrade(raw)
		require.NoError(t, err)
		assert.Equal(t, "BTCUSDT", parsedSym)
		require.Len(t, trades, 1)

		assert.Equal(t, 65000.0, trades[0].Price)
		assert.Equal(t, 0.25, trades[0].Volume)
		assert.True(t, trades[0].Side.IsLong())
		assert.Equal(t, time.UnixMilli(1778158574200).UTC(), trades[0].Timestamp)
	})
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
