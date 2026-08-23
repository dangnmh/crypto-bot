package futures_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/mexc/futures"
	pkgws "crypto-bot/pkg/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFuturesWsAdapter_ParseDepth(t *testing.T) {
	t.Parallel()
	adapter := futures.NewWsAdapter()

	// 1. Basic depth message
	raw := []byte(`{
		"channel": "push.depth.full",
		"symbol": "BTC_USDT",
		"data": {
			"bids": [[60000.5, 1, 1.2], [60000.0, 1, 3.4]],
			"asks": [[60001.0, 1, 0.5], [60001.5, 1, 2.1]],
			"version": 123456
		},
		"ts": 1600000000000
	}`)

	sym, ob, err := adapter.ParseDepth(raw)
	require.NoError(t, err)
	assert.Equal(t, "BTC_USDT", sym)
	require.NotNil(t, ob)
	assert.Equal(t, int64(123456), ob.Version)
	require.Len(t, ob.Bids, 2)
	assert.Equal(t, 60000.5, ob.Bids[0].Price)
	assert.Equal(t, 1.2, ob.Bids[0].Volume) // Contract quantity at index 2
	require.Len(t, ob.Asks, 2)
	assert.Equal(t, 60001.0, ob.Asks[0].Price)
	assert.Equal(t, 0.5, ob.Asks[0].Volume)

	// 2. 3-element format [price, orderCount, quantity] with begin and end versions
	raw3Elem := []byte(`{
		"channel": "push.depth",
		"symbol": "BTC_USDT",
		"data": {
			"begin": 40949478001,
			"end": 40949478038,
			"version": 40949478038,
			"cts": 1787301607692,
			"asks": [
				[77515.4, 16284, 2.5]
			],
			"bids": [
				[77506.4, 5969, 5.0]
			]
		},
		"ts": 1787301607702
	}`)
	sym4, ob4, err := adapter.ParseDepth(raw3Elem)
	require.NoError(t, err)
	assert.Equal(t, "BTC_USDT", sym4)
	require.NotNil(t, ob4)
	assert.Equal(t, int64(40949478001), ob4.FirstVersion)
	assert.Equal(t, int64(40949478038), ob4.Version)
	require.Len(t, ob4.Bids, 1)
	assert.Equal(t, 77506.4, ob4.Bids[0].Price)
	assert.Equal(t, 5.0, ob4.Bids[0].Volume)
	require.Len(t, ob4.Asks, 1)
	assert.Equal(t, 77515.4, ob4.Asks[0].Price)
	assert.Equal(t, 2.5, ob4.Asks[0].Volume)

	// 3. Invalid JSON
	_, _, err = adapter.ParseDepth([]byte(`invalid`))
	require.Error(t, err)
}

func TestFuturesWsAdapter_ParseTicker(t *testing.T) {
	t.Parallel()
	adapter := futures.NewWsAdapter()

	tickerMsg := []byte(`{
		"channel": "push.ticker",
		"symbol": "BTC_USDT",
		"data": {
			"symbol": "BTC_USDT",
			"lastPrice": 60000.5,
			"bid1": 60000.0,
			"ask1": 60001.0,
			"fairPrice": 60000.2,
			"volume24": 1500000.0
		}
	}`)
	sym, pd, err := adapter.ParseTicker(tickerMsg)
	require.NoError(t, err)
	assert.Equal(t, "BTC_USDT", sym)
	assert.Equal(t, 60000.5, pd.LastPrice)
	assert.Equal(t, 60000.0, pd.BestBid)
	assert.Equal(t, 60001.0, pd.BestAsk)
	assert.Equal(t, 60000.2, pd.FairPrice)
	assert.Equal(t, 1500000.0, pd.Volume24)

	// Fallback when bid1/ask1 are 0
	fallbackMsg := []byte(`{
		"channel": "push.ticker",
		"symbol": "ETH_USDT",
		"data": {
			"lastPrice": 3000.0,
			"bid1": 0.0,
			"ask1": 0.0,
			"maxBidPrice": 2999.0,
			"minAskPrice": 3001.0
		}
	}`)
	sym2, pd2, err := adapter.ParseTicker(fallbackMsg)
	require.NoError(t, err)
	assert.Equal(t, "ETH_USDT", sym2)
	assert.Equal(t, 2999.0, pd2.BestBid)
	assert.Equal(t, 3001.0, pd2.BestAsk)

	// Errors
	_, _, err = adapter.ParseTicker([]byte(`invalid`))
	require.Error(t, err)
}

