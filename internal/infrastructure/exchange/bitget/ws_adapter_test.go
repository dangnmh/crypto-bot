package bitget_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/exchange/bitget"
	pkgws "crypto-bot/pkg/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWsAdapter_GetChannelExtractor(t *testing.T) {
	t.Parallel()

	adapter := bitget.NewWsAdapter()
	extractor := adapter.GetChannelExtractor()

	assert.Equal(t, "tickers", extractor([]byte(`{"arg":{"channel":"ticker","instId":"BTCUSDT"}}`)))
	assert.Equal(t, "kline", extractor([]byte(`{"arg":{"channel":"candle1m","instId":"BTCUSDT"}}`)))
	assert.Equal(t, "depth", extractor([]byte(`{"arg":{"channel":"books","instId":"BTCUSDT"}}`)))
	assert.Equal(t, "personal.order", extractor([]byte(`{"arg":{"channel":"orders"}}`)))
	assert.Equal(t, "personal.position", extractor([]byte(`{"arg":{"channel":"positions"}}`)))
	assert.Equal(t, "personal.position", extractor([]byte(`{"arg":{"channel":"positions-history"}}`)))
}

func TestWsAdapter_ParseTicker(t *testing.T) {
	t.Parallel()

	adapter := bitget.NewWsAdapter()
	raw := []byte(`{
		"arg": {"channel": "ticker", "instId": "BTCUSDT"},
		"data": [
			{
				"symbol": "BTCUSDT",
				"lastPr": "50000.5",
				"bidPr": "50000.0",
				"askPr": "50001.0",
				"baseVolume": "1000"
			}
		]
	}`)

	symbol, pd, err := adapter.ParseTicker(raw)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", symbol)
	assert.Equal(t, 50000.5, pd.LastPrice)
	assert.Equal(t, 50000.0, pd.BestBid)
	assert.Equal(t, 50001.0, pd.BestAsk)
	assert.Equal(t, 1000.0, pd.Volume24)
}

func TestWsAdapter_ParsePosition(t *testing.T) {
	t.Parallel()

	adapter := bitget.NewWsAdapter()
	raw := []byte(`{
		"arg": {"channel": "positions"},
		"data": [
			{
				"symbol": "BTCUSDT",
				"total": "1",
				"leverage": "10",
				"openPriceAvg": "50000",
				"liquidationPrice": "45000",
				"achievedProfits": "5.5",
				"marginSize": "5000",
				"holdSide": "long",
				"marginMode": "crossed"
			}
		]
	}`)

	update, err := adapter.ParsePosition(raw)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", update.Symbol)
	assert.Equal(t, 1.0, update.HoldVol)
	assert.Equal(t, 10, update.Leverage)
	assert.Equal(t, 1, update.PositionType)

	// Test fallback to instId if symbol is empty
	rawFallback := []byte(`{
		"arg": {"channel": "positions"},
		"data": [
			{
				"instId": "ETHUSDT",
				"total": "1.5",
				"leverage": "20",
				"openPriceAvg": "1900",
				"liquidationPrice": "1500",
				"achievedProfits": "0",
				"marginSize": "9.5",
				"holdSide": "short",
				"marginMode": "crossed"
			}
		]
	}`)

	updateFallback, err := adapter.ParsePosition(rawFallback)
	require.NoError(t, err)
	assert.Equal(t, "ETHUSDT", updateFallback.Symbol)
	assert.Equal(t, 1.5, updateFallback.HoldVol)
	assert.Equal(t, 20, updateFallback.Leverage)
	assert.Equal(t, 2, updateFallback.PositionType)

	// Test positions-history parsing (total closed positions)
	rawHistory := []byte(`{
		"arg": {"channel": "positions-history", "instType": "USDT-FUTURES", "instId": "default"},
		"data": [
			{
				"posId": "1",
				"instId": "BTCUSDT",
				"marginCoin": "USDT",
				"marginMode": "crossed",
				"holdSide": "short",
				"posMode": "one_way_mode",
				"openPriceAvg": "20000.0",
				"closePriceAvg": "26221.0",
				"openSize": "0.010",
				"closeSize": "0.010",
				"achievedProfits": "-62.21000000",
				"settleFee": "-0.02277989",
				"openFee": "-0.12000000",
				"closeFee": "-0.15732600",
				"cTime": "1696907951177",
				"uTime": "1697090609976"
			}
		]
	}`)

	updateHistory, err := adapter.ParsePosition(rawHistory)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", updateHistory.Symbol)
	assert.Equal(t, 0.0, updateHistory.HoldVol)
	assert.Equal(t, 2, updateHistory.PositionType)
	assert.Equal(t, 20000.0, updateHistory.OpenAvgPrice)
	assert.Equal(t, 20000.0, updateHistory.HoldAvgPrice)
	assert.Equal(t, 0.010, updateHistory.CloseVol)
	assert.Equal(t, 26221.0, updateHistory.CloseAvgPrice)
	assert.Equal(t, -62.21000000, updateHistory.CloseProfitLoss)
	assert.InDelta(t, -0.277326, updateHistory.Fee, 1e-9) // openFee + closeFee = -0.12 + -0.157326 = -0.277326
	assert.Equal(t, -0.02277989, updateHistory.HoldFee)
	assert.Equal(t, int64(1697090609976), updateHistory.UpdateTime)
}

