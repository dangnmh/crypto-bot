package binance_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange/binance"
	pkgws "crypto-bot/pkg/ws"
	"log/slog"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWsAdapter_HooksAndParsing(t *testing.T) {
	t.Parallel()

	adapter := binance.NewWsAdapter("")
	require.NotNil(t, adapter)

	// Check extractor routing
	extractor := adapter.GetChannelExtractor()
	assert.Equal(t, "ticker", extractor([]byte(`{"e": "24hrTicker"}`)))
	assert.Equal(t, "ticker", extractor([]byte(`{"e": "24hrTicker", "E": 1780208818274}`)))
	assert.Equal(t, "ticker", extractor([]byte(`{"e": "24hrMiniTicker"}`)))
	assert.Equal(t, "depth", extractor([]byte(`{"e": "depthUpdate"}`)))
	assert.Equal(t, "kline", extractor([]byte(`{"e": "kline"}`)))
	assert.Equal(t, "personal.order", extractor([]byte(`{"e": "ORDER_TRADE_UPDATE"}`)))
	assert.Equal(t, "personal.position", extractor([]byte(`{"e": "ACCOUNT_UPDATE"}`)))

	// Check ping config
	ping, interval := adapter.GetPingConfig()
	assert.Equal(t, time.Duration(0), interval)
	assert.Nil(t, ping)

	// Check ParseTicker
	rawTicker := []byte(`{
		"s": "BTCUSDT",
		"c": "50000.0",
		"b": "49999.0",
		"a": "50001.0",
		"v": "100.0"
	}`)
	sym, pd, err := adapter.ParseTicker(rawTicker)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", sym)
	assert.Equal(t, 50000.0, pd.LastPrice)
	assert.Equal(t, 49999.0, pd.BestBid)
	assert.Equal(t, 50001.0, pd.BestAsk)
	assert.Equal(t, 50000.0, pd.FairPrice)

	// Check ParseTicker with actual case-insensitive e/E collision payload
	rawCollision := []byte(`{"e":"24hrTicker","E":1780208818274,"s":"HIVEUSDT","p":"0.0196800","P":"33.561","w":"0.0698741","c":"0.0783200","Q":"88","o":"0.0586400","h":"0.0792500","l":"0.0579000","v":"500096749","q":"34943789.3428000","O":1780122360000,"C":1780208818272,"F":101251824,"L":101704076,"n":452236}`)
	symC, pdC, errC := adapter.ParseTicker(rawCollision)
	require.NoError(t, errC)
	assert.Equal(t, "HIVEUSDT", symC)
	assert.Equal(t, 0.0783200, pdC.LastPrice)
	assert.Equal(t, 0.0783200, pdC.FairPrice)
	assert.Equal(t, 500096749.0, pdC.Volume24)

	// Check ParseTicker with standard bookTicker payload falling back to mid-price
	rawBookTicker := []byte(`{
		"e":"bookTicker",
		"u":400900217,
		"E":1568014460893,
		"T":1568014460891,
		"s":"BNBUSDT",
		"b":"25.35190000",
		"B":"31.21000000",
		"a":"25.36520000",
		"A":"40.66000000"
	}`)
	symB, pdB, errB := adapter.ParseTicker(rawBookTicker)
	require.NoError(t, errB)
	assert.Equal(t, "BNBUSDT", symB)
	assert.Equal(t, 25.35190000, pdB.BestBid)
	assert.Equal(t, 25.36520000, pdB.BestAsk)
	assert.Equal(t, (25.35190000+25.36520000)/2.0, pdB.LastPrice)
	assert.Equal(t, pdB.LastPrice, pdB.FairPrice)

	// Check ParseTicker with 24hrMiniTicker payload
	rawMiniTicker := []byte(`{
		"e":"24hrMiniTicker",
		"E":123456789,
		"s":"BTCUSDT",
		"c":"0.0025",
		"o":"0.0010",
		"h":"0.0025",
		"l":"0.0010",
		"v":"10000",
		"q":"18"
	}`)
	symM, pdM, errM := adapter.ParseTicker(rawMiniTicker)
	require.NoError(t, errM)
	assert.Equal(t, "BTCUSDT", symM)
	assert.Equal(t, 0.0025, pdM.LastPrice)
	assert.Equal(t, 0.0025, pdM.FairPrice)
	assert.Equal(t, 10000.0, pdM.Volume24)

	// Check ParsePosition
	rawPos := []byte(`{
		"a": {
			"P": [{
				"s": "BTCUSDT",
				"pa": "0.5",
				"ep": "50000.0",
				"up": "10.0",
				"ps": "LONG"
			}]
		}
	}`)
	pos, err := adapter.ParsePosition(rawPos)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", pos.Symbol)
	assert.Equal(t, 0.5, pos.HoldVol)
	assert.Equal(t, 50000.0, pos.HoldAvgPrice)
}

