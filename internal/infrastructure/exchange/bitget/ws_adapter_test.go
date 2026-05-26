package bitget_test

import (
	"testing"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/bitget"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWsAdapter_GetChannelExtractor(t *testing.T) {
	t.Parallel()

	adapter := bitget.NewWsAdapter()
	extractor := adapter.GetChannelExtractor()

	assert.Equal(t, "tickers", extractor([]byte(`{"arg":{"channel":"ticker","instId":"BTCUSDT"}}`)))
	assert.Equal(t, "kline", extractor([]byte(`{"arg":{"channel":"candle1m","instId":"BTCUSDT"}}`)))
	assert.Equal(t, "depth", extractor([]byte(`{"arg":{"channel":"books","instId":"BTCUSDT"}}`)))
	assert.Equal(t, "personal.order", extractor([]byte(`{"arg":{"channel":"orders"}}`)))
	assert.Equal(t, "personal.position", extractor([]byte(`{"arg":{"channel":"positions"}}`)))
}

func TestWsAdapter_ParseTicker(t *testing.T) {
	t.Parallel()

	adapter := bitget.NewWsAdapter()
	raw := []byte(`{
		"arg": {"channel": "ticker", "instId": "BTCUSDT"},
		"data": [
			{
				"symbol": "BTCUSDT",
				"lastPr": "50000.5",
				"bidPr": "50000.0",
				"askPr": "50001.0",
				"baseVolume": "1000"
			}
		]
	}`)

	symbol, pd, err := adapter.ParseTicker(raw)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", symbol)
	assert.Equal(t, 50000.5, pd.LastPrice)
	assert.Equal(t, 50000.0, pd.BestBid)
	assert.Equal(t, 50001.0, pd.BestAsk)
	assert.Equal(t, 1000.0, pd.Volume24)
}

func TestWsAdapter_ParseDepth(t *testing.T) {
	t.Parallel()

	adapter := bitget.NewWsAdapter()
	raw := []byte(`{
		"arg": {"channel": "books", "instId": "BTCUSDT"},
		"data": [
			{
				"symbol": "BTCUSDT",
				"asks": [["50001.0", "1.5"]],
				"bids": [["50000.0", "2.0"]],
				"ts": "1695812285073"
			}
		]
	}`)

	symbol, ob, err := adapter.ParseDepth(raw)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", symbol)
	assert.Equal(t, int64(1695812285073), ob.Version)
	require.Len(t, ob.Asks, 1)
	require.Len(t, ob.Bids, 1)
	assert.Equal(t, 50001.0, ob.Asks[0].Price)
	assert.Equal(t, 1.5, ob.Asks[0].Volume)
}

func TestWsAdapter_ParseKline(t *testing.T) {
	t.Parallel()

	adapter := bitget.NewWsAdapter()
	raw := []byte(`{
		"arg": {"channel": "candle1m", "instId": "BTCUSDT"},
		"data": [
			["1695812285000", "50000.0", "50001.0", "49999.0", "50000.5", "10", "500000"]
		]
	}`)

	symbol, k, err := adapter.ParseKline(raw)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", symbol)
	assert.Equal(t, int64(1695812285000), k.Timestamp)
	assert.Equal(t, 50000.0, k.Open)
	assert.Equal(t, 50000.5, k.Close)
}

func TestWsAdapter_ParseOrder(t *testing.T) {
	t.Parallel()

	adapter := bitget.NewWsAdapter()
	raw := []byte(`{
		"arg": {"channel": "orders"},
		"data": [
			{
				"symbol": "BTCUSDT",
				"orderId": "12345",
				"clientOid": "my_client_id",
				"price": "50000",
				"size": "2",
				"side": "buy",
				"posSide": "long",
				"state": "filled",
				"baseVolume": "2",
				"priceAvg": "50000",
				"cTime": "1695812285000",
				"uTime": "1695812285073"
			}
		]
	}`)

	deal, err := adapter.ParseOrder(raw)
	require.NoError(t, err)
	assert.Equal(t, "12345", deal.OrderID)
	assert.Equal(t, "BTCUSDT", deal.Symbol)
	assert.Equal(t, exchange.SideOpenLong, deal.Side)
	assert.Equal(t, exchange.OrderStateFilled, deal.State)
}

func TestWsAdapter_ParsePosition(t *testing.T) {
	t.Parallel()

	adapter := bitget.NewWsAdapter()
	raw := []byte(`{
		"arg": {"channel": "positions"},
		"data": [
			{
				"symbol": "BTCUSDT",
				"total": "1",
				"leverage": "10",
				"openPriceAvg": "50000",
				"liquidationPrice": "45000",
				"achievedProfits": "5.5",
				"marginSize": "5000",
				"holdSide": "long",
				"marginMode": "crossed"
			}
		]
	}`)

	update, err := adapter.ParsePosition(raw)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", update.Symbol)
	assert.Equal(t, 1.0, update.HoldVol)
	assert.Equal(t, 10, update.Leverage)
	assert.Equal(t, 1, update.PositionType)
}
