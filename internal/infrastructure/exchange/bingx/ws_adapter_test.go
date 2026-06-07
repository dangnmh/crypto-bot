package bingx_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange/bingx"
	pkgws "crypto-bot/pkg/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWsAdapter_GetChannelExtractor(t *testing.T) {
	t.Parallel()

	adapter := bingx.NewWsAdapter("")
	extractor := adapter.GetChannelExtractor()

	assert.Equal(t, "Ping", extractor([]byte(`Ping`)))
	assert.Equal(t, "ticker", extractor([]byte(`{"dataType":"BTC-USDT@ticker"}`)))
	assert.Equal(t, "kline", extractor([]byte(`{"dataType":"BTC-USDT@kline_1m"}`)))
	assert.Equal(t, "depth", extractor([]byte(`{"dataType":"BTC-USDT@depth20"}`)))
	assert.Equal(t, "", extractor([]byte(`{}`)))
	assert.Equal(t, "", extractor([]byte(`{"dataType":"invalid"}`)))
	assert.Equal(t, "BTC-USDT@custom", extractor([]byte(`{"dataType":"BTC-USDT@custom"}`)))
}

func TestWsAdapter_ParseTicker(t *testing.T) {
	t.Parallel()

	adapter := bingx.NewWsAdapter("")
	raw := []byte(`{
		"dataType": "BTC-USDT@ticker",
		"data": {
			"c": 50000.5,
			"B": 50000.0,
			"A": 50001.0,
			"v": 1000.0
		}
	}`)

	symbol, pd, err := adapter.ParseTicker(raw)
	require.NoError(t, err)
	assert.Equal(t, "BTC-USDT", symbol)
	assert.Equal(t, 50000.5, pd.LastPrice)
	assert.Equal(t, 50000.0, pd.BestBid)
	assert.Equal(t, 50001.0, pd.BestAsk)
	assert.Equal(t, 1000.0, pd.Volume24)

	// Test real 24h ticker schema
	realRaw := []byte(`{
		"dataType": "BTC-USDT@ticker",
		"data": {
			"e": "24hrTicker",
			"s": "BTC-USDT",
			"c": 50000.5,
			"v": 1000.0,
			"B": 50000.0,
			"A": 50001.0
		}
	}`)
	symbolReal, pdReal, errReal := adapter.ParseTicker(realRaw)
	require.NoError(t, errReal)
	assert.Equal(t, "BTC-USDT", symbolReal)
	assert.Equal(t, 50000.5, pdReal.LastPrice)
	assert.Equal(t, 50000.0, pdReal.BestBid)
	assert.Equal(t, 50001.0, pdReal.BestAsk)
	assert.Equal(t, 1000.0, pdReal.Volume24)

	// Test case-insensitive key collision (e.g., c vs C, B vs b, A vs a)
	collisionRaw := []byte(`{
		"dataType": "BTC-USDT@ticker",
		"data": {
			"e": "24hrTicker",
			"s": "BTC-USDT",
			"c": 50000.5,
			"C": 1780809567801,
			"v": 1000.0,
			"B": 50000.0,
			"b": 30596.98,
			"A": 50001.0,
			"a": 2064.91
		}
	}`)
	symbolColl, pdColl, errColl := adapter.ParseTicker(collisionRaw)
	require.NoError(t, errColl)
	assert.Equal(t, "BTC-USDT", symbolColl)
	assert.Equal(t, 50000.5, pdColl.LastPrice)
	assert.Equal(t, 50000.0, pdColl.BestBid)
	assert.Equal(t, 50001.0, pdColl.BestAsk)
	assert.Equal(t, 1000.0, pdColl.Volume24)
}

func TestWsAdapter_ParseBookTicker(t *testing.T) {
	t.Parallel()

	adapter := bingx.NewWsAdapter("")
	raw := []byte(`{
		"dataType": "BTC-USDT@bookTicker",
		"data": {
			"e": "bookTicker",
			"u": 578534658,
			"E": 1760001840686,
			"T": 1760001840687,
			"s": "BTC-USDT",
			"b": "121584.1",
			"B": "18.7084",
			"a": "121584.3",
			"A": "4.9602"
		}
	}`)

	symbol, pd, err := adapter.ParseTicker(raw)
	require.NoError(t, err)
	assert.Equal(t, "BTC-USDT", symbol)
	assert.InDelta(t, 121584.2, pd.LastPrice, 1e-9) // (121584.1 + 121584.3) / 2
	assert.Equal(t, 121584.1, pd.BestBid)
	assert.Equal(t, 121584.3, pd.BestAsk)
	assert.Equal(t, 18.7084, pd.Volume24) // best bid qty fallback
}

