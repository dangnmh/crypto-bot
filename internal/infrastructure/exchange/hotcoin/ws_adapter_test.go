package hotcoin_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"log/slog"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/exchange/hotcoin"
	pkgws "crypto-bot/pkg/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWsAdapter_GetPingConfig(t *testing.T) {
	t.Parallel()
	a := hotcoin.NewWsAdapter("key", "secret")
	payload, interval := a.GetPingConfig()

	assert.NotNil(t, payload)
	assert.Equal(t, 30*time.Second, interval)
}

func TestWsAdapter_GetChannelExtractor(t *testing.T) {
	t.Parallel()
	a := hotcoin.NewWsAdapter("key", "secret")
	extractor := a.GetChannelExtractor()
	require.NotNil(t, extractor)

	assert.Equal(t, "market.ticker", extractor([]byte(`{"channel":"market.btcusdt.ticker"}`)))
	assert.Equal(t, "market.ticker", extractor([]byte(`{"channel":"market.ethusdt.depth"}`)))
	assert.Equal(t, "personal.position", extractor([]byte(`{"channel":"personal.position"}`)))
	assert.Equal(t, "personal.position", extractor([]byte(`{"event":"positions"}`)))
	assert.Equal(t, "personal.order", extractor([]byte(`{"channel":"personal.order"}`)))
	assert.Equal(t, "personal.order", extractor([]byte(`{"event":"fills"}`)))
	assert.Equal(t, "subscribed", extractor([]byte(`{"status":"ok"}`)))
	assert.Equal(t, "subscribed", extractor([]byte(`{"event":"subscribe"}`)))
	assert.Equal(t, "", extractor([]byte(`{}`)))
}

func TestWsAdapter_GetPreprocessor(t *testing.T) {
	t.Parallel()
	a := hotcoin.NewWsAdapter("key", "secret")
	preprocessor := a.GetPreprocessor()
	require.NotNil(t, preprocessor)

	raw := []byte(`{"ping":"ping"}`)
	out, err := preprocessor(raw)
	require.NoError(t, err)
	assert.Equal(t, raw, out)

	// Test compressed GZIP
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write(raw)
	_ = zw.Close()

	outGz, err := preprocessor(buf.Bytes())
	require.NoError(t, err)
	assert.Equal(t, raw, outGz)
}

func TestWsAdapter_GetAuthHook(t *testing.T) {
	t.Parallel()
	a := hotcoin.NewWsAdapter("my-key", "my-secret")
	a.SetClock(mockClock{now: time.UnixMilli(1783229340000)})

	hook := a.GetAuthHook("my-key", "my-secret")
	require.NotNil(t, hook)

	client := pkgws.NewClient("wss://dummy", slog.Default())
	hook(client)
}

func TestWsAdapter_ParseTickerAndDepth(t *testing.T) {
	t.Parallel()
	a := hotcoin.NewWsAdapter("key", "secret")

	// 1. Ticker Msg
	tickerPayload := []byte(`{
		"channel": "market.btcusdt.ticker",
		"data": {
			"ask": "96123.5",
			"bid": "96122.0",
			"lastPrice": "96123.0",
			"baseVolume": "120.45",
			"markPrice": "96122.5"
		}
	}`)

	symbol, pd, err := a.ParseTicker(tickerPayload)
	require.NoError(t, err)
	assert.Equal(t, "BTC_USDT", symbol)
	assert.Equal(t, 96123.0, pd.LastPrice)
	assert.Equal(t, 96122.5, pd.FairPrice)
	assert.Equal(t, 96123.0, pd.BestBid)
	assert.Equal(t, 96123.0, pd.BestAsk)
	assert.Equal(t, 120.45, pd.Volume24)

	// 2. Depth Msg
	depthPayload := []byte(`{
		"channel": "market.btcusdt.depth",
		"data": {
			"bids": [["96121.0", "1.5"]],
			"asks": [["96124.0", "0.8"]]
		}
	}`)

	symbol, pd, err = a.ParseTicker(depthPayload)
	require.NoError(t, err)
	assert.Equal(t, "BTC_USDT", symbol)
	assert.Equal(t, 96121.0, pd.BestBid)
	assert.Equal(t, 96124.0, pd.BestAsk)

	// 3. New Ticker Msg
	newTickerPayload := []byte(`{
		"biz": "perpetual",
		"data": [
			[
				1712800975802,
				"99999.00",
				"60000.00",
				"15021528",
				"1042275165",
				"69539.35",
				"70615.76",
				1065,
				"1.55",
				"70598.22",
				"70607.15",
				"btcusdt",
				"508041.27",
				0
			]
		],
		"type": "ticker",
		"env": 0,
		"contractCode": "btcusdt",
		"timestamp": 1712800977406
	}`)

	symbol, pd, err = a.ParseTicker(newTickerPayload)
	require.NoError(t, err)
	assert.Equal(t, "BTC_USDT", symbol)
	assert.Equal(t, 70615.76, pd.LastPrice)
	assert.Equal(t, 15021528.0, pd.Volume24)
	assert.Equal(t, 70598.22, pd.BestBid)
	assert.Equal(t, 70607.15, pd.BestAsk)
}

func TestWsAdapter_ParsePosition(t *testing.T) {
	t.Parallel()
	a := hotcoin.NewWsAdapter("key", "secret")

	payload := []byte(`{
		"event": "positions",
		"data": {
			"contractCode": "btcusdt",
			"side": "long",
			"holdVolume": "1.5",
			"openAvgPrice": "95000",
			"leverage": 20
		}
	}`)

	pos, err := a.ParsePosition(payload)
	require.NoError(t, err)
	require.NotNil(t, pos)

	assert.Equal(t, "BTC_USDT", pos.Symbol)
	assert.Equal(t, 1.5, pos.HoldVolContract)
	assert.Equal(t, 95000.0, pos.OpenAvgPrice)
	assert.Equal(t, 20, pos.Leverage)
	assert.Equal(t, 1, int(pos.PositionType)) // Long = 1
}

func TestWsAdapter_SubscribePublicAndPrivate(t *testing.T) {
	t.Parallel()

	a := hotcoin.NewWsAdapter("key", "secret")
	pool := pkgws.NewPool("ws://dummy", 30, slog.Default())
	a.SetPool(pool)
	a.SetClient(nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := a.SubscribeTicker(ctx, "BTC_USDT")
	assert.Error(t, err)

	err = a.UnsubscribeTicker(ctx, "BTC_USDT")
	assert.NoError(t, err)

	err = a.SubscribePersonal(ctx)
	assert.NoError(t, err)

	pingHandler := a.GetCustomPingHandler()
	assert.NotNil(t, pingHandler)

	// Test string ping
	ok := pingHandler(nil, []byte(`{"ping":"ping"}`))
	assert.True(t, ok)

	// Test numeric/timestamp ping
	ok = pingHandler(nil, []byte(`{"ping":1783433819000}`))
	assert.True(t, ok)

	// Test non-ping json
	ok = pingHandler(nil, []byte(`{"other":"data"}`))
	assert.False(t, ok)
}