func TestWsAdapter_SubscriptionsAndAdditionalFeatures(t *testing.T) {
	t.Parallel()

	adapter := binance.NewWsAdapter("wss://fstream.binance.com/private/ws")
	adapter.SetURLs("wss://fstream.binance.com/public", "wss://fstream.binance.com/market")
	pool := pkgws.NewPool("ws://127.0.0.1:1", 30, slog.Default())
	defer pool.Close()
	adapter.SetPool(pool)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_ = adapter.SubscribeTicker(ctx, "BTCUSDT")
	_ = adapter.UnsubscribeTicker(ctx, "BTCUSDT")
	_ = adapter.SubscribeKline(ctx, "BTCUSDT")
	_ = adapter.UnsubscribeKline(ctx, "BTCUSDT")
	_ = adapter.SubscribeDepth(ctx, "BTCUSDT", "1")
	_ = adapter.UnsubscribeDepth(ctx, "BTCUSDT", "1")
	_ = adapter.SubscribePersonal(ctx)

	hook := adapter.GetAuthHook("apiKey", "apiSecret")
	assert.Nil(t, hook)
}

func TestWsAdapter_GetPrivateURLFunc(t *testing.T) {
	t.Parallel()

	// Start a mock REST server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" && r.URL.Path == "/fapi/v1/listenKey" {
			_, _ = w.Write([]byte(`{"listenKey": "mocked_listen_key_123"}`))
			return
		}
	}))
	t.Cleanup(server.Close)

	client := binance.NewClient(server.Client(), server.URL, "apiKey", "apiSecret", config.LoggingConfig{})

	t.Run("Default fallback URL", func(t *testing.T) {
		t.Parallel()
		adapter := binance.NewWsAdapter("")
		adapter.SetClient(client)

		urlFunc := adapter.GetPrivateURLFunc(context.Background())
		url, err := urlFunc()
		require.NoError(t, err)
		assert.Equal(t, "wss://fstream.binance.com/private/ws/mocked_listen_key_123", url)
	})

	t.Run("Configured explicit private URL", func(t *testing.T) {
		t.Parallel()
		adapter := binance.NewWsAdapter("wss://fstream.binance.com/private/ws")
		adapter.SetClient(client)

		urlFunc := adapter.GetPrivateURLFunc(context.Background())
		url, err := urlFunc()
		require.NoError(t, err)
		assert.Equal(t, "wss://fstream.binance.com/private/ws/mocked_listen_key_123", url)
	})

	t.Run("Custom domain or test URL", func(t *testing.T) {
		t.Parallel()
		adapter := binance.NewWsAdapter("wss://test-futures.binance.com/private/ws/")
		adapter.SetClient(client)

		urlFunc := adapter.GetPrivateURLFunc(context.Background())
		url, err := urlFunc()
		require.NoError(t, err)
		assert.Equal(t, "wss://test-futures.binance.com/private/ws/mocked_listen_key_123", url)
	})
}
