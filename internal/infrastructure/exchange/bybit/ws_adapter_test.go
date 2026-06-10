package bybit_test

import (
	"testing"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/bybit"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWsAdapter_ParsePositionBybitSchema(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"id": "1003076014fb7eedb-c7e6-45d6-a8c1-270f0169171a",
		"topic": "position",
		"creationTime": 1697682317044,
		"data": [{
			"positionIdx": 2,
			"tradeMode": 0,
			"riskId": 1,
			"riskLimitValue": "2000000",
			"symbol": "BTCUSDT",
			"side": "",
			"size": "0",
			"entryPrice": "0",
			"leverage": "10",
			"breakEvenPrice":"93556.73034991",
			"positionValue": "0",
			"positionBalance": "0",
			"markPrice": "28184.5",
			"positionIM": "0",
			"positionIMByMp": "0",
			"positionMM": "0",
			"positionMMByMp": "0",
			"takeProfit": "0",
			"stopLoss": "0",
			"trailingStop": "0",
			"unrealisedPnl": "0",
			"curRealisedPnl": "1.26",
			"cumRealisedPnl": "-25.06579337",
			"sessionAvgPrice": "0",
			"createdTime": "1694402496913",
			"updatedTime": "1697682317038",
			"tpslMode": "Full",
			"liqPrice": "0",
			"bustPrice": "",
			"category": "linear",
			"positionStatus": "Normal",
			"adlRankIndicator": 0,
			"autoAddMargin": 0,
			"leverageSysUpdatedTime": "",
			"mmrSysUpdatedTime": "",
			"seq": 8327597863,
			"isReduceOnly": false
		}]
	}`)

	pos, err := bybit.NewWsAdapter().ParsePosition(raw)
	require.NoError(t, err)
	require.NotNil(t, pos)

	assert.Equal(t, "BTCUSDT", pos.Symbol)
	assert.Equal(t, 0.0, pos.HoldVol)
	assert.Equal(t, exchange.PositionTypeShort, pos.PositionType)
	assert.Equal(t, 10, pos.Leverage)
	assert.Equal(t, 1.26, pos.CloseProfitLoss)
	assert.Equal(t, int64(1697682317038), pos.UpdateTime)
}

func TestWsAdapter_ParsePositionAvgPriceFallback(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"topic": "position",
		"data": [{
			"symbol": "IDUSDT",
			"side": "Sell",
			"size": "497",
			"avgPrice": "0.03016",
			"leverage": "10",
			"positionIdx": 2,
			"positionValue": "14.99946",
			"positionIM": "1.50802066",
			"curRealisedPnl": "-0.00824424"
		}]
	}`)

	pos, err := bybit.NewWsAdapter().ParsePosition(raw)
	require.NoError(t, err)
	require.NotNil(t, pos)

	assert.Equal(t, "IDUSDT", pos.Symbol)
	assert.Equal(t, 497.0, pos.HoldVol)
	assert.Equal(t, 0.03016, pos.HoldAvgPrice)
	assert.Equal(t, 0.03016, pos.OpenAvgPrice)
	assert.Equal(t, exchange.PositionTypeShort, pos.PositionType)
}

func TestWsAdapter_ParsePositionSelectsActiveRow(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"topic": "position",
		"data": [
			{"symbol": "BTCUSDT", "side": "", "size": "0", "positionIdx": 1},
			{"symbol": "BTCUSDT", "side": "Sell", "size": "2", "positionIdx": 2, "entryPrice": "60000"}
		]
	}`)

	pos, err := bybit.NewWsAdapter().ParsePosition(raw)
	require.NoError(t, err)
	require.NotNil(t, pos)

	assert.Equal(t, 2.0, pos.HoldVol)
	assert.Equal(t, exchange.PositionTypeShort, pos.PositionType)
	assert.Equal(t, 60000.0, pos.HoldAvgPrice)
}

func TestWsAdapter_ParsePositionSelectsRecentlyClosedRow(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"topic": "position",
		"data": [
			{"symbol": "AIGENSYNUSDT", "side": "", "size": "0", "positionIdx": 1, "updatedTime": "1780040399979"},
			{"symbol": "AIGENSYNUSDT", "side": "", "size": "0", "positionIdx": 2, "updatedTime": "1780042885945", "cumRealisedPnl": "0.00567"}
		]
	}`)

	pos, err := bybit.NewWsAdapter().ParsePosition(raw)
	require.NoError(t, err)
	require.NotNil(t, pos)

	assert.Equal(t, 0.0, pos.HoldVol)
	assert.Equal(t, exchange.PositionTypeShort, pos.PositionType)
	assert.Equal(t, 0.00567, pos.CloseProfitLoss)
}
