package kucoin_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange/kucoin"
	pkgws "crypto-bot/pkg/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWsAdapter_GetChannelExtractor(t *testing.T) {
	t.Parallel()

	adapter := kucoin.NewWsAdapter()
	extractor := adapter.GetChannelExtractor()

	assert.Equal(t, "ticker", extractor([]byte(`{"subject":"tickerV2"}`)))
	assert.Equal(t, "depth", extractor([]byte(`{"subject":"level2"}`)))
	assert.Equal(t, "kline", extractor([]byte(`{"subject":"kline"}`)))
	assert.Equal(t, "personal.position", extractor([]byte(`{"subject":"position.change"}`)))
	assert.Equal(t, "unknown", extractor([]byte(`{"subject":"unknown"}`)))
}

func TestWsAdapter_ParseTicker(t *testing.T) {
	t.Parallel()

	adapter := kucoin.NewWsAdapter()
	raw := []byte(`{
		"subject": "tickerV2",
		"data": {
			"symbol": "XBTUSDTM",
			"price": "50000.5",
			"bestBidPrice": "50000.0",
			"bestAskPrice": "50001.0"
		}
	}`)

	symbol, pd, err := adapter.ParseTicker(raw)
	require.NoError(t, err)
	assert.Equal(t, "XBTUSDTM", symbol)
	assert.Equal(t, 50000.5, pd.LastPrice)
	assert.Equal(t, 50000.0, pd.BestBid)
	assert.Equal(t, 50001.0, pd.BestAsk)

	// Test case for tickerV2 without 'price' field
	rawV2 := []byte(`{"topic":"/contractMarket/tickerV2:BABYUSDTM","type":"message","subject":"tickerV2","sn":1744926189248,"data":{"symbol":"BABYUSDTM","sequence":1744926189248,"bestBidSize":2492,"bestBidPrice":"0.02157","bestAskPrice":"0.02162","bestAskSize":636,"ts":1780661588016000000}}`)
	symbol2, pd2, err2 := adapter.ParseTicker(rawV2)
	require.NoError(t, err2)
	assert.Equal(t, "BABYUSDTM", symbol2)
	assert.Equal(t, 0.02157, pd2.BestBid)
	assert.Equal(t, 0.02162, pd2.BestAsk)
	// Last price should fallback to (bestBid + bestAsk) / 2
	assert.Equal(t, 0.021595, pd2.LastPrice)
}

func TestWsAdapter_ParseDepth(t *testing.T) {
	t.Parallel()

	adapter := kucoin.NewWsAdapter()
	raw := []byte(`{
		"topic": "/contractMarket/level2:XBTUSDTM",
		"data": {
			"asks": [{"price": "50001.0", "volume": "1.5"}],
			"bids": [{"price": "50000.0", "volume": "2.0"}]
		}
	}`)

	symbol, ob, err := adapter.ParseDepth(raw)
	require.NoError(t, err)
	assert.Equal(t, "XBTUSDTM", symbol)
	require.Len(t, ob.Asks, 1)
	require.Len(t, ob.Bids, 1)
	assert.Equal(t, 50001.0, ob.Asks[0].Price)
	assert.Equal(t, 1.5, ob.Asks[0].Volume)
}

func TestWsAdapter_OtherMethods(t *testing.T) {
	t.Parallel()

	adapter := kucoin.NewWsAdapter()

	// 1. GetPingConfig
	pingMsg, interval := adapter.GetPingConfig()
	assert.NotNil(t, pingMsg)
	assert.Equal(t, 20*time.Second, interval)

	// 2. GetAuthHook
	hook := adapter.GetAuthHook("", "")
	assert.Nil(t, hook)

	// 3. SubscribePersonal (needs mock pool set up since it sends message)
	pool := pkgws.NewPool("ws://127.0.0.1:1", 30, slog.Default())
	defer pool.Close()
	adapter.SetPool(pool)
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

	// 8. ParsePosition errors
	_, err = adapter.ParsePosition([]byte{})
	assert.Error(t, err)

	// 9. ParseTicker errors
	_, _, err = adapter.ParseTicker([]byte(`{}`))
	assert.Error(t, err)

	_, _, err = adapter.ParseTicker([]byte(`{"data":"invalid"}`))
	assert.Error(t, err)

	// 10. ParseDepth errors
	_, _, err = adapter.ParseDepth([]byte(`{}`))
	assert.Error(t, err)

	_, _, err = adapter.ParseDepth([]byte(`{"topic":"invalid_topic"}`))
	assert.Error(t, err)

	_, _, err = adapter.ParseDepth([]byte(`{"topic":"depth:BTC", "data":"invalid"}`))
	assert.Error(t, err)
}

