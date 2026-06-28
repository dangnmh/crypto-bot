package bitunix_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/bitunix"
	pkgws "crypto-bot/pkg/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockClock struct {
	now time.Time
}

func (m mockClock) Now() time.Time {
	return m.now
}

func TestWsAdapter_Lifecycle(t *testing.T) {
	t.Parallel()

	adapter := bitunix.NewWsAdapter()
	pool := pkgws.NewPool("ws://127.0.0.1:1", 30, slog.Default())
	adapter.SetPool(pool)
	adapter.SetClient(nil)

	// Ping config
	ping, interval := adapter.GetPingConfig()
	assert.NotNil(t, ping)
	assert.Equal(t, 30*time.Second, interval)

	// Auth hook (empty credentials)
	hookEmpty := adapter.GetAuthHook("", "")
	assert.Nil(t, hookEmpty)

	// Auth hook (with credentials)
	adapter.SetClock(mockClock{now: time.UnixMilli(1700000000000)})
	hook := adapter.GetAuthHook("my-key", "my-secret")
	assert.NotNil(t, hook)

	// Channel extractor
	extractor := adapter.GetChannelExtractor()
	require.NotNil(t, extractor)

	assert.Equal(t, "login", extractor([]byte(`{"op": "login", "code": 0, "msg": "success"}`)))
	assert.Equal(t, "ticker", extractor([]byte(`{"ch": "tickers"}`)))
	assert.Equal(t, "ticker", extractor([]byte(`{"ch": "ticker"}`)))
	assert.Equal(t, "ticker", extractor([]byte(`{"topic": "ticker:BTCUSDT", "data": {}}`)))
	assert.Equal(t, "personal.order", extractor([]byte(`{"topic": "order", "data": {}}`)))
	assert.Equal(t, "personal.position", extractor([]byte(`{"ch": "position"}`)))
	assert.Equal(t, "personal.position", extractor([]byte(`{"topic": "position", "data": []}`)))
	assert.Equal(t, "pong", extractor([]byte(`{"op": "pong"}`)))
	assert.Equal(t, "", extractor([]byte(`{"topic": "unknown"}`)))

	// Cancel context to test subscriptions without blocking
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_ = adapter.SubscribeTicker(ctx, "BTCUSDT")
	_ = adapter.UnsubscribeTicker(ctx, "BTCUSDT")
	_ = adapter.SubscribePersonal(ctx)
}

func TestWsAdapter_ParseTicker(t *testing.T) {
	t.Parallel()

	adapter := bitunix.NewWsAdapter()
	adapter.SetClock(mockClock{now: time.UnixMilli(1700000000000)})

	// 1. Success case (new tickers channel format)
	newTickerData := []byte(`{
		"ch": "tickers",
		"ts": 1732178884994,
		"data": [
			{
				"s": "BTCUSDT",
				"la": "68650.9",
				"o": "69141.6",
				"h": "70319.9",
				"l": "68241.9",
				"b": "26295.3977",
				"bd": "68650.8",
				"ak": "68651.0"
			}
		]
	}`)
	sym, pd, err := adapter.ParseTicker(newTickerData)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", sym)
	assert.Equal(t, 68650.9, pd.LastPrice)
	assert.Equal(t, 26295.3977, pd.Volume24)
	assert.Equal(t, 68650.8, pd.BestBid)
	assert.Equal(t, 68651.0, pd.BestAsk)

	// 3. Success case (new ticker single channel format)
	singleTickerData := []byte(`{
		"ch": "ticker",
		"ts": 1732178884994,
		"data": {
			"s": "BTCUSDT",
			"la": "68650.9",
			"o": "69141.6",
			"h": "70319.9",
			"l": "68241.9",
			"b": "26295.3977"
		}
	}`)
	sym, pd, err = adapter.ParseTicker(singleTickerData)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", sym)
	assert.Equal(t, 68650.9, pd.LastPrice)
	assert.Equal(t, 26295.3977, pd.Volume24)
	// Bids and asks should be retained from previous update since single channel does not provide them
	assert.Equal(t, 68650.8, pd.BestBid)
	assert.Equal(t, 68651.0, pd.BestAsk)

	// Error case (invalid json)
	_, _, err = adapter.ParseTicker([]byte(`{invalid`))
	require.Error(t, err)

	// Error case (empty symbol)
	_, _, err = adapter.ParseTicker([]byte(`{"topic": "ticker"}`))
	require.Error(t, err)
}

