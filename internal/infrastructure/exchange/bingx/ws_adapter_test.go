package bingx_test

import (
	"testing"

	"crypto-bot/internal/infrastructure/exchange/bingx"

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
