package bingx_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/exchange/bingx"
	pkgws "crypto-bot/pkg/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWsAdapter_GetChannelExtractor(t *testing.T) {
	t.Parallel()

	adapter := bingx.NewWsAdapter()
	extractor := adapter.GetChannelExtractor()

	assert.Equal(t, "Ping", extractor([]byte(`Ping`)))
	assert.Equal(t, "tickers", extractor([]byte(`{"dataType":"BTC-USDT@ticker"}`)))
	assert.Equal(t, "kline", extractor([]byte(`{"dataType":"BTC-USDT@kline_1m"}`)))
	assert.Equal(t, "depth", extractor([]byte(`{"dataType":"BTC-USDT@depth20"}`)))
	assert.Equal(t, "", extractor([]byte(`{}`)))
	assert.Equal(t, "", extractor([]byte(`{"dataType":"invalid"}`)))
	assert.Equal(t, "BTC-USDT@custom", extractor([]byte(`{"dataType":"BTC-USDT@custom"}`)))
}

func TestWsAdapter_ParseTicker(t *testing.T) {
	t.Parallel()

	adapter := bingx.NewWsAdapter()
	raw := []byte(`{
		"dataType": "BTC-USDT@ticker",
		"data": {
			"lastPrice": "50000.5",
			"bidPrice": "50000.0",
			"askPrice": "50001.0",
			"volume": "1000"
		}
	}`)

	symbol, pd, err := adapter.ParseTicker(raw)
	require.NoError(t, err)
	assert.Equal(t, "BTC-USDT", symbol)
	assert.Equal(t, 50000.5, pd.LastPrice)
	assert.Equal(t, 50000.0, pd.BestBid)
	assert.Equal(t, 50001.0, pd.BestAsk)
	assert.Equal(t, 1000.0, pd.Volume24)
}

func TestWsAdapter_ParseDepth(t *testing.T) {
	t.Parallel()

	adapter := bingx.NewWsAdapter()
	raw := []byte(`{
		"dataType": "BTC-USDT@depth20",
		"data": {
			"asks": [["50001.0", "1.5"]],
			"bids": [["50000.0", "2.0"]]
		}
	}`)

	symbol, ob, err := adapter.ParseDepth(raw)
	require.NoError(t, err)
	assert.Equal(t, "BTC-USDT", symbol)
	require.Len(t, ob.Asks, 1)
	require.Len(t, ob.Bids, 1)
	assert.Equal(t, 50001.0, ob.Asks[0].Price)
	assert.Equal(t, 1.5, ob.Asks[0].Volume)
}

func TestWsAdapter_OtherMethods(t *testing.T) {
	t.Parallel()

	adapter := bingx.NewWsAdapter()

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

	// 4. ParseKline
	_, _, err = adapter.ParseKline([]byte{})
	assert.Error(t, err)

	// 5. ParseOrder
	_, err = adapter.ParseOrder([]byte{})
	assert.Error(t, err)

	// 6. ParseOrderDeal
	_, err = adapter.ParseOrderDeal([]byte{})
	assert.Error(t, err)

	// 7. ParseTrackOrder
	_, err = adapter.ParseTrackOrder([]byte{})
	assert.Error(t, err)

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

	// 12. ParseDepth errors
	_, _, err = adapter.ParseDepth([]byte(`{}`))
	assert.Error(t, err)

	_, _, err = adapter.ParseDepth([]byte(`{"dataType":"invalid"}`))
	assert.Error(t, err)

	_, _, err = adapter.ParseDepth([]byte(`{"dataType":"BTC-USDT@depth20", "data":"invalid"}`))
	assert.Error(t, err)
}

func TestWsAdapter_SubscriptionsAndAdditionalFeatures(t *testing.T) {
	t.Parallel()

	adapter := bingx.NewWsAdapter()
	pool := pkgws.NewPool("ws://127.0.0.1:1", 30, nil)
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
