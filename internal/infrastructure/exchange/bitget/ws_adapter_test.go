package bitget_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/bitget"
	pkgws "crypto-bot/pkg/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWsAdapter_GetChannelExtractor(t *testing.T) {
	t.Parallel()

	adapter := bitget.NewWsAdapter("pass")
	extractor := adapter.GetChannelExtractor()

	assert.Equal(t, "ticker", extractor([]byte(`{"arg":{"channel":"ticker","instId":"BTCUSDT"}}`)))
	assert.Equal(t, "kline", extractor([]byte(`{"arg":{"channel":"candle1m","instId":"BTCUSDT"}}`)))
	assert.Equal(t, "depth", extractor([]byte(`{"arg":{"channel":"books","instId":"BTCUSDT"}}`)))
	assert.Equal(t, "personal.order", extractor([]byte(`{"arg":{"channel":"orders"}}`)))
	assert.Equal(t, "personal.position", extractor([]byte(`{"arg":{"channel":"positions"}}`)))
	assert.Equal(t, "personal.position", extractor([]byte(`{"arg":{"channel":"positions-history"}}`)))

	// Test user's exact BIRBUSDT raw message routing
	userRaw := []byte(`{"action":"snapshot","arg":{"instType":"USDT-FUTURES","channel":"ticker","instId":"BIRBUSDT"},"data":[{"instId":"BIRBUSDT","lastPr":"0.07623","bidPr":"0.07615","askPr":"0.0762","bidSz":"1863","askSz":"1290","open24h":"0.08453","high24h":"0.10126","low24h":"0.07447","change24h":"-0.09819","fundingRate":"-0.006439","nextFundingTime":"1783080000000","markPrice":"0.07628","indexPrice":"0.0774530536255575","holdingAmount":"23439001","baseVolume":"381989307","quoteVolume":"33328445.00488","openUtc":"0.09095","symbolType":"1","symbol":"BIRBUSDT","deliveryPrice":"0","ts":"1783066672529"}],"ts":1783066672529}`)
	assert.Equal(t, "ticker", extractor(userRaw))

	// Test user's exact subscribe confirmation message (should return "")
	subConfirmRaw := []byte(`{"event":"subscribe","arg":{"instType":"USDT-FUTURES","channel":"positions-history","instId":"default"}}`)
	assert.Equal(t, "", extractor(subConfirmRaw))
}

func TestWsAdapter_ParseTicker(t *testing.T) {
	t.Parallel()

	adapter := bitget.NewWsAdapter("pass")
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

	// Test case with actual User payload for BIRBUSDT
	userRaw := []byte(`{"action":"snapshot","arg":{"instType":"USDT-FUTURES","channel":"ticker","instId":"BIRBUSDT"},"data":[{"instId":"BIRBUSDT","lastPr":"0.07623","bidPr":"0.07615","askPr":"0.0762","bidSz":"1863","askSz":"1290","open24h":"0.08453","high24h":"0.10126","low24h":"0.07447","change24h":"-0.09819","fundingRate":"-0.006439","nextFundingTime":"1783080000000","markPrice":"0.07628","indexPrice":"0.0774530536255575","holdingAmount":"23439001","baseVolume":"381989307","quoteVolume":"33328445.00488","openUtc":"0.09095","symbolType":"1","symbol":"BIRBUSDT","deliveryPrice":"0","ts":"1783066672529"}],"ts":1783066672529}`)
	symbolUser, pdUser, errUser := adapter.ParseTicker(userRaw)
	require.NoError(t, errUser)
	assert.Equal(t, "BIRBUSDT", symbolUser)
	assert.Equal(t, 0.07623, pdUser.LastPrice)
	assert.Equal(t, 0.07615, pdUser.BestBid)
	assert.Equal(t, 0.0762, pdUser.BestAsk)
	assert.Equal(t, 381989307.0, pdUser.Volume24)

	// Test control event (unsubscribe confirmation) skipping (should return "", nil, nil)
	unsubRaw := []byte(`{"event":"unsubscribe","arg":{"instType":"USDT-FUTURES","channel":"ticker","instId":"BIRBUSDT"}}`)
	symbolUnsub, pdUnsub, errUnsub := adapter.ParseTicker(unsubRaw)
	require.NoError(t, errUnsub)
	assert.Empty(t, symbolUnsub)
	assert.Nil(t, pdUnsub)
}

