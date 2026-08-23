package futures_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/kucoin/futures"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFuturesWsAdapter_ParseDepth(t *testing.T) {
	t.Parallel()
	adapter := futures.NewWsAdapter()

	// 1. Valid ask update with sequence and price,side,size
	rawAsk := []byte(`{
		"topic": "/contractMarket/level2:XBTUSDTM",
		"type": "message",
		"subject": "level2",
		"sn": 1709400450243,
		"data": {
			"sequence": 1709400450243,
			"change": "90631.2,sell,2",
			"timestamp": 1731897467182
		}
	}`)

	sym, ob, err := adapter.ParseDepth(rawAsk)
	require.NoError(t, err)
	assert.Equal(t, "XBTUSDTM", sym)
	require.NotNil(t, ob)
	assert.Equal(t, int64(1709400450243), ob.Version)
	assert.Empty(t, ob.Bids)
	require.Len(t, ob.Asks, 1)
	assert.Equal(t, 90631.2, ob.Asks[0].Price)
	assert.Equal(t, 2.0, ob.Asks[0].Volume)

	// 2. Valid bid deletion (size == 0)
	rawBidDel := []byte(`{
		"topic": "/contractMarket/level2:XBTUSDTM",
		"type": "message",
		"subject": "level2",
		"sn": 1709400450244,
		"data": {
			"sequence": 1709400450244,
			"change": "3988.50,buy,0",
			"timestamp": 1731897467190
		}
	}`)

	sym2, ob2, err2 := adapter.ParseDepth(rawBidDel)
	require.NoError(t, err2)
	assert.Equal(t, "XBTUSDTM", sym2)
	require.NotNil(t, ob2)
	assert.Equal(t, int64(1709400450244), ob2.Version)
	require.Len(t, ob2.Bids, 1)
	assert.Equal(t, 3988.50, ob2.Bids[0].Price)
	assert.Equal(t, 0.0, ob2.Bids[0].Volume)
	assert.Empty(t, ob2.Asks)

	// 3. Invalid change string
	rawInvalid := []byte(`{
		"topic": "/contractMarket/level2:XBTUSDTM",
		"data": {
			"sequence": 100,
			"change": "invalid"
		}
	}`)
	_, _, errInvalid := adapter.ParseDepth(rawInvalid)
	assert.Error(t, errInvalid)
}

func TestFuturesWsAdapter_ParseTicker(t *testing.T) {
	t.Parallel()
	adapter := futures.NewWsAdapter()

	tickerMsg := []byte(`{
		"topic": "/contractMarket/tickerV2:XBTUSDTM",
		"subject": "tickerV2",
		"data": {
			"symbol": "XBTUSDTM",
			"bestBidPrice": 50000.0,
			"bestBidSize": 10,
			"bestAskPrice": 50001.0,
			"bestAskSize": 15,
			"ts": 1672531200000
		}
	}`)
	sym, pd, err := adapter.ParseTicker(tickerMsg)
	require.NoError(t, err)
	assert.Equal(t, "XBTUSDTM", sym)
	assert.Equal(t, 50000.0, pd.BestBid)
	assert.Equal(t, 50001.0, pd.BestAsk)
	assert.Equal(t, 50000.5, pd.LastPrice)

	// Errors
	_, _, err = adapter.ParseTicker([]byte(`invalid`))
	require.Error(t, err)
}

func TestFuturesWsAdapter_ParsePosition(t *testing.T) {
	t.Parallel()
	adapter := futures.NewWsAdapter()

	rawShort := []byte(`{
		"topic": "/contract/position:XBTUSDTM",
		"subject": "position.change",
		"data": {
			"symbol": "XBTUSDTM",
			"currentQty": -5,
			"avgEntryPrice": 50000,
			"liquidationPrice": 40000,
			"currentTimestamp": 1672531200000
		}
	}`)
	update, err := adapter.ParsePosition(rawShort)
	require.NoError(t, err)
	assert.Equal(t, "XBTUSDTM", update.Symbol)
	assert.Equal(t, 5.0, update.HoldVolContract)
	assert.Equal(t, exchange.PositionTypeShort, update.PositionType)
	assert.Equal(t, 50000.0, update.HoldAvgPrice)
	assert.Equal(t, 40000.0, update.LiquidatePrice)
	assert.Equal(t, int64(1672531200000), update.UpdateTime)

	// Long position
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
	assert.Equal(t, 3.0, updateLong.HoldVolContract)
	assert.Equal(t, exchange.PositionTypeLong, updateLong.PositionType)
	assert.Equal(t, 48000.5, updateLong.HoldAvgPrice)
}

func TestFuturesWsAdapter_GetURLFunc(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code": "200000",
			"data": {
				"token": "test_token",
				"instanceServers": [{
					"endpoint": "wss://ws-api.kucoin.com/endpoint",
					"pingInterval": 20000,
					"pingTimeout": 10000
				}]
			}
		}`))
	}))
	defer server.Close()

	client := futures.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
	adapter := futures.NewWsAdapter()
	adapter.SetClient(client)

	urlFunc := adapter.GetPrivateURLFunc(context.Background())
	u, err := urlFunc()
	require.NoError(t, err)
	assert.Contains(t, u, "wss://ws-api.kucoin.com/endpoint?token=test_token&connectId=")

	pubUrlFunc := adapter.GetPublicURLFunc(context.Background())
	pubU, err := pubUrlFunc()
	require.NoError(t, err)
	assert.Contains(t, pubU, "wss://ws-api.kucoin.com/endpoint?token=test_token&connectId=")
}

func TestFuturesWsAdapter_Subscriptions(t *testing.T) {
	t.Parallel()
	adapter := futures.NewWsAdapter()
	ctx := context.Background()

	assert.NoError(t, adapter.SubscribeDepth(ctx, "XBTUSDM"))
	assert.NoError(t, adapter.UnsubscribeDepth(ctx, "XBTUSDM"))
	assert.NoError(t, adapter.SubscribeTicker(ctx, "XBTUSDM"))
	assert.NoError(t, adapter.UnsubscribeTicker(ctx, "XBTUSDM"))
	assert.NoError(t, adapter.SubscribePersonal(ctx))
	assert.NoError(t, adapter.UnsubscribePersonal(ctx))

	pingPayload, dur := adapter.GetPingConfig()
	assert.NotNil(t, pingPayload)
	assert.Greater(t, dur, int64(0))

	assert.Nil(t, adapter.GetAuthHook("key", "secret"))
}