func TestWsAdapter_ParsePosition(t *testing.T) {
	t.Parallel()

	adapter := bitunix.NewWsAdapter()

	// 1. Array format position update (old fields)
	posArrayData := []byte(`{
		"topic": "position",
		"data": [
			{
				"symbol": "BTCUSDT",
				"size": "0.5",
				"entryPrice": "55000.0",
				"side": "LONG",
				"leverage": "20",
				"unrealizedProfit": "150.0",
				"updateTime": 1782576000000
			}
		]
	}`)

	update, err := adapter.ParsePosition(posArrayData)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", update.Symbol)
	assert.Equal(t, 0.5, update.HoldVol)
	assert.Equal(t, 55000.0, update.HoldAvgPrice)
	assert.Equal(t, exchange.PositionTypeLong, update.PositionType)
	assert.Equal(t, 20, update.Leverage)
	assert.Equal(t, 150.0, update.CloseProfitLoss)
	assert.Equal(t, int64(1782576000000), update.UpdateTime)

	// 2. Single object format position update (new fields)
	posSingleData := []byte(`{
		"ch": "position",
		"data": {
			"symbol": "ETHUSDT",
			"qty": "1.2",
			"entryPrice": "3000.0",
			"side": "SHORT",
			"leverage": "10",
			"unrealizedPNL": "-20.0",
			"ctime": 1782576100000
		}
	}`)

	updateSingle, err := adapter.ParsePosition(posSingleData)
	require.NoError(t, err)
	assert.Equal(t, "ETHUSDT", updateSingle.Symbol)
	assert.Equal(t, 1.2, updateSingle.HoldVol)
	assert.Equal(t, 3000.0, updateSingle.HoldAvgPrice)
	assert.Equal(t, exchange.PositionTypeShort, updateSingle.PositionType)
	assert.Equal(t, 10, updateSingle.Leverage)
	assert.Equal(t, -20.0, updateSingle.CloseProfitLoss)
	assert.Equal(t, int64(1782576100000), updateSingle.UpdateTime)

	// 3. Single object format position close update (with ISO-8601 string time and Event: CLOSE)
	posCloseData := []byte(`{
		"ch": "position",
		"data": {
			"symbol": "POWRUSDT",
			"qty": "290",
			"entryPrice": "0.05121",
			"side": "SHORT",
			"leverage": "5",
			"unrealizedPNL": "0.04043",
			"ctime": "2026-06-28T04:35:00.218831000Z",
			"event": "CLOSE"
		}
	}`)

	updateClose, err := adapter.ParsePosition(posCloseData)
	require.NoError(t, err)
	assert.Equal(t, "POWRUSDT", updateClose.Symbol)
	assert.Equal(t, 0.0, updateClose.HoldVol) // Should be 0 since Event is CLOSE
	assert.Equal(t, 0.05121, updateClose.HoldAvgPrice)
	assert.Equal(t, exchange.PositionTypeShort, updateClose.PositionType)
	assert.Equal(t, 5, updateClose.Leverage)
	assert.Equal(t, 0.04043, updateClose.CloseProfitLoss)

	// 4. Position update with stringified numeric ctime
	posStringNumericData := []byte(`{
		"ch": "position",
		"data": {
			"symbol": "POWRUSDT",
			"qty": "304",
			"entryPrice": "0.04914",
			"side": "SHORT",
			"leverage": "5",
			"ctime": "1782624299000"
		}
	}`)

	updateStrNum, err := adapter.ParsePosition(posStringNumericData)
	require.NoError(t, err)
	assert.Equal(t, "POWRUSDT", updateStrNum.Symbol)
	assert.Equal(t, int64(1782624299000), updateStrNum.UpdateTime) // ctime is fallback for UpdateTime

	// Error cases
	_, err = adapter.ParsePosition([]byte(`{invalid`))
	require.Error(t, err)

	_, err = adapter.ParsePosition([]byte(`{"topic": "position", "data": []}`))
	require.Error(t, err)

	_, err = adapter.ParsePosition([]byte(`{"topic": "position", "data": {}}`))
	require.Error(t, err)
}