func TestWsAdapter_ParsePosition(t *testing.T) {
	t.Parallel()

	adapter := bitget.NewWsAdapter("pass")
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
	assert.Equal(t, exchange.PositionTypeLong, update.PositionType)

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
	assert.Equal(t, exchange.PositionTypeShort, updateFallback.PositionType)

	// Test positions-history parsing (total closed positions)
	rawHistory := []byte(`{"action":"update","arg":{"instType":"USDT-FUTURES","channel":"positions-history","instId":"default"},"data":[{"posId":"1456862048120639491","instId":"BIRBUSDT","marginCoin":"USDT","marginMode":"fixed","holdSide":"short","posMode":"hedge_mode","openPriceAvg":"0.07637","closePriceAvg":"0.07651","openSize":"196","closeSize":"196","achievedProfits":"-0.02679000","settleFee":"0","openFee":"-0.00898111","closeFee":"-0.00899718","cTime":"1783068900136","uTime":"1783068926572"}],"ts":1783068926581}`)

	updateHistory, err := adapter.ParsePosition(rawHistory)
	require.NoError(t, err)
	assert.Equal(t, "BIRBUSDT", updateHistory.Symbol)
	assert.Equal(t, 0.0, updateHistory.HoldVol)
	assert.Equal(t, exchange.PositionTypeShort, updateHistory.PositionType)
	assert.Equal(t, 0.07637, updateHistory.OpenAvgPrice)
	assert.Equal(t, 0.07637, updateHistory.HoldAvgPrice)
	assert.Equal(t, 196.0, updateHistory.CloseVol)
	assert.Equal(t, 0.07651, updateHistory.CloseAvgPrice)
	assert.Equal(t, -0.02679000, updateHistory.CloseProfitLoss)
	assert.InDelta(t, -0.01797829, updateHistory.Fee, 1e-9) // openFee + closeFee = -0.00898111 + -0.00899718 = -0.01797829
	assert.Equal(t, 0.0, updateHistory.HoldFee)
	assert.Equal(t, int64(1783068926572), updateHistory.UpdateTime)

	// Test empty positions list (should return nil, nil without error)
	rawEmptyPos := []byte(`{
		"arg": {"channel": "positions"},
		"data": []
	}`)
	updateEmptyPos, errEmptyPos := adapter.ParsePosition(rawEmptyPos)
	require.NoError(t, errEmptyPos)
	assert.Nil(t, updateEmptyPos)

	// Test empty positions-history list (should return nil, nil without error)
	rawEmptyHistory := []byte(`{"action":"snapshot","arg":{"instType":"USDT-FUTURES","channel":"positions-history","instId":"default"},"data":[],"ts":1783066903146}`)
	updateEmptyHistory, errEmptyHistory := adapter.ParsePosition(rawEmptyHistory)
	require.NoError(t, errEmptyHistory)
	assert.Nil(t, updateEmptyHistory)
	// Test control event (subscribe confirmation) skipping (should return nil, nil)
	rawConfirm := []byte(`{"event":"subscribe","arg":{"instType":"USDT-FUTURES","channel":"positions-history","instId":"default"}}`)
	updateConfirm, errConfirm := adapter.ParsePosition(rawConfirm)
	require.NoError(t, errConfirm)
	assert.Nil(t, updateConfirm)
}

func TestWsAdapter_OtherMethods(t *testing.T) {
	t.Parallel()

	adapter := bitget.NewWsAdapter("pass")

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

	adapter := bitget.NewWsAdapter("pass")

	// Using a dummy pool with a pre-canceled context so that pool methods return immediately without trying to connect.
	pool := pkgws.NewPool("ws://127.0.0.1:1", 30, slog.Default())
	defer pool.Close()
	adapter.SetPool(pool)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_ = adapter.SubscribeTicker(ctx, "BTCUSDT")
	_ = adapter.UnsubscribeTicker(ctx, "BTCUSDT")
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
	assert.Equal(t, exchange.PositionTypeShort, update.PositionType) // Short

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
	assert.Equal(t, exchange.PositionTypeLong, update.PositionType) // Long
}

func TestWsAdapter_LoginSync(t *testing.T) {
	t.Parallel()

	t.Run("Success Login Closes Authenticated Channel", func(t *testing.T) {
		t.Parallel()
		adapter := bitget.NewWsAdapter("pass")
		extractor := adapter.GetChannelExtractor()

		// GetAuthHook with key returns non-nil hook
		hook := adapter.GetAuthHook("key", "secret")
		assert.NotNil(t, hook)

		// Before running hook or receiving login event, SubscribePersonal with short timeout context should timeout
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		err := adapter.SubscribePersonal(ctx)
		cancel()
		assert.ErrorIs(t, err, context.DeadlineExceeded)

		// Simulate receiving a successful login event response from Bitget
		loginResp := []byte(`{"event":"login","code":"0"}`)
		channel := extractor(loginResp)
		assert.Equal(t, "login", channel)

		// Now SubscribePersonal should unblock instantly even with an active context
		ctx2, cancel2 := context.WithCancel(context.Background())
		// Prepare a mock private client to avoid panic during SendPrivate
		pool := pkgws.NewPool("ws://127.0.0.1:1", 1, nil)
		adapter.SetPool(pool)

		err = adapter.SubscribePersonal(ctx2)
		cancel2()
		// Since the private client is nil in the pool, it will return nil (success/noop) instead of blocking
		assert.NoError(t, err)
	})

	t.Run("Empty APIKey Closes Authenticated Channel Immediately", func(t *testing.T) {
		t.Parallel()
		adapter := bitget.NewWsAdapter("pass")

		hook := adapter.GetAuthHook("", "")
		assert.Nil(t, hook)

		// Since apiKey is empty, a.authenticated should be closed immediately
		ctx, cancel := context.WithCancel(context.Background())
		pool := pkgws.NewPool("ws://127.0.0.1:1", 1, nil)
		adapter.SetPool(pool)

		err := adapter.SubscribePersonal(ctx)
		cancel()
		assert.NoError(t, err)
	})
}
