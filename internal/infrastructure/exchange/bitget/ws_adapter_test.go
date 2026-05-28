package bitget_test

import (
	"context"
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

	adapter := bitget.NewWsAdapter()
	extractor := adapter.GetChannelExtractor()

	assert.Equal(t, "tickers", extractor([]byte(`{"arg":{"channel":"ticker","instId":"BTCUSDT"}}`)))
	assert.Equal(t, "kline", extractor([]byte(`{"arg":{"channel":"candle1m","instId":"BTCUSDT"}}`)))
	assert.Equal(t, "depth", extractor([]byte(`{"arg":{"channel":"books","instId":"BTCUSDT"}}`)))
	assert.Equal(t, "personal.order", extractor([]byte(`{"arg":{"channel":"orders"}}`)))
	assert.Equal(t, "personal.position", extractor([]byte(`{"arg":{"channel":"positions"}}`)))
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

func TestWsAdapter_ParseDepth(t *testing.T) {
	t.Parallel()

	adapter := bitget.NewWsAdapter()
	raw := []byte(`{
		"arg": {"channel": "books", "instId": "BTCUSDT"},
		"data": [
			{
				"symbol": "BTCUSDT",
				"asks": [["50001.0", "1.5"]],
				"bids": [["50000.0", "2.0"]],
				"ts": "1695812285073"
			}
		]
	}`)

	symbol, ob, err := adapter.ParseDepth(raw)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", symbol)
	assert.Equal(t, int64(1695812285073), ob.Version)
	require.Len(t, ob.Asks, 1)
	require.Len(t, ob.Bids, 1)
	assert.Equal(t, 50001.0, ob.Asks[0].Price)
	assert.Equal(t, 1.5, ob.Asks[0].Volume)
}

func TestWsAdapter_ParseKline(t *testing.T) {
	t.Parallel()

	adapter := bitget.NewWsAdapter()
	raw := []byte(`{
		"arg": {"channel": "candle1m", "instId": "BTCUSDT"},
		"data": [
			["1695812285000", "50000.0", "50001.0", "49999.0", "50000.5", "10", "500000"]
		]
	}`)

	symbol, k, err := adapter.ParseKline(raw)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", symbol)
	assert.Equal(t, int64(1695812285000), k.Timestamp)
	assert.Equal(t, 50000.0, k.Open)
	assert.Equal(t, 50000.5, k.Close)
}

func TestWsAdapter_ParseOrder(t *testing.T) {
	t.Parallel()

	adapter := bitget.NewWsAdapter()
	raw := []byte(`{
		"arg": {"channel": "orders"},
		"data": [
			{
				"symbol": "BTCUSDT",
				"orderId": "12345",
				"clientOid": "my_client_id",
				"price": "50000",
				"size": "2",
				"side": "buy",
				"posSide": "long",
				"state": "filled",
				"baseVolume": "2",
				"priceAvg": "50000",
				"cTime": "1695812285000",
				"uTime": "1695812285073"
			}
		]
	}`)

	deal, err := adapter.ParseOrder(raw)
	require.NoError(t, err)
	assert.Equal(t, "12345", deal.OrderID)
	assert.Equal(t, "BTCUSDT", deal.Symbol)
	assert.Equal(t, exchange.SideOpenLong, deal.Side)
	assert.Equal(t, exchange.OrderStateFilled, deal.State)
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

	// 3. ParseOrderDeal (stub, returns error)
	_, err := adapter.ParseOrderDeal([]byte{})
	assert.Error(t, err)

	// 4. ParseTrackOrder (stub, returns error)
	_, err = adapter.ParseTrackOrder([]byte{})
	assert.Error(t, err)

	// 5. Extractor other cases
	extractor := adapter.GetChannelExtractor()
	assert.Equal(t, "pong", extractor([]byte("pong")))
	assert.Equal(t, "", extractor([]byte(`{}`)))
	assert.Equal(t, "custom", extractor([]byte(`{"arg":{"channel":"custom"}}`)))

	// 6. ParseTicker errors
	_, _, err = adapter.ParseTicker([]byte(`{}`))
	assert.Error(t, err)

	_, _, err = adapter.ParseTicker([]byte(`{"arg":{"instId":"BTCUSDT"}, "data":[]}`))
	assert.Error(t, err)

	// 7. ParseDepth errors
	_, _, err = adapter.ParseDepth([]byte(`{}`))
	assert.Error(t, err)

	_, _, err = adapter.ParseDepth([]byte(`{"arg":{"instId":"BTCUSDT"}, "data":[]}`))
	assert.Error(t, err)

	// 8. ParseKline errors
	_, _, err = adapter.ParseKline([]byte(`{}`))
	assert.Error(t, err)

	_, _, err = adapter.ParseKline([]byte(`{"arg":{"instId":"BTCUSDT"}, "data":[]}`))
	assert.Error(t, err)

	_, _, err = adapter.ParseKline([]byte(`{"arg":{"instId":"BTCUSDT"}, "data":[["1695812285000"]]}`))
	assert.Error(t, err)

	// 9. ParseOrder errors
	_, err = adapter.ParseOrder([]byte(`{}`))
	assert.Error(t, err)

	_, err = adapter.ParseOrder([]byte(`{"data":[]}`))
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
	pool := pkgws.NewPool("ws://127.0.0.1:1", 30, nil)
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

	// 3. Test ParseOrder with different sides (posSide = short/long/default, side = sell/buy)
	rawShortSell := []byte(`{
		"arg": {"channel": "orders"},
		"data": [
			{
				"symbol": "BTCUSDT",
				"orderId": "12345",
				"clientOid": "my_client_id",
				"price": "50000",
				"size": "2",
				"side": "sell",
				"posSide": "short",
				"state": "filled",
				"baseVolume": "2",
				"priceAvg": "50000",
				"cTime": "1695812285000",
				"uTime": "1695812285073"
			}
		]
	}`)
	deal, err := adapter.ParseOrder(rawShortSell)
	require.NoError(t, err)
	assert.Equal(t, exchange.SideOpenShort, deal.Side)

	rawShortBuy := []byte(`{
		"arg": {"channel": "orders"},
		"data": [
			{
				"symbol": "BTCUSDT",
				"orderId": "12345",
				"clientOid": "my_client_id",
				"price": "50000",
				"size": "2",
				"side": "buy",
				"posSide": "short",
				"state": "filled",
				"baseVolume": "2",
				"priceAvg": "50000",
				"cTime": "1695812285000",
				"uTime": "1695812285073"
			}
		]
	}`)
	deal, err = adapter.ParseOrder(rawShortBuy)
	require.NoError(t, err)
	assert.Equal(t, exchange.SideCloseShort, deal.Side)

	rawNetBuy := []byte(`{
		"arg": {"channel": "orders"},
		"data": [
			{
				"symbol": "BTCUSDT",
				"orderId": "12345",
				"clientOid": "my_client_id",
				"price": "50000",
				"size": "2",
				"side": "buy",
				"posSide": "net",
				"state": "canceled",
				"baseVolume": "2",
				"priceAvg": "50000",
				"cTime": "1695812285000",
				"uTime": "1695812285073"
			}
		]
	}`)
	deal, err = adapter.ParseOrder(rawNetBuy)
	require.NoError(t, err)
	assert.Equal(t, exchange.SideOpenLong, deal.Side)
	assert.Equal(t, exchange.OrderStateCanceled, deal.State)

	rawNetSell := []byte(`{
		"arg": {"channel": "orders"},
		"data": [
			{
				"symbol": "BTCUSDT",
				"orderId": "12345",
				"clientOid": "my_client_id",
				"price": "50000",
				"size": "2",
				"side": "sell",
				"posSide": "net",
				"state": "new",
				"baseVolume": "2",
				"priceAvg": "50000",
				"cTime": "1695812285000",
				"uTime": "1695812285073"
			}
		]
	}`)
	deal, err = adapter.ParseOrder(rawNetSell)
	require.NoError(t, err)
	assert.Equal(t, exchange.SideOpenShort, deal.Side)
	assert.Equal(t, exchange.OrderStatePartial, deal.State)

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
	assert.Equal(t, 2, update.OpenType)     // Cross

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
	assert.Equal(t, 1, update.OpenType)     // Isolated
}
