package spot_test

import (
	"context"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/exchange/kucoin/spot"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpotWsAdapter_ParseDepth(t *testing.T) {
	t.Parallel()
	adapter := spot.NewWsAdapter()
	msg := []byte(`{
		"topic": "/market/level2:BTC-USDT",
		"type": "message",
		"subject": "trade.l2update",
		"data": {
			"changes": {
				"asks": [
					["67993.3", "1.21427407", "14701689783"]
				],
				"bids": [
					["67990.0", "2.50000000", "14701689782"]
				]
			},
			"sequenceEnd": 14701689783,
			"sequenceStart": 14701689782,
			"symbol": "BTC-USDT",
			"time": 1729816425625
		}
	}`)

	sym, depth, err := adapter.ParseDepth(msg)
	require.NoError(t, err)
	require.NotNil(t, depth)
	assert.Equal(t, "BTC-USDT", sym)
	assert.Equal(t, "BTC-USDT", depth.Symbol)
	assert.Equal(t, int64(14701689782), depth.FirstVersion)
	assert.Equal(t, int64(14701689783), depth.Version)
	assert.Equal(t, time.UnixMilli(1729816425625).UTC(), depth.Timestamp)
	require.Len(t, depth.Bids, 1)
	assert.Equal(t, 67990.0, depth.Bids[0].Price)
	assert.Equal(t, 2.5, depth.Bids[0].Volume)
	require.Len(t, depth.Asks, 1)
	assert.Equal(t, 67993.3, depth.Asks[0].Price)
	assert.Equal(t, 1.21427407, depth.Asks[0].Volume)
}

func TestSpotWsAdapter_Subscriptions(t *testing.T) {
	t.Parallel()
	adapter := spot.NewWsAdapter()
	ctx := context.Background()

	assert.NoError(t, adapter.SubscribeDepth(ctx, "BTC-USDT"))
	assert.NoError(t, adapter.UnsubscribeDepth(ctx, "BTC-USDT"))
	assert.NoError(t, adapter.SubscribeTrade(ctx, "BTC-USDT"))
	assert.NoError(t, adapter.UnsubscribeTrade(ctx, "BTC-USDT"))
	assert.NoError(t, adapter.SubscribeTicker(ctx, "BTC-USDT"))
	assert.NoError(t, adapter.UnsubscribeTicker(ctx, "BTC-USDT"))

	pingPayload, dur := adapter.GetPingConfig()
	assert.NotNil(t, pingPayload)
	assert.Greater(t, dur, int64(0))

	assert.Nil(t, adapter.GetAuthHook("key", "secret"))

	extractor := adapter.GetChannelExtractor()
	assert.Equal(t, "depth", extractor([]byte(`{"topic":"/market/level2:BTC-USDT"}`)))
	assert.Equal(t, "trade", extractor([]byte(`{"topic":"/market/match:BTC-USDT"}`)))
	assert.Equal(t, "trade", extractor([]byte(`{"subject":"trade.l3match"}`)))
}

func TestSpotWsAdapter_ParseTrade(t *testing.T) {
	t.Parallel()
	adapter := spot.NewWsAdapter()

	raw := []byte(`{
		"topic": "/market/match:BTC-USDT",
		"type": "message",
		"subject": "trade.l3match",
		"data": {
			"makerOrderId": "671b5007389355000701b1d3",
			"price": "67523",
			"sequence": "11067996711960577",
			"side": "buy",
			"size": "0.003",
			"symbol": "BTC-USDT",
			"takerOrderId": "671b50161777ff00074c168d",
			"time": "1729843222921000000",
			"tradeId": "11067996711960577",
			"type": "match"
		}
	}`)

	sym, trades, err := adapter.ParseTrade(raw)
	require.NoError(t, err)
	assert.Equal(t, "BTC-USDT", sym)
	require.Len(t, trades, 1)

	assert.Equal(t, 67523.0, trades[0].Price)
	assert.Equal(t, 0.003, trades[0].Volume)
	assert.True(t, trades[0].Side.IsLong())
	assert.Equal(t, time.Unix(0, 1729843222921000000).UTC(), trades[0].Timestamp)

	// Sell trade
	rawSell := []byte(`{
		"topic": "/market/match:BTC-USDT",
		"type": "message",
		"subject": "trade.l3match",
		"data": {
			"side": "sell",
			"size": "1.25",
			"price": "67520.5",
			"time": "1729843222921"
		}
	}`)
	symSell, tradesSell, errSell := adapter.ParseTrade(rawSell)
	require.NoError(t, errSell)
	assert.Equal(t, "BTC-USDT", symSell)
	require.Len(t, tradesSell, 1)
	assert.Equal(t, 67520.5, tradesSell[0].Price)
	assert.Equal(t, 1.25, tradesSell[0].Volume)
	assert.False(t, tradesSell[0].Side.IsLong())
	assert.Equal(t, time.UnixMilli(1729843222921).UTC(), tradesSell[0].Timestamp)
}
