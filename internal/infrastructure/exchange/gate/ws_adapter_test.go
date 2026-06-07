package gate_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

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
}

func TestWsAdapter_ParsePersonalMessages(t *testing.T) {
	t.Parallel()

	a := gate.NewWsAdapter()

	//nolint:misspell // Gate.io uses the British spelling in the API field name.
	position, err := a.ParsePosition([]byte(`{"result":[{"contract":"ETH_USDT","size":-3,"entry_price":"2000.5","leverage":20,"realised_pnl":"1.2"}]}`))
	require.NoError(t, err)
	assert.Equal(t, "ETH_USDT", position.Symbol)
	assert.Equal(t, 3.0, position.HoldVol)
	assert.Equal(t, 2, position.PositionType)
	assert.Equal(t, 2000.5, position.HoldAvgPrice)
}

func TestWsAdapter_ParseErrors(t *testing.T) {
	t.Parallel()

	a := gate.NewWsAdapter()
	_, _, err := a.ParseTicker([]byte(`{`))
	require.Error(t, err)
	_, _, err = a.ParseTicker([]byte(`{"result":[]}`))
	require.Error(t, err)
	_, err = a.ParsePosition([]byte(`{`))
	require.Error(t, err)
	_, err = a.ParsePosition([]byte(`{"result":[]}`))
	require.Error(t, err)
}
