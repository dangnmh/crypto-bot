package kucoin_test

import (
	"context"
	"net/http"
	"net/http/httptest"
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

	assert.Equal(t, "tickers", extractor([]byte(`{"subject":"tickerV2"}`)))
	assert.Equal(t, "depth", extractor([]byte(`{"subject":"level2"}`)))
	assert.Equal(t, "kline", extractor([]byte(`{"subject":"kline"}`)))
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
		assert.Contains(t, r.URL.Path, "/bullet-public")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "200000",
			"data": {
				"token": "mockToken",
				"instanceServers": [
					{
						"endpoint": "wss://mock.kucoin.com/endpoint",
						"pingInterval": 20000,
						"pingTimeout": 10000
					}
				]
			}
		}`))
	}))
	defer server.Close()

	// 2. Initialize REST Client and test GetURLFunc
	ctx := t.Context()

	restClient := kucoin.NewClient(server.Client(), server.URL, "apiKey", "secret", "phrase", config.LoggingConfig{})
	urlFunc := kucoin.GetURLFunc(ctx, restClient)
	resolvedURL, err := urlFunc()
	require.NoError(t, err)
	assert.Equal(t, "wss://mock.kucoin.com/endpoint?token=mockToken", resolvedURL)

	// 3. Test WS Subscriptions
	adapter := kucoin.NewWsAdapter()
	pool := pkgws.NewPool("ws://127.0.0.1:1", 30, nil)
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

	_, err = adapter.ParsePosition([]byte{})
	assert.Error(t, err)
}
