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
	"crypto-bot/internal/infrastructure/exchange"
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

	// 4. ParsePosition errors
	_, err = adapter.ParsePosition([]byte{})
	assert.Error(t, err)

	// 5. ParseTicker errors
	_, _, err = adapter.ParseTicker([]byte(`{}`))
	assert.Error(t, err)

	_, _, err = adapter.ParseTicker([]byte(`{"data":"invalid"}`))
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
	assert.True(t, strings.HasPrefix(resolvedPubURL, "wss://mock.kucoin.com/endpoint?token=mockTokenPublic&connectId="))

	privURLFunc := adapter.GetPrivateURLFunc(ctx)
	resolvedPrivURL, err := privURLFunc()
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(resolvedPrivURL, "wss://mock.kucoin.com/endpoint?token=mockTokenPrivate&connectId="))

	// Verify legacy package-level GetURLFunc
	urlFunc := kucoin.GetURLFunc(ctx, restClient)
	resolvedURL, err := urlFunc()
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(resolvedURL, "wss://mock.kucoin.com/endpoint?token=mockTokenPublic&connectId="))

	// 3. Test WS Subscriptions
	pool := pkgws.NewPool("ws://127.0.0.1:1", 30, slog.Default())
	defer pool.Close()
	adapter.SetPool(pool)

	// Cancel context to avoid any blocks
	subCtx, subCancel := context.WithCancel(context.Background())
	subCancel()

	_ = adapter.SubscribeTicker(subCtx, "BTC-USDT")
	_ = adapter.UnsubscribeTicker(subCtx, "BTC-USDT")
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
	assert.Equal(t, 5.0, update.HoldVolContract)
	assert.Equal(t, exchange.PositionTypeShort, update.PositionType) // 2 for Short
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
	assert.Equal(t, 3.0, updateLong.HoldVolContract)
	assert.Equal(t, exchange.PositionTypeLong, updateLong.PositionType) // 1 for Long
	assert.Equal(t, 48000.5, updateLong.HoldAvgPrice)

	// Test closed short position using positionSide
	rawClosedShort := []byte(`{
		"topic": "/contract/position:XBTUSDTM",
		"subject": "position.change",
		"data": {
			"symbol": "XBTUSDTM",
			"currentQty": 0,
			"avgEntryPrice": 48000.5,
			"currentTimestamp": 1672531200000,
			"positionSide": "SHORT"
		}
	}`)
	updateClosedShort, err := adapter.ParsePosition(rawClosedShort)
	require.NoError(t, err)
	assert.Equal(t, 0.0, updateClosedShort.HoldVolContract)
	assert.Equal(t, exchange.PositionTypeShort, updateClosedShort.PositionType) // 2 for Short

	// Test closed long position using positionSide
	rawClosedLong := []byte(`{
		"topic": "/contract/position:XBTUSDTM",
		"subject": "position.change",
		"data": {
			"symbol": "XBTUSDTM",
			"currentQty": 0,
			"avgEntryPrice": 48000.5,
			"currentTimestamp": 1672531200000,
			"positionSide": "LONG"
		}
	}`)
	updateClosedLong, err := adapter.ParsePosition(rawClosedLong)
	require.NoError(t, err)
	assert.Equal(t, 0.0, updateClosedLong.HoldVolContract)
	assert.Equal(t, exchange.PositionTypeLong, updateClosedLong.PositionType) // 1 for Long

	// Test case from user report
	rawUserEvent := []byte(`{
		"type": "message",
		"topic": "/contract/positionAll",
		"subject": "position.change",
		"data": {
			"symbol": "XBTUSDTM",
			"maintMarginReq": 0.004,
			"riskLimit": 50000000,
			"realLeverage": 19.1376874933,
			"crossMode": false,
			"delevPercentage": 0.87,
			"openingTimestamp": 1771400783360,
			"autoDeposit": false,
			"currentTimestamp": 1771474169458,
			"currentQty": -1,
			"currentCost": -68.0942,
			"currentComm": 0.03794569,
			"unrealisedCost": -68.0942,
			"realisedCost": 0.03794569,
			"isOpen": true,
			"markPrice": 66954.6,
			"markValue": -66.9546,
			"posCost": -68.0942,
			"posCross": 1,
			"posInit": 1.361884,
			"posComm": 0,
			"posLoss": 0.00291083,
			"posMargin": 2.35897317,
			"posFunding": -0.00291083,
			"posMaint": 0.30799116,
			"maintMargin": 3.49857317,
			"avgEntryPrice": 68094.2,
			"liquidationPrice": 70130.5725363,
			"bankruptPrice": 70453.17317,
			"settleCurrency": "USDT",
			"changeReason": "changeRiskLimit",
			"riskLimitLevel": 5,
			"realisedGrossCost": 0.0,
			"realisedGrossPnl": 0.0,
			"realisedPnl": -0.04376735,
			"unrealisedPnl": 1.1396,
			"unrealisedPnlPcnt": 0.0167,
			"unrealisedRoePcnt": 0.8368,
			"leverage": 19.1376874933,
			"marginMode": "ISOLATED",
			"positionSide": "SHORT",
			"tax": 0,
			"dealComm": -0.04085652,
			"fundingFee": -0.00291083,
			"aggRate": 0.0046
		}
	}`)
	updateUserEvent, err := adapter.ParsePosition(rawUserEvent)
	require.NoError(t, err)
	assert.Equal(t, "XBTUSDTM", updateUserEvent.Symbol)
	assert.Equal(t, 1.0, updateUserEvent.HoldVolContract)
	assert.Equal(t, exchange.PositionTypeShort, updateUserEvent.PositionType) // Short
	assert.Equal(t, 68094.2, updateUserEvent.HoldAvgPrice)
	assert.Equal(t, 70130.5725363, updateUserEvent.LiquidatePrice)
	assert.Equal(t, int64(1771474169458), updateUserEvent.UpdateTime)
}

func TestWsAdapter_ParseDepth(t *testing.T) {
	t.Parallel()

	adapter := kucoin.NewWsAdapter()

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

func TestWsAdapter_SubscribeDepth(t *testing.T) {
	t.Parallel()

	adapter := kucoin.NewWsAdapter()
	pool := pkgws.NewPool("ws://127.0.0.1:1", 30, slog.Default())
	defer pool.Close()
	adapter.SetPool(pool)

	subCtx, subCancel := context.WithCancel(context.Background())
	subCancel()

	_ = adapter.SubscribeDepth(subCtx, "XBTUSDTM")
	_ = adapter.UnsubscribeDepth(subCtx, "XBTUSDTM")
}
