package okx_test

import (
	"context"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/okx"
	pkgws "crypto-bot/pkg/ws"
	"log/slog"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWsAdapter_GetChannelExtractor(t *testing.T) {
	t.Parallel()

	adapter := okx.NewWsAdapter("")
	extractor := adapter.GetChannelExtractor()

	assert.Equal(t, "ticker", extractor([]byte(`{"arg":{"channel":"tickers","instId":"BTC-USDT-SWAP"}}`)))
	assert.Equal(t, "kline", extractor([]byte(`{"arg":{"channel":"candle1m","instId":"BTC-USDT-SWAP"}}`)))
	assert.Equal(t, "depth", extractor([]byte(`{"arg":{"channel":"books5","instId":"BTC-USDT-SWAP"}}`)))
	assert.Equal(t, "personal.order", extractor([]byte(`{"arg":{"channel":"orders"}}`)))
	assert.Equal(t, "personal.position", extractor([]byte(`{"arg":{"channel":"positions"}}`)))
}

func TestWsAdapter_ParseTicker(t *testing.T) {
	t.Parallel()

	adapter := okx.NewWsAdapter("")
	raw := []byte(`{
		"arg": {"channel": "tickers", "instId": "BTC-USDT-SWAP"},
		"data": [
			{
				"last": "50000.5",
				"bidPx": "50000.0",
				"askPx": "50001.0",
				"volCcy24h": "50000000"
			}
		]
	}`)

	symbol, pd, err := adapter.ParseTicker(raw)
	require.NoError(t, err)
	assert.Equal(t, "BTC-USDT-SWAP", symbol)
	assert.Equal(t, 50000.5, pd.LastPrice)
	assert.Equal(t, 50000.0, pd.BestBid)
	assert.Equal(t, 50001.0, pd.BestAsk)
	assert.Equal(t, 50000000.0, pd.Volume24)
}

func TestWsAdapter_ParsePosition(t *testing.T) {
	t.Parallel()

	adapter := okx.NewWsAdapter("")

	// 1. Long position in hedge mode
	rawLong := []byte(`{
		"arg": {"channel": "positions"},
		"data": [
			{
				"instId": "BTC-USDT-SWAP",
				"pos": "1",
				"lever": "10",
				"avgPx": "50000",
				"liqPx": "45000",
				"realizedPnl": "5.5",
				"margin": "5000",
				"posSide": "long",
				"mgnMode": "isolated"
			}
		]
	}`)
	update, err := adapter.ParsePosition(rawLong)
	require.NoError(t, err)
	assert.Equal(t, "BTC-USDT-SWAP", update.Symbol)
	assert.Equal(t, 1.0, update.HoldVol)
	assert.Equal(t, 10, update.Leverage)
	assert.Equal(t, exchange.PositionTypeLong, update.PositionType)

	// 2. Short position in hedge mode
	rawShort := []byte(`{
		"arg": {"channel": "positions"},
		"data": [
			{
				"instId": "BTC-USDT-SWAP",
				"pos": "1.5",
				"lever": "10",
				"avgPx": "50000",
				"liqPx": "45000",
				"realizedPnl": "5.5",
				"margin": "5000",
				"posSide": "short",
				"mgnMode": "isolated"
			}
		]
	}`)
	update, err = adapter.ParsePosition(rawShort)
	require.NoError(t, err)
	assert.Equal(t, "BTC-USDT-SWAP", update.Symbol)
	assert.Equal(t, 1.5, update.HoldVol)
	assert.Equal(t, exchange.PositionTypeShort, update.PositionType)

	// 3. Short position in net mode (negative quantity)
	rawNetShort := []byte(`{
		"arg": {"channel": "positions"},
		"data": [
			{
				"instId": "BTC-USDT-SWAP",
				"pos": "-2.5",
				"lever": "10",
				"avgPx": "50000",
				"liqPx": "45000",
				"realizedPnl": "5.5",
				"margin": "5000",
				"posSide": "net",
				"mgnMode": "isolated"
			}
		]
	}`)
	update, err = adapter.ParsePosition(rawNetShort)
	require.NoError(t, err)
	assert.Equal(t, "BTC-USDT-SWAP", update.Symbol)
	assert.Equal(t, 2.5, update.HoldVol)
	assert.Equal(t, exchange.PositionTypeShort, update.PositionType)

	// 4. Margin position in net mode matching base currency (long)
	rawMarginLong := []byte(`{
		"arg": {"channel": "positions"},
		"data": [
			{
				"instId": "BTC-USDT",
				"pos": "0.5",
				"lever": "1",
				"avgPx": "50000",
				"liqPx": "45000",
				"realizedPnl": "5.5",
				"margin": "5000",
				"posSide": "net",
				"mgnMode": "isolated",
				"posCcy": "BTC"
			}
		]
	}`)
	update, err = adapter.ParsePosition(rawMarginLong)
	require.NoError(t, err)
	assert.Equal(t, "BTC-USDT", update.Symbol)
	assert.Equal(t, 0.5, update.HoldVol)
	assert.Equal(t, exchange.PositionTypeLong, update.PositionType)

	// 5. Margin position in net mode matching quote currency (short)
	rawMarginShort := []byte(`{
		"arg": {"channel": "positions"},
		"data": [
			{
				"instId": "BTC-USDT",
				"pos": "100.0",
				"lever": "1",
				"avgPx": "50000",
				"liqPx": "45000",
				"realizedPnl": "5.5",
				"margin": "5000",
				"posSide": "net",
				"mgnMode": "isolated",
				"posCcy": "USDT"
			}
		]
	}`)
	update, err = adapter.ParsePosition(rawMarginShort)
	require.NoError(t, err)
	assert.Equal(t, "BTC-USDT", update.Symbol)
	assert.Equal(t, 100.0, update.HoldVol)
	assert.Equal(t, exchange.PositionTypeShort, update.PositionType)
}

func TestWsAdapter_SubscriptionAndErrors(t *testing.T) {
	t.Parallel()

	adapter := okx.NewWsAdapter("")

	// 1. GetPingConfig
	pingMsg, interval := adapter.GetPingConfig()
	assert.Equal(t, "ping", pingMsg)
	assert.Equal(t, 20*time.Second, interval)

	// 2. GetAuthHook
	hook := adapter.GetAuthHook("", "")
	assert.Nil(t, hook)

	hookWithKey := adapter.GetAuthHook("my_api_key", "my_api_secret")
	assert.NotNil(t, hookWithKey)

	// 3. Subscription checks with cancelled context to cover pool routing
	pool := pkgws.NewPool("ws://127.0.0.1:1", 1, slog.Default())
	adapter.SetPool(pool)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_ = adapter.SubscribeTicker(ctx, "BTC-USDT-SWAP")
	_ = adapter.UnsubscribeTicker(ctx, "BTC-USDT-SWAP")
	_ = adapter.SubscribeKline(ctx, "BTC-USDT-SWAP")
	_ = adapter.UnsubscribeKline(ctx, "BTC-USDT-SWAP")
	_ = adapter.SubscribeDepth(ctx, "BTC-USDT-SWAP", "5")
	_ = adapter.UnsubscribeDepth(ctx, "BTC-USDT-SWAP", "5")
	_ = adapter.SubscribePersonal(ctx)

	// 6. Error parsing cases
	_, _, err := adapter.ParseTicker([]byte(`{}`))
	assert.Error(t, err)

	_, _, err = adapter.ParseTicker([]byte(`{"arg":{"instId":"BTC-USDT-SWAP"}}`))
	assert.Error(t, err)

	_, _, err = adapter.ParseTicker([]byte(`{"arg":{"instId":"BTC-USDT-SWAP"},"data":[]}`))
	assert.Error(t, err)

	_, _, err = adapter.ParseTicker([]byte(`{"arg":{"instId":"BTC-USDT-SWAP"},"data":"invalid"}`))
	assert.Error(t, err)

	_, err = adapter.ParsePosition([]byte(`{}`))
	assert.Error(t, err)

	pUpdate, err := adapter.ParsePosition([]byte(`{"data":[]}`))
	assert.NoError(t, err)
	assert.Nil(t, pUpdate)
}

func TestWsAdapter_LoginSync(t *testing.T) {
	t.Parallel()

	t.Run("Success Login Closes Authenticated Channel", func(t *testing.T) {
		t.Parallel()
		adapter := okx.NewWsAdapter("passphrase")
		extractor := adapter.GetChannelExtractor()

		// GetAuthHook with key returns non-nil hook
		hook := adapter.GetAuthHook("key", "secret")
		assert.NotNil(t, hook)

		// Before running hook or receiving login event, SubscribePersonal with short timeout context should timeout
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		err := adapter.SubscribePersonal(ctx)
		cancel()
		assert.ErrorIs(t, err, context.DeadlineExceeded)

		// Simulate receiving a successful login event response from OKX
		loginResp := []byte(`{"event":"login","code":"0","msg":""}`)
		channel := extractor(loginResp)
		assert.Equal(t, "login", channel)

		// Now SubscribePersonal should unblock instantly even with an active context
		ctx2, cancel2 := context.WithCancel(context.Background())
		// Prepare a mock private client to avoid panic during SendPrivate
		pool := pkgws.NewPool("ws://127.0.0.1:1", 1, slog.Default())
		adapter.SetPool(pool)

		err = adapter.SubscribePersonal(ctx2)
		cancel2()
		// Since the private client is nil in the pool, it will return nil (success/noop) instead of blocking
		assert.NoError(t, err)
	})

	t.Run("Empty APIKey Closes Authenticated Channel Immediately", func(t *testing.T) {
		t.Parallel()
		adapter := okx.NewWsAdapter("passphrase")

		hook := adapter.GetAuthHook("", "")
		assert.Nil(t, hook)

		// Since apiKey is empty, a.authenticated should be closed immediately
		ctx, cancel := context.WithCancel(context.Background())
		// Setup mock pool to avoid nil pointer dereference on SendPrivate
		pool := pkgws.NewPool("ws://127.0.0.1:1", 1, slog.Default())
		adapter.SetPool(pool)

		err := adapter.SubscribePersonal(ctx)
		cancel()
		assert.NoError(t, err)
	})
}

type mockClock struct {
	t time.Time
}

func (m mockClock) Now() time.Time {
	return m.t
}

func TestWsAdapter_SetClock(t *testing.T) {
	t.Parallel()

	adapter := okx.NewWsAdapter("passphrase")
	fixedTime := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	clk := mockClock{t: fixedTime}
	adapter.SetClock(clk)

	// Verify that the clock is used in ParseTicker
	raw := []byte(`{
		"arg": {"channel": "tickers", "instId": "BTC-USDT-SWAP"},
		"data": [
			{
				"last": "50000.5",
				"bidPx": "50000.0",
				"askPx": "50001.0",
				"volCcy24h": "50000000"
			}
		]
	}`)
	_, pd, err := adapter.ParseTicker(raw)
	require.NoError(t, err)
	assert.Equal(t, fixedTime, pd.UpdatedAt)
}