func TestWsAdapter_OtherMethods(t *testing.T) {
	t.Parallel()

	adapter := bitget.NewWsAdapter()

	// 1. GetPingConfig
	pingMsg, interval := adapter.GetPingConfig()
	assert.Equal(t, "ping", pingMsg)
	assert.Equal(t, 30*time.Second, interval)

	// 2. GetAuthHook
	hook := adapter.GetAuthHook("apiKey", "apiSecret")
	assert.NotNil(t, hook)

	hookNil := adapter.GetAuthHook("", "")
	assert.Nil(t, hookNil)

	// 5. Extractor other cases
	extractor := adapter.GetChannelExtractor()
	assert.Equal(t, "pong", extractor([]byte("pong")))
	assert.Equal(t, "", extractor([]byte(`{}`)))
	assert.Equal(t, "custom", extractor([]byte(`{"arg":{"channel":"custom"}}`)))

	// 6. ParseTicker errors
	_, _, err := adapter.ParseTicker([]byte(`{}`))
	assert.Error(t, err)

	_, _, err = adapter.ParseTicker([]byte(`{"arg":{"instId":"BTCUSDT"}, "data":[]}`))
	assert.Error(t, err)

	// 10. ParsePosition errors
	_, err = adapter.ParsePosition([]byte(`{}`))
	assert.Error(t, err)

	_, err = adapter.ParsePosition([]byte(`{"data":[]}`))
	assert.Error(t, err)
}

func TestWsAdapter_SubscriptionsAndAdditionalFeatures(t *testing.T) {
	t.Parallel()

	adapter := bitget.NewWsAdapter()

	// Using a dummy pool with a pre-canceled context so that pool methods return immediately without trying to connect.
	pool := pkgws.NewPool("ws://127.0.0.1:1", 30, slog.Default())
	defer pool.Close()
	adapter.SetPool(pool)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// 1. Test subscriptions (should return immediately with canceled context)
	_ = adapter.SubscribeTicker(ctx, "BTCUSDT")
	_ = adapter.UnsubscribeTicker(ctx, "BTCUSDT")
	_ = adapter.SubscribeKline(ctx, "BTCUSDT")
	_ = adapter.UnsubscribeKline(ctx, "BTCUSDT")
	_ = adapter.SubscribeDepth(ctx, "BTCUSDT", "1")
	_ = adapter.UnsubscribeDepth(ctx, "BTCUSDT", "1")
	_ = adapter.SubscribePersonal(ctx)

	// 2. Test GetAuthHook with apiKey & secret (valid and invalid paths)
	hookWithKey := adapter.GetAuthHook("apiKey", "apiSecret")
	assert.NotNil(t, hookWithKey)

	// 4. Test ParsePosition with crossed and isolated margin modes
	rawCrossed := []byte(`{
		"arg": {"channel": "positions"},
		"data": [
			{
				"symbol": "BTCUSDT",
				"total": "1",
				"leverage": "10",
				"openPriceAvg": "50000",
				"liquidationPrice": "45000",
				"achievedProfits": "5.5",
				"marginSize": "5000",
				"holdSide": "short",
				"marginMode": "crossed"
			}
		]
	}`)
	update, err := adapter.ParsePosition(rawCrossed)
	require.NoError(t, err)
	assert.Equal(t, 2, update.PositionType) // Short

	rawIsolated := []byte(`{
		"arg": {"channel": "positions"},
		"data": [
			{
				"symbol": "BTCUSDT",
				"total": "1",
				"leverage": "10",
				"openPriceAvg": "50000",
				"liquidationPrice": "45000",
				"achievedProfits": "5.5",
				"marginSize": "5000",
				"holdSide": "long",
				"marginMode": "isolated"
			}
		]
	}`)
	update, err = adapter.ParsePosition(rawIsolated)
	require.NoError(t, err)
	assert.Equal(t, 1, update.PositionType) // Long
}
