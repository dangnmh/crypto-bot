package kucoin_test

import (
	"testing"

	"crypto-bot/internal/infrastructure/exchange/kucoin"

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
