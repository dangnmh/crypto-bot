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

	adapter := okx.NewWsAdapter()
	extractor := adapter.GetChannelExtractor()

	assert.Equal(t, "tickers", extractor([]byte(`{"arg":{"channel":"tickers","instId":"BTC-USDT-SWAP"}}`)))
	assert.Equal(t, "kline", extractor([]byte(`{"arg":{"channel":"candle1m","instId":"BTC-USDT-SWAP"}}`)))
	assert.Equal(t, "depth", extractor([]byte(`{"arg":{"channel":"books5","instId":"BTC-USDT-SWAP"}}`)))
	assert.Equal(t, "personal.order", extractor([]byte(`{"arg":{"channel":"orders"}}`)))
	assert.Equal(t, "personal.position", extractor([]byte(`{"arg":{"channel":"positions"}}`)))
}

func TestWsAdapter_ParseTicker(t *testing.T) {
	t.Parallel()

	adapter := okx.NewWsAdapter()
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

func TestWsAdapter_ParseDepth(t *testing.T) {
	t.Parallel()

	adapter := okx.NewWsAdapter()
	raw := []byte(`{
		"arg": {"channel": "books5", "instId": "BTC-USDT-SWAP"},
		"data": [
			{
				"asks": [["50001.0", "1.5"]],
				"bids": [["50000.0", "2.0"]],
				"ts": "1597026383085"
			}
		]
	}`)

	symbol, ob, err := adapter.ParseDepth(raw)
	require.NoError(t, err)
	assert.Equal(t, "BTC-USDT-SWAP", symbol)
	assert.Equal(t, int64(1597026383085), ob.Version)
	require.Len(t, ob.Asks, 1)
	require.Len(t, ob.Bids, 1)
	assert.Equal(t, 50001.0, ob.Asks[0].Price)
	assert.Equal(t, 1.5, ob.Asks[0].Volume)
}

func TestWsAdapter_ParseKline(t *testing.T) {
	t.Parallel()

	adapter := okx.NewWsAdapter()
	raw := []byte(`{
		"arg": {"channel": "candle1m", "instId": "BTC-USDT-SWAP"},
		"data": [
			["1597026300000", "50000.0", "50001.0", "49999.0", "50000.5", "10", "500000"]
		]
	}`)

	symbol, k, err := adapter.ParseKline(raw)
	require.NoError(t, err)
	assert.Equal(t, "BTC-USDT-SWAP", symbol)
	assert.Equal(t, int64(1597026300000), k.Timestamp)
	assert.Equal(t, 50000.0, k.Open)
	assert.Equal(t, 50000.5, k.Close)
}

func TestWsAdapter_ParseOrder(t *testing.T) {
	t.Parallel()

	adapter := okx.NewWsAdapter()
	raw := []byte(`{
		"arg": {"channel": "orders"},
		"data": [
			{
				"instId": "BTC-USDT-SWAP",
				"ordId": "12345",
				"clOrdId": "my_client_id",
				"px": "50000",
				"sz": "2",
				"side": "buy",
				"posSide": "long",
				"state": "filled",
				"fillSz": "2",
				"avgPx": "50000",
				"cTime": "1597026383000",
				"uTime": "1597026383085"
			}
		]
	}`)

	deal, err := adapter.ParseOrder(raw)
	require.NoError(t, err)
	assert.Equal(t, "12345", deal.OrderID)
	assert.Equal(t, "BTC-USDT-SWAP", deal.Symbol)
	assert.Equal(t, exchange.SideOpenLong, deal.Side)
	assert.Equal(t, exchange.OrderStateFilled, deal.State)
}

func TestWsAdapter_ParsePosition(t *testing.T) {
	t.Parallel()

	adapter := okx.NewWsAdapter()
	raw := []byte(`{
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

	update, err := adapter.ParsePosition(raw)
	require.NoError(t, err)
	assert.Equal(t, "BTC-USDT-SWAP", update.Symbol)
	assert.Equal(t, 1.0, update.HoldVol)
	assert.Equal(t, 10, update.Leverage)
	assert.Equal(t, 1, update.PositionType)
}

func TestWsAdapter_SubscriptionAndErrors(t *testing.T) {
	t.Parallel()

	adapter := okx.NewWsAdapter()

	// 1. GetPingConfig
	pingMsg, interval := adapter.GetPingConfig()
	assert.Equal(t, "ping", pingMsg)
	assert.Equal(t, 20*time.Second, interval)

	// 2. GetAuthHook
	hook := adapter.GetAuthHook("", "")
	assert.Nil(t, hook)

	hookWithKey := adapter.GetAuthHook("my_api_key", "my_api_secret")
	assert.NotNil(t, hookWithKey)

	// 3. ParseOrderDeal (unimplemented fallback)
	_, err := adapter.ParseOrderDeal(nil)
	assert.Error(t, err)

	// 4. ParseTrackOrder (unimplemented fallback)
	_, err = adapter.ParseTrackOrder(nil)
	assert.Error(t, err)

	// 5. Subscription checks with cancelled context to cover pool routing
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
	_, _, err = adapter.ParseTicker([]byte(`{}`))
	assert.Error(t, err)

	_, _, err = adapter.ParseTicker([]byte(`{"arg":{"instId":"BTC-USDT-SWAP"}}`))
	assert.Error(t, err)

	_, _, err = adapter.ParseTicker([]byte(`{"arg":{"instId":"BTC-USDT-SWAP"},"data":[]}`))
	assert.Error(t, err)

	_, _, err = adapter.ParseTicker([]byte(`{"arg":{"instId":"BTC-USDT-SWAP"},"data":"invalid"}`))
	assert.Error(t, err)

	_, _, err = adapter.ParseDepth([]byte(`{}`))
	assert.Error(t, err)

	_, _, err = adapter.ParseKline([]byte(`{}`))
	assert.Error(t, err)

	_, _, err = adapter.ParseKline([]byte(`{"arg":{"instId":"BTC-USDT-SWAP"},"data":[]}`))
	assert.Error(t, err)

	_, _, err = adapter.ParseKline([]byte(`{"arg":{"instId":"BTC-USDT-SWAP"},"data":[["12345"]]}`))
	assert.Error(t, err)

	_, err = adapter.ParseOrder([]byte(`{}`))
	assert.Error(t, err)

	_, err = adapter.ParseOrder([]byte(`{"data":[]}`))
	assert.Error(t, err)

	_, err = adapter.ParsePosition([]byte(`{}`))
	assert.Error(t, err)

	_, err = adapter.ParsePosition([]byte(`{"data":[]}`))
	assert.Error(t, err)
}
