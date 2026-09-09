package futures_test

import (
	"context"
	"testing"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	futures "crypto-bot/internal/infrastructure/exchange/bybit/futures"
	pkgws "crypto-bot/pkg/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWsAdapter_ParsePositionBybitSchema(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"id": "1003076014fb7eedb-c7e6-45d6-a8c1-270f0169171a",
		"topic": "position",
		"creationTime": 1697682317044,
		"data": [{
			"positionIdx": 2,
			"tradeMode": 0,
			"riskId": 1,
			"riskLimitValue": "2000000",
			"symbol": "BTCUSDT",
			"side": "",
			"size": "0",
			"entryPrice": "0",
			"leverage": "10",
			"breakEvenPrice":"93556.73034991",
			"positionValue": "0",
			"positionBalance": "0",
			"markPrice": "28184.5",
			"positionIM": "0",
			"positionIMByMp": "0",
			"positionMM": "0",
			"positionMMByMp": "0",
			"takeProfit": "0",
			"stopLoss": "0",
			"trailingStop": "0",
			"unrealisedPnl": "0",
			"curRealisedPnl": "1.26",
			"cumRealisedPnl": "-25.06579337",
			"sessionAvgPrice": "0",
			"createdTime": "1694402496913",
			"updatedTime": "1697682317038",
			"tpslMode": "Full",
			"liqPrice": "0",
			"bustPrice": "",
			"category": "linear",
			"positionStatus": "Normal",
			"adlRankIndicator": 0,
			"autoAddMargin": 0,
			"leverageSysUpdatedTime": "",
			"mmrSysUpdatedTime": "",
			"seq": 8327597863,
			"isReduceOnly": false
		}]
	}`)

	pos, err := futures.NewWsAdapter().ParsePosition(raw)
	require.NoError(t, err)
	require.NotNil(t, pos)

	assert.Equal(t, "BTCUSDT", pos.Symbol)
	assert.Equal(t, 0.0, pos.HoldVolCoin)
	assert.Equal(t, exchange.PositionTypeShort, pos.PositionType)
	assert.Equal(t, 10, pos.Leverage)
	assert.Equal(t, 1.26, pos.CloseProfitLoss)
	assert.Equal(t, int64(1697682317038), pos.UpdateTime)
}

func TestWsAdapter_ParsePositionAvgPriceFallback(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"topic": "position",
		"data": [{
			"symbol": "IDUSDT",
			"side": "Sell",
			"size": "497",
			"avgPrice": "0.03016",
			"leverage": "10",
			"positionIdx": 2,
			"positionValue": "14.99946",
			"positionIM": "1.50802066",
			"curRealisedPnl": "-0.00824424"
		}]
	}`)

	pos, err := futures.NewWsAdapter().ParsePosition(raw)
	require.NoError(t, err)
	require.NotNil(t, pos)

	assert.Equal(t, "IDUSDT", pos.Symbol)
	assert.Equal(t, 497.0, pos.HoldVolCoin)
	assert.Equal(t, 0.03016, pos.HoldAvgPrice)
	assert.Equal(t, 0.03016, pos.OpenAvgPrice)
	assert.Equal(t, exchange.PositionTypeShort, pos.PositionType)
}

func TestWsAdapter_ParsePositionSelectsActiveRow(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"topic": "position",
		"data": [
			{"symbol": "BTCUSDT", "side": "", "size": "0", "positionIdx": 1},
			{"symbol": "BTCUSDT", "side": "Sell", "size": "2", "positionIdx": 2, "entryPrice": "60000"}
		]
	}`)

	pos, err := futures.NewWsAdapter().ParsePosition(raw)
	require.NoError(t, err)
	require.NotNil(t, pos)

	assert.Equal(t, 2.0, pos.HoldVolCoin)
	assert.Equal(t, exchange.PositionTypeShort, pos.PositionType)
	assert.Equal(t, 60000.0, pos.HoldAvgPrice)
}

func TestWsAdapter_ParsePositionSelectsRecentlyClosedRow(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"topic": "position",
		"data": [
			{"symbol": "AIGENSYNUSDT", "side": "", "size": "0", "positionIdx": 1, "updatedTime": "1780040399979"},
			{"symbol": "AIGENSYNUSDT", "side": "", "size": "0", "positionIdx": 2, "updatedTime": "1780042885945", "cumRealisedPnl": "0.00567"}
		]
	}`)

	pos, err := futures.NewWsAdapter().ParsePosition(raw)
	require.NoError(t, err)
	require.NotNil(t, pos)

	assert.Equal(t, 0.0, pos.HoldVolCoin)
	assert.Equal(t, exchange.PositionTypeShort, pos.PositionType)
	assert.Equal(t, 0.00567, pos.CloseProfitLoss)
}

func TestWsAdapter_LoginSync(t *testing.T) {
	t.Parallel()

	t.Run("Success Login Closes Authenticated Channel", func(t *testing.T) {
		t.Parallel()
		adapter := futures.NewWsAdapter()
		extractor := adapter.GetChannelExtractor()

		hook := adapter.GetAuthHook("key", "secret")
		assert.NotNil(t, hook)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		err := adapter.SubscribePersonal(ctx)
		cancel()
		assert.ErrorIs(t, err, context.DeadlineExceeded)

		loginResp := []byte(`{"op":"auth","retCode":0,"retMsg":"OK"}`)
		channel := extractor(loginResp)
		assert.Equal(t, "", channel)

		ctx2, cancel2 := context.WithCancel(context.Background())
		pool := pkgws.NewPool("ws://127.0.0.1:1", 1, nil)
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
		pool := pkgws.NewPool("ws://127.0.0.1:1", 1, nil)
		adapter.SetPool(pool)

		err := adapter.SubscribePersonal(ctx)
		cancel()
		assert.NoError(t, err)
	})
}