func TestWsAdapter_OtherMethods(t *testing.T) {
	t.Parallel()

	adapter := bingx.NewWsAdapter("")

	// 1. GetPingConfig
	pingMsg, interval := adapter.GetPingConfig()
	assert.Equal(t, "Ping", pingMsg)
	assert.Equal(t, 30*time.Second, interval)

	// 2. GetAuthHook
	hook := adapter.GetAuthHook("", "")
	assert.Nil(t, hook)

	// 3. SubscribePersonal
	err := adapter.SubscribePersonal(context.Background())
	assert.NoError(t, err)

	// 8. ParsePosition
	_, err = adapter.ParsePosition([]byte{})
	assert.Error(t, err)

	// 9. GetPreprocessor with valid GZIP
	preprocessor := adapter.GetPreprocessor()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write([]byte("hello world"))
	_ = zw.Close()
	decompressed, err := preprocessor(buf.Bytes())
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(decompressed))

	// 10. GetPreprocessor with invalid GZIP
	_, err = preprocessor([]byte("invalid gzip"))
	assert.Error(t, err)

	// 11. ParseTicker errors
	_, _, err = adapter.ParseTicker([]byte(`{}`))
	assert.Error(t, err)

	_, _, err = adapter.ParseTicker([]byte(`{"dataType":"invalid"}`))
	assert.Error(t, err)

	_, _, err = adapter.ParseTicker([]byte(`{"dataType":"BTC-USDT@ticker", "data":"invalid"}`))
	assert.Error(t, err)
}

func TestWsAdapter_SubscriptionsAndAdditionalFeatures(t *testing.T) {
	t.Parallel()

	adapter := bingx.NewWsAdapter("")
	pool := pkgws.NewPool("ws://127.0.0.1:1", 30, slog.Default())
	defer pool.Close()
	adapter.SetPool(pool)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_ = adapter.SubscribeTicker(ctx, "BTC-USDT")
	_ = adapter.UnsubscribeTicker(ctx, "BTC-USDT")
	_ = adapter.SubscribeKline(ctx, "BTC-USDT")
	_ = adapter.UnsubscribeKline(ctx, "BTC-USDT")
	_ = adapter.SubscribeDepth(ctx, "BTC-USDT", "1")
	_ = adapter.UnsubscribeDepth(ctx, "BTC-USDT", "1")
	_ = adapter.SubscribePersonal(ctx)
}

func TestWsAdapter_PrivateParsersAndPrivateURLFunc(t *testing.T) {
	t.Parallel()

	adapter := bingx.NewWsAdapter("wss://open-api-swap.bingx.com/swap-market")

	// 2. Test ParsePosition
	positionRaw := []byte(`{
		"e": "ACCOUNT_UPDATE",
		"a": {
			"m": "ORDER",
			"B": [{"a": "USDT", "wb": "100.0"}],
			"P": [
				{
					"s": "BTC-USDT",
					"pa": "0.002",
					"ep": "60100",
					"up": "1.0",
					"ps": "LONG"
				}
			]
		}
	}`)
	pos, err := adapter.ParsePosition(positionRaw)
	require.NoError(t, err)
	assert.Equal(t, "BTC-USDT", pos.Symbol)
	assert.Equal(t, 0.002, pos.HoldVol)
	assert.Equal(t, 60100.0, pos.HoldAvgPrice)
	assert.Equal(t, 1.0, pos.CloseProfitLoss)
	assert.Equal(t, 1, pos.PositionType) // LONG

	// Test channel extractor for private event
	testClient := bingx.NewClient(nil, "", "key", "secret", config.LoggingConfig{})
	adapter.SetClient(testClient)
	extractor := adapter.GetChannelExtractor()
	assert.Equal(t, "personal.position", extractor(positionRaw))

	// 3. Test GetPrivateURLFunc
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" && r.URL.Path == "/openApi/user/auth/userDataStream" {
			_, _ = w.Write([]byte(`{"code": 0, "msg": "success", "data": {"listenKey": "lk-abcde"}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := bingx.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	adapter.SetClient(client)

	urlFunc := adapter.GetPrivateURLFunc(context.Background())
	u, err := urlFunc()
	require.NoError(t, err)
	assert.Equal(t, "wss://open-api-swap.bingx.com/swap-market?listenKey=lk-abcde", u)
}