func TestWsAdapter_SubscriptionsAndAdditionalFeatures(t *testing.T) {
	t.Parallel()

	// 1. Mock HTTP server for bullet token
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/bullet-public") {
			_, _ = w.Write([]byte(`{
				"code": "200000",
				"data": {
					"token": "mockTokenPublic",
					"instanceServers": [
						{
							"endpoint": "wss://mock.kucoin.com/endpoint",
							"pingInterval": 20000,
							"pingTimeout": 10000
						}
					]
				}
			}`))
		} else if strings.Contains(r.URL.Path, "/bullet-private") {
			_, _ = w.Write([]byte(`{
				"code": "200000",
				"data": {
					"token": "mockTokenPrivate",
					"instanceServers": [
						{
							"endpoint": "wss://mock.kucoin.com/endpoint",
							"pingInterval": 20000,
							"pingTimeout": 10000
						}
					]
				}
			}`))
		}
	}))
	defer server.Close()

	// 2. Initialize REST Client and test GetURLFunc / URL Providers
	ctx := t.Context()

	restClient := kucoin.NewClient(server.Client(), server.URL, "apiKey", "secret", "phrase", config.LoggingConfig{})
	adapter := kucoin.NewWsAdapter()
	adapter.SetClient(restClient)

	pubURLFunc := adapter.GetPublicURLFunc(ctx)
	resolvedPubURL, err := pubURLFunc()
	require.NoError(t, err)
	assert.Equal(t, "wss://mock.kucoin.com/endpoint?token=mockTokenPublic", resolvedPubURL)

	privURLFunc := adapter.GetPrivateURLFunc(ctx)
	resolvedPrivURL, err := privURLFunc()
	require.NoError(t, err)
	assert.Equal(t, "wss://mock.kucoin.com/endpoint?token=mockTokenPrivate", resolvedPrivURL)

	// Verify legacy package-level GetURLFunc
	urlFunc := kucoin.GetURLFunc(ctx, restClient)
	resolvedURL, err := urlFunc()
	require.NoError(t, err)
	assert.Equal(t, "wss://mock.kucoin.com/endpoint?token=mockTokenPublic", resolvedURL)

	// 3. Test WS Subscriptions
	pool := pkgws.NewPool("ws://127.0.0.1:1", 30, slog.Default())
	defer pool.Close()
	adapter.SetPool(pool)

	// Cancel context to avoid any blocks
	subCtx, subCancel := context.WithCancel(context.Background())
	subCancel()

	_ = adapter.SubscribeTicker(subCtx, "BTC-USDT")
	_ = adapter.UnsubscribeTicker(subCtx, "BTC-USDT")
	_ = adapter.SubscribeKline(subCtx, "BTC-USDT")
	_ = adapter.UnsubscribeKline(subCtx, "BTC-USDT")
	_ = adapter.SubscribeDepth(subCtx, "BTC-USDT", "1")
	_ = adapter.UnsubscribeDepth(subCtx, "BTC-USDT", "1")

	// 4. Test Placeholders
	_, _, err = adapter.ParseKline([]byte{})
	assert.Error(t, err)

	_, err = adapter.ParseOrder([]byte{})
	assert.Error(t, err)

	_, err = adapter.ParseOrderDeal([]byte{})
	assert.Error(t, err)

	_, err = adapter.ParseTrackOrder([]byte{})
	assert.Error(t, err)
}

func TestWsAdapter_ParsePosition(t *testing.T) {
	t.Parallel()

	adapter := kucoin.NewWsAdapter()
	raw := []byte(`{
		"topic": "/contract/position:XBTUSDTM",
		"subject": "position.change",
		"data": {
			"symbol": "XBTUSDTM",
			"currentQty": -5.0,
			"avgEntryPrice": "50000.0",
			"liquidationPrice": "40000.0",
			"currentTimestamp": 1672531200000
		}
	}`)

	update, err := adapter.ParsePosition(raw)
	require.NoError(t, err)
	assert.Equal(t, "XBTUSDTM", update.Symbol)
	assert.Equal(t, 5.0, update.HoldVol)
	assert.Equal(t, 2, update.PositionType) // 2 for Short
	assert.Equal(t, 50000.0, update.HoldAvgPrice)
	assert.Equal(t, 40000.0, update.LiquidatePrice)
	assert.Equal(t, int64(1672531200000), update.UpdateTime)

	// Test long position
	rawLong := []byte(`{
		"topic": "/contract/position:XBTUSDTM",
		"subject": "position.change",
		"data": {
			"symbol": "XBTUSDTM",
			"currentQty": 3,
			"avgEntryPrice": 48000.5,
			"liquidationPrice": 35000,
			"currentTimestamp": 1672531200000
		}
	}`)
	updateLong, err := adapter.ParsePosition(rawLong)
	require.NoError(t, err)
	assert.Equal(t, 3.0, updateLong.HoldVol)
	assert.Equal(t, 1, updateLong.PositionType) // 1 for Long
	assert.Equal(t, 48000.5, updateLong.HoldAvgPrice)
}