func TestFuturesWsAdapter_ParsePosition(t *testing.T) {
	t.Parallel()
	adapter := futures.NewWsAdapter()

	posPayload := []byte(`{
		"channel": "push.personal.position",
		"data": {
			"positionId": 123456,
			"symbol": "BTC_USDT",
			"positionType": 1,
			"holdVol": 10.0,
			"openAvgPrice": 50000.0,
			"holdAvgPrice": 50100.0,
			"realized": 25.5,
			"leverage": 20,
			"liquidatePrice": 45000.0,
			"updateTime": 1670000000000
		}
	}`)
	pos, err := adapter.ParsePosition(posPayload)
	require.NoError(t, err)
	assert.Equal(t, "BTC_USDT", pos.Symbol)
	assert.Equal(t, 10.0, pos.HoldVolContract)
	assert.Equal(t, exchange.PositionType(1), pos.PositionType)
	assert.Equal(t, 50000.0, pos.OpenAvgPrice)
	assert.Equal(t, 50100.0, pos.HoldAvgPrice)
	assert.Equal(t, 25.5, pos.CloseProfitLoss)
	assert.Equal(t, 20, pos.Leverage)
	assert.Equal(t, 45000.0, pos.LiquidatePrice)
	assert.Equal(t, int64(1670000000000), pos.UpdateTime)

	// Invalid
	_, err = adapter.ParsePosition([]byte(`invalid`))
	require.Error(t, err)
}

func TestFuturesWsAdapter_LoginSync(t *testing.T) {
	t.Parallel()

	t.Run("Success Login Closes Authenticated Channel", func(t *testing.T) {
		t.Parallel()
		adapter := futures.NewWsAdapter()
		extractor := adapter.GetChannelExtractor()

		hook := adapter.GetAuthHook("key", "secret")
		assert.NotNil(t, hook)

		// Before receiving login response, SubscribePersonal with short timeout should timeout
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		err := adapter.SubscribePersonal(ctx)
		cancel()
		assert.ErrorIs(t, err, context.DeadlineExceeded)

		// Simulate receiving a successful login event response from MEXC
		loginResp := []byte(`{"channel":"rs.login","data":"success"}`)
		channel := extractor(loginResp)
		assert.Equal(t, "login", channel)

		// Now SubscribePersonal should unblock instantly
		ctx2, cancel2 := context.WithCancel(context.Background())
		pool := pkgws.NewPool("ws://127.0.0.1:1", 1, slog.Default())
		adapter.SetPool(pool)

		err = adapter.SubscribePersonal(ctx2)
		cancel2()
		assert.NoError(t, err)
	})

	t.Run("Empty APIKey Closes Authenticated Channel Immediately", func(t *testing.T) {
		t.Parallel()
		adapter := futures.NewWsAdapter()

		hook := adapter.GetAuthHook("", "")
		assert.Nil(t, hook)

		ctx, cancel := context.WithCancel(context.Background())
		pool := pkgws.NewPool("ws://127.0.0.1:1", 1, slog.Default())
		adapter.SetPool(pool)

		err := adapter.SubscribePersonal(ctx)
		cancel()
		assert.NoError(t, err)
	})
}

func TestFuturesWsAdapter_Subscriptions(t *testing.T) {
	t.Parallel()
	adapter := futures.NewWsAdapter()
	ctx := context.Background()

	assert.NoError(t, adapter.SubscribeDepth(ctx, "BTC_USDT"))
	assert.NoError(t, adapter.UnsubscribeDepth(ctx, "BTC_USDT"))
	assert.NoError(t, adapter.SubscribeTicker(ctx, "BTC_USDT"))
	assert.NoError(t, adapter.UnsubscribeTicker(ctx, "BTC_USDT"))

	pingPayload, dur := adapter.GetPingConfig()
	assert.NotNil(t, pingPayload)
	assert.Greater(t, dur, int64(0))

	assert.NoError(t, adapter.UnsubscribePersonal(ctx))
}
