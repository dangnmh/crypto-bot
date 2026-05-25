package gate_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/gate"
	pkgws "crypto-bot/pkg/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWsAdapter_ChannelExtractorAndAuth(t *testing.T) {
	t.Parallel()

	a := gate.NewWsAdapter()
	assert.Nil(t, a.GetAuthHook("key", "secret"))
	payload, interval := a.GetPingConfig()
	assert.NotNil(t, payload)
	assert.Equal(t, 15*time.Second, interval)

	extract := a.GetChannelExtractor()
	tests := []struct {
		name string
		msg  string
		want string
	}{
		{"ticker", `{"channel":"futures.tickers"}`, "ticker"},
		{"depth", `{"channel":"futures.order_book"}`, "depth"},
		{"kline", `{"channel":"futures.candlesticks"}`, "kline"},
		{"order", `{"channel":"futures.orders"}`, "personal.order"},
		{"position", `{"channel":"futures.positions"}`, "personal.position"},
		{"unknown", `{"channel":"custom"}`, "custom"},
		{"bad json", `{`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, extract([]byte(tt.msg)))
		})
	}
}

func TestWsAdapterSubscriptionsReturnContextErrors(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := gate.NewWsAdapter()
	a.SetPool(pkgws.NewPool("ws://127.0.0.1:1", 1, logger))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.ErrorIs(t, a.SubscribeTicker(ctx, "BTC_USDT"), context.Canceled)
	require.NoError(t, a.UnsubscribeTicker(ctx, "BTC_USDT"))
	require.ErrorIs(t, a.SubscribeKline(ctx, "BTC_USDT"), context.Canceled)
	err := a.UnsubscribeKline(ctx, "BTC_USDT")
	assert.True(t, err == nil || errors.Is(err, context.Canceled))
	require.ErrorIs(t, a.SubscribeDepth(ctx, "BTC_USDT", "0"), context.Canceled)
	err = a.UnsubscribeDepth(ctx, "BTC_USDT", "0")
	assert.True(t, err == nil || errors.Is(err, context.Canceled))
}

func TestWsAdapterSubscribePersonalWithoutPrivateClient(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := gate.NewWsAdapter()
	a.SetPool(pkgws.NewPool("ws://127.0.0.1:1", 1, logger))
	assert.Nil(t, a.GetAuthHook("key", "secret"))

	require.NoError(t, a.SubscribePersonal(context.Background()))
}

func TestWsAdapter_ParseMarketMessages(t *testing.T) {
	t.Parallel()

	a := gate.NewWsAdapter()

	symbol, price, err := a.ParseTicker([]byte(`{"result":[{"contract":"BTC_USDT","last":"100.5","lowest_ask":"101","highest_bid":"100","volume_24h":"12.5"}]}`))
	require.NoError(t, err)
	assert.Equal(t, "BTC_USDT", symbol)
	assert.Equal(t, 100.5, price.LastPrice)
	assert.Equal(t, 100.0, price.BestBid)
	assert.Equal(t, 101.0, price.BestAsk)

	symbol, depth, err := a.ParseDepth([]byte(`{"result":{"contract":"BTC_USDT","asks":[{"p":"101","s":"2"},{"p":"0","s":"9"}],"bids":[{"p":"100","s":"3"}]}}`))
	require.NoError(t, err)
	assert.Equal(t, "BTC_USDT", symbol)
	require.Len(t, depth.Asks, 1)
	require.Len(t, depth.Bids, 1)
	assert.Equal(t, 101.0, depth.Asks[0].Price)

	symbol, kline, err := a.ParseKline([]byte(`{"result":[{"t":1700000000,"v":"5","c":"100","h":"102","l":"99","o":"101","n":"1m_BTC_USDT"}]}`))
	require.NoError(t, err)
	assert.Equal(t, "BTC_USDT", symbol)
	assert.Equal(t, int64(1700000000000), kline.Timestamp)
	assert.Equal(t, 101.0, kline.Open)
	assert.Equal(t, 100.0, kline.Close)
}

func TestWsAdapter_ParsePersonalMessages(t *testing.T) {
	t.Parallel()

	a := gate.NewWsAdapter()

	order, err := a.ParseOrder([]byte(`{"result":[{"id":42,"contract":"BTC_USDT","size":5,"price":"100","status":"finished","finish_as":"filled","left":0,"text":"t-ext"}]}`))
	require.NoError(t, err)
	assert.Equal(t, "42", order.OrderID)
	assert.Equal(t, exchange.OrderStateFilled, order.State)
	assert.Equal(t, "ext", order.ExternalOID)
	assert.Equal(t, 5.0, order.DealVol)

	order, err = a.ParseOrder([]byte(`{"result":[{"id":43,"contract":"BTC_USDT","size":-5,"price":"100","status":"open","left":2,"text":"raw"}]}`))
	require.NoError(t, err)
	assert.Equal(t, exchange.OrderStatePartial, order.State)
	assert.Equal(t, "raw", order.ExternalOID)

	//nolint:misspell // Gate.io uses the British spelling in the API field name.
	position, err := a.ParsePosition([]byte(`{"result":[{"contract":"ETH_USDT","size":-3,"entry_price":"2000.5","leverage":20,"realised_pnl":"1.2"}]}`))
	require.NoError(t, err)
	assert.Equal(t, "ETH_USDT", position.Symbol)
	assert.Equal(t, 3.0, position.HoldVol)
	assert.Equal(t, 2, position.PositionType)
	assert.Equal(t, 2000.5, position.HoldAvgPrice)

	deal, err := a.ParseOrderDeal([]byte(`{}`))
	require.NoError(t, err)
	assert.Nil(t, deal)
	track, err := a.ParseTrackOrder([]byte(`{}`))
	require.NoError(t, err)
	assert.Nil(t, track)
}

func TestWsAdapter_ParseErrors(t *testing.T) {
	t.Parallel()

	a := gate.NewWsAdapter()
	_, _, err := a.ParseTicker([]byte(`{`))
	require.Error(t, err)
	_, _, err = a.ParseTicker([]byte(`{"result":[]}`))
	require.Error(t, err)
	_, _, err = a.ParseKline([]byte(`{`))
	require.Error(t, err)
	_, _, err = a.ParseKline([]byte(`{"result":[]}`))
	require.Error(t, err)
	_, err = a.ParseOrder([]byte(`{`))
	require.Error(t, err)
	_, err = a.ParseOrder([]byte(`{"result":[]}`))
	require.Error(t, err)
	_, err = a.ParsePosition([]byte(`{`))
	require.Error(t, err)
	_, err = a.ParsePosition([]byte(`{"result":[]}`))
	require.Error(t, err)
}
