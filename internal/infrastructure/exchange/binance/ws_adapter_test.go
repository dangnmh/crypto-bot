package binance_test

import (
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/binance"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWsAdapter_HooksAndParsing(t *testing.T) {
	t.Parallel()

	adapter := binance.NewWsAdapter()
	require.NotNil(t, adapter)

	// Check extractor routing
	extractor := adapter.GetChannelExtractor()
	assert.Equal(t, "ticker", extractor([]byte(`{"e": "24hrTicker"}`)))
	assert.Equal(t, "depth", extractor([]byte(`{"e": "depthUpdate"}`)))
	assert.Equal(t, "kline", extractor([]byte(`{"e": "kline"}`)))
	assert.Equal(t, "personal.order", extractor([]byte(`{"e": "ORDER_TRADE_UPDATE"}`)))
	assert.Equal(t, "personal.position", extractor([]byte(`{"e": "ACCOUNT_UPDATE"}`)))

	// Check ping config
	ping, interval := adapter.GetPingConfig()
	assert.Equal(t, 3*time.Minute, interval)
	pingMap, ok := ping.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "PING", pingMap["method"])

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

	// Check ParseDepth
	rawDepth := []byte(`{
		"s": "BTCUSDT",
		"b": [["49999", "1.5"]],
		"a": [["50001", "2.5"]],
		"u": 12345
	}`)
	sym, ob, err := adapter.ParseDepth(rawDepth)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", sym)
	assert.Equal(t, int64(12345), ob.Version)
	require.Len(t, ob.Bids, 1)
	assert.Equal(t, 49999.0, ob.Bids[0].Price)
	assert.Equal(t, 1.5, ob.Bids[0].Volume)

	// Check ParseKline
	rawKline := []byte(`{
		"s": "BTCUSDT",
		"k": {
			"t": 1672531200000,
			"o": "50000",
			"c": "50050",
			"h": "50100",
			"l": "49950",
			"v": "10.5",
			"q": "500000"
		}
	}`)
	sym, k, err := adapter.ParseKline(rawKline)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", sym)
	assert.Equal(t, int64(1672531200000), k.Timestamp)
	assert.Equal(t, 50000.0, k.Open)
	assert.Equal(t, 50050.0, k.Close)

	// Check ParseOrder
	rawOrder := []byte(`{
		"o": {
			"s": "BTCUSDT",
			"i": 1234567,
			"c": "external_123",
			"p": "50000",
			"q": "0.5",
			"z": "0.5",
			"ap": "50000",
			"S": "BUY",
			"ps": "LONG",
			"X": "FILLED"
		}
	}`)
	deal, err := adapter.ParseOrder(rawOrder)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", deal.Symbol)
	assert.Equal(t, "1234567", deal.GetOrderID())
	assert.Equal(t, exchange.OrderStateFilled, deal.State)
	assert.Equal(t, exchange.SideOpenLong, deal.Side)

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