func TestWsAdapter_TradeSubscription(t *testing.T) {
	t.Parallel()

	adapter := futures.NewWsAdapter()
	ctx := context.Background()

	// With nil pool, methods return nil gracefully
	assert.NoError(t, adapter.SubscribeTrade(ctx, "BTCUSDT"))
	assert.NoError(t, adapter.UnsubscribeTrade(ctx, "BTCUSDT"))
	assert.NoError(t, adapter.SubscribeDepth(ctx, "BTCUSDT"))
	assert.NoError(t, adapter.UnsubscribeDepth(ctx, "BTCUSDT"))
	assert.NoError(t, adapter.SubscribeTicker(ctx, "BTCUSDT"))
	assert.NoError(t, adapter.UnsubscribeTicker(ctx, "BTCUSDT"))
}

func TestWsAdapter_ChannelExtractor(t *testing.T) {
	t.Parallel()

	adapter := futures.NewWsAdapter()
	extractor := adapter.GetChannelExtractor()

	assert.Equal(t, "ticker", extractor([]byte(`{"topic":"tickers.BTCUSDT"}`)))
	assert.Equal(t, "trade", extractor([]byte(`{"topic":"publicTrade.BTCUSDT"}`)))
	assert.Equal(t, "depth", extractor([]byte(`{"topic":"orderbook.50.BTCUSDT"}`)))
	assert.Equal(t, "kline", extractor([]byte(`{"topic":"kline.1.BTCUSDT"}`)))
	assert.Equal(t, "personal.order", extractor([]byte(`{"topic":"order"}`)))
	assert.Equal(t, "personal.position", extractor([]byte(`{"topic":"position"}`)))
	assert.Equal(t, "", extractor([]byte(`invalid json`)))
}

func TestWsAdapter_ParseTrade(t *testing.T) {
	t.Parallel()

	adapter := futures.NewWsAdapter()

	t.Run("Multiple Trades Snapshot", func(t *testing.T) {
		t.Parallel()
		raw := []byte(`{
			"topic": "publicTrade.BTCUSDT",
			"type": "snapshot",
			"ts": 1672304486868,
			"data": [
				{
					"T": 1672304486865,
					"s": "BTCUSDT",
					"S": "Buy",
					"v": "0.001",
					"p": "16578.50",
					"L": "PlusTick",
					"i": "20f43950-d8dd-5b31-9112-a178eb6023af",
					"BT": false,
					"seq": 1783284617
				},
				{
					"T": 1672304486870,
					"s": "BTCUSDT",
					"S": "Sell",
					"v": "0.005",
					"p": "16578.00",
					"L": "MinusTick",
					"i": "20f43950-d8dd-5b31-9112-a178eb6023b0",
					"BT": false,
					"seq": 1783284618
				}
			]
		}`)

		sym, trades, err := adapter.ParseTrade(raw)
		require.NoError(t, err)
		assert.Equal(t, "BTCUSDT", sym)
		require.Len(t, trades, 2)

		// Trade 1: Taker Buy
		assert.Equal(t, "BTCUSDT", trades[0].Symbol)
		assert.Equal(t, 16578.50, trades[0].Price)
		assert.Equal(t, 0.001, trades[0].Volume)
		assert.Equal(t, domain.SideOpenLong, trades[0].Side)
		assert.Equal(t, time.UnixMilli(1672304486865).UTC(), trades[0].Timestamp)

		// Trade 2: Taker Sell
		assert.Equal(t, "BTCUSDT", trades[1].Symbol)
		assert.Equal(t, 16578.00, trades[1].Price)
		assert.Equal(t, 0.005, trades[1].Volume)
		assert.Equal(t, domain.SideOpenShort, trades[1].Side)
		assert.Equal(t, time.UnixMilli(1672304486870).UTC(), trades[1].Timestamp)
	})

	t.Run("Single Trade Entry", func(t *testing.T) {
		t.Parallel()
		raw := []byte(`{
			"topic": "publicTrade.ETHUSDT",
			"type": "snapshot",
			"ts": 1672304486900,
			"data": {
				"T": 1672304486899,
				"s": "ETHUSDT",
				"S": "Buy",
				"v": "1.25",
				"p": "1200.50",
				"i": "trade-id-1"
			}
		}`)

		sym, trades, err := adapter.ParseTrade(raw)
		require.NoError(t, err)
		assert.Equal(t, "ETHUSDT", sym)
		require.Len(t, trades, 1)
		assert.Equal(t, 1200.50, trades[0].Price)
		assert.Equal(t, 1.25, trades[0].Volume)
		assert.Equal(t, domain.SideOpenLong, trades[0].Side)
	})

	t.Run("Empty Data List", func(t *testing.T) {
		t.Parallel()
		raw := []byte(`{
			"topic": "publicTrade.SOLUSDT",
			"type": "snapshot",
			"ts": 1672304486900,
			"data": []
		}`)

		sym, trades, err := adapter.ParseTrade(raw)
		require.NoError(t, err)
		assert.Equal(t, "SOLUSDT", sym)
		assert.Empty(t, trades)
	})

	t.Run("Invalid Payload", func(t *testing.T) {
		t.Parallel()
		_, _, err := adapter.ParseTrade([]byte(`invalid json`))
		assert.Error(t, err)
	})
}
