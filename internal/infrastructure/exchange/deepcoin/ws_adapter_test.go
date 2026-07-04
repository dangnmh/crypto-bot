package deepcoin_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/deepcoin"
	pkgws "crypto-bot/pkg/ws"

	"github.com/stretchr/testify/assert"
)

func TestWsAdapter_GetPrivateURLFunc(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/deepcoin/listenkey/acquire" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"code":"0","msg":"","data":{"listenkey":"test-key"}}`))
		}
	}))
	defer server.Close()

	client := deepcoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	adapter := deepcoin.NewWsAdapter("wss://stream.deepcoin.com/v1/private")
	adapter.SetClient(client)

	urlFunc := adapter.GetPrivateURLFunc(context.Background())
	url, err := urlFunc()
	assert.NoError(t, err)
	assert.Equal(t, "wss://stream.deepcoin.com/v1/private?listenKey=test-key", url)
}

func TestWsAdapter_ParseTicker(t *testing.T) {
	t.Parallel()

	adapter := deepcoin.NewWsAdapter("")
	payload := []byte(`{
		"Topic": "market",
		"Data": {
			"Symbol": "BTCUSDT",
			"LastPrice": 95000.5,
			"BidPrice": 95000.0,
			"AskPrice": 95001.0,
			"Volume24": 120.5
		}
	}`)
	sym, pd, err := adapter.ParseTicker(payload)
	assert.NoError(t, err)
	assert.Equal(t, "BTCUSDT", sym)
	assert.Equal(t, 95000.5, pd.LastPrice)
}

func TestWsAdapter_ParsePosition(t *testing.T) {
	t.Parallel()

	adapter := deepcoin.NewWsAdapter("")
	payload := []byte(`{
		"action": "PushPosition",
		"result": [
			{
				"table": "Position",
				"data": {
					"I": "BTCUSDT",
					"p": "1",
					"Po": 55,
					"OP": 29393.10,
					"u": 12.93
				}
			}
		]
	}`)
	update, err := adapter.ParsePosition(payload)
	assert.NoError(t, err)
	assert.Equal(t, "BTCUSDT", update.Symbol)
	assert.Equal(t, 55.0, update.HoldVol)
	assert.Equal(t, 29393.10, update.HoldAvgPrice)
	assert.Equal(t, exchange.PositionTypeLong, update.PositionType)

	// Test position close event (volume 0)
	closePayload := []byte(`{
		"action": "PushPosition",
		"result": [
			{
				"table": "Position",
				"data": {
					"I": "BTCUSDT",
					"p": "1",
					"Po": 0,
					"OP": 29393.10,
					"u": 0
				}
			}
		]
	}`)
	updateClose, err := adapter.ParsePosition(closePayload)
	assert.NoError(t, err)
	assert.Equal(t, "BTCUSDT", updateClose.Symbol)
	assert.Equal(t, 0.0, updateClose.HoldVol)
	assert.Equal(t, exchange.PositionTypeUnknown, updateClose.PositionType)
}

func TestWsAdapter_ParseTickerReal(t *testing.T) {
	t.Parallel()

	adapter := deepcoin.NewWsAdapter("")
	payload := []byte(`{
		"a": "PO",
		"m": "Success",
		"tt": 1757642301185,
		"mt": 1757642301185,
		"d": {
			"I": "BTCUSDT",
			"U": 1757642301089,
			"PF": 1756690200,
			"E": 0.0005251816,
			"O": 114206.7,
			"H": 116346,
			"L": 114132.8,
			"V": 7688046,
			"T": 885654450.392686,
			"N": 115482.9,
			"M": 115473.7,
			"D": 115455.77,
			"V2": 19978848,
			"T2": 2288286517.724497,
			"F": 57727.9,
			"C": 173183.6,
			"BP1": 115482.8,
			"AP1": 115482.9
		}
	}`)
	sym, pd, err := adapter.ParseTicker(payload)
	assert.NoError(t, err)
	assert.Equal(t, "BTCUSDT", sym)
	assert.Equal(t, 115482.9, pd.LastPrice)
	assert.Equal(t, 115482.8, pd.BestBid)
	assert.Equal(t, 115482.9, pd.BestAsk)
	assert.Equal(t, 7688046.0, pd.Volume24)
}

func TestWsAdapter_SubscriptionsAndChannelExtractor(t *testing.T) {
	t.Parallel()

	adapter := deepcoin.NewWsAdapter("wss://stream.deepcoin.com/v1/private")
	pool := pkgws.NewPool("ws://dummy", 30, slog.Default())
	adapter.SetPool(pool)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := adapter.SubscribeTicker(ctx, "BTC-USDT-SWAP")
	assert.Error(t, err)

	err = adapter.UnsubscribeTicker(ctx, "BTC-USDT-SWAP")
	assert.NoError(t, err)

	err = adapter.SubscribePersonal(ctx)
	assert.NoError(t, err)

	pingPayload, pingInterval := adapter.GetPingConfig()
	assert.Equal(t, "ping", pingPayload)
	assert.Equal(t, 15*time.Second, pingInterval)

	authHook := adapter.GetAuthHook("key", "secret")
	assert.Nil(t, authHook)

	extractor := adapter.GetChannelExtractor()
	assert.NotNil(t, extractor)
	assert.Equal(t, "pong", extractor([]byte("pong")))
	assert.Equal(t, "ticker", extractor([]byte(`{"Topic":"market"}`)))
	assert.Equal(t, "ticker", extractor([]byte(`{"a":"PO"}`)))
	assert.Equal(t, "personal.position", extractor([]byte(`{"result":[{"table":"Position"}]}`)))
	assert.Equal(t, "", extractor([]byte(`{}`)))
}
