package tradecalc_test

import (
	"errors"
	"testing"

	"crypto-bot/pkg/tradecalc"

	"github.com/stretchr/testify/assert"
)

func TestCalculateIOCPrice(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                string
		side                tradecalc.Side
		bestBid             float64
		bestAsk             float64
		maxPriceDiffPercent float64
		priceUnit           float64
		priceScale          int
		want                float64
		wantErr             error
	}{
		{
			name:      "invalid price unit",
			side:      tradecalc.SideOpenLong,
			priceUnit: 0,
			wantErr:   tradecalc.ErrInvalidPriceUnit,
		},
		{
			name:      "invalid side",
			side:      tradecalc.SideUnknown,
			priceUnit: 0.1,
			wantErr:   tradecalc.ErrInvalidSide,
		},
		{
			name:      "zero ref price long",
			side:      tradecalc.SideOpenLong,
			bestAsk:   0,
			priceUnit: 0.1,
			wantErr:   tradecalc.ErrZeroRefPrice,
		},
		{
			name:      "zero ref price short",
			side:      tradecalc.SideOpenShort,
			bestBid:   0,
			priceUnit: 0.1,
			wantErr:   tradecalc.ErrZeroRefPrice,
		},
		{
			name:                "LONG basic",
			side:                tradecalc.SideOpenLong,
			bestBid:             65000,
			bestAsk:             65010,
			maxPriceDiffPercent: 0.002,
			priceUnit:           0.1,
			priceScale:          1,
			want:                65011.3,
		},
		{
			name:                "SHORT basic",
			side:                tradecalc.SideOpenShort,
			bestBid:             65000,
			bestAsk:             65010,
			maxPriceDiffPercent: 0.002,
			priceUnit:           0.1,
			priceScale:          1,
			want:                64998.7,
		},
		{
			name:                "LONG aggressiveness sanity check trigger",
			side:                tradecalc.SideOpenLong,
			bestBid:             100.05,
			bestAsk:             100.05,
			maxPriceDiffPercent: 0.0,
			priceUnit:           0.09,
			priceScale:          0,
			want:                100.0, // rounds from 100.17 to 100.0 (less than 100.05), snapped/clamped
		},
		{
			name:                "SHORT aggressiveness sanity check trigger",
			side:                tradecalc.SideOpenShort,
			bestBid:             100.95,
			bestAsk:             100.95,
			maxPriceDiffPercent: 0.0,
			priceUnit:           0.09,
			priceScale:          0,
			want:                101.0, // rounds to 101.0 (greater than 100.95), triggers clamp to 101.0
		},
		{
			name:                "zero IOC price trigger",
			side:                tradecalc.SideOpenShort,
			bestBid:             0.1,
			maxPriceDiffPercent: 100.0,
			priceUnit:           0.1,
			priceScale:          1,
			wantErr:             tradecalc.ErrZeroIOCPrice,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tradecalc.CalculateIOCPrice(tt.side, tt.bestBid, tt.bestAsk, tt.maxPriceDiffPercent, tt.priceUnit, tt.priceScale)
			if tt.wantErr != nil {
				assert.True(t, errors.Is(err, tt.wantErr))
			} else {
				assert.NoError(t, err)
				assert.InDelta(t, tt.want, got, 1e-9)
			}
		})
	}
}

func TestCalculateSlippage(t *testing.T) {
	t.Parallel()
	// slippage < priceUnit*2
	got1 := tradecalc.CalculateSlippage(10, 0.0001, 0.1)
	assert.InDelta(t, 0.2, got1, 1e-9)

	// slippage >= priceUnit*2
	got2 := tradecalc.CalculateSlippage(1000, 1.0, 0.1)
	assert.InDelta(t, 10.0, got2, 1e-9)
}

func TestCalculateVolume(t *testing.T) {
	t.Parallel()
	// contractSize <= 0
	assert.Zero(t, tradecalc.CalculateVolume(10, 20, 0, 100, 1, 2))
	// refPrice <= 0
	assert.Zero(t, tradecalc.CalculateVolume(10, 20, 1, 0, 1, 2))

	// normal vol >= minVol
	got1 := tradecalc.CalculateVolume(10, 20, 0.0001, 65000, 1, 0)
	assert.InDelta(t, 30.0, got1, 1e-9)

	// vol < minVol clamps to minVol
	got2 := tradecalc.CalculateVolume(1, 1, 1.0, 50000, 5, 0)
	assert.InDelta(t, 5.0, got2, 1e-9)
}

func TestExecutionRefPrice(t *testing.T) {
	t.Parallel()
	assert.InDelta(t, 102.0, tradecalc.ExecutionRefPrice(tradecalc.SideOpenLong, 100, 99, 102), 1e-9)
	assert.InDelta(t, 98.0, tradecalc.ExecutionRefPrice(tradecalc.SideOpenShort, 100, 98, 102), 1e-9)
	assert.InDelta(t, 100.0, tradecalc.ExecutionRefPrice(tradecalc.SideUnknown, 100, 98, 102), 1e-9)
	assert.InDelta(t, 100.0, tradecalc.ExecutionRefPrice(tradecalc.SideOpenLong, 100, 99, 0), 1e-9)
	assert.InDelta(t, 100.0, tradecalc.ExecutionRefPrice(tradecalc.SideOpenShort, 100, 0, 102), 1e-9)
}

func TestCalculateVolumeForNotional(t *testing.T) {
	t.Parallel()
	assert.Zero(t, tradecalc.CalculateVolumeForNotional(0, 100, 1, 2, 0))
	assert.Zero(t, tradecalc.CalculateVolumeForNotional(100, 0, 1, 2, 0))
	assert.Zero(t, tradecalc.CalculateVolumeForNotional(100, 100, 0, 2, 0))

	// Clamps to minVol
	got := tradecalc.CalculateVolumeForNotional(1, 100, 1, 2, 0)
	assert.InDelta(t, 2.0, got, 1e-9)
}

func TestReversionNotionalUSDT(t *testing.T) {
	t.Parallel()
	assert.InDelta(t, 200.0, tradecalc.ReversionNotionalUSDT(10, 20), 1e-9)
}

func TestNotionalForVolume(t *testing.T) {
	t.Parallel()
	assert.Zero(t, tradecalc.NotionalForVolume(0, 100, 1))
	assert.Zero(t, tradecalc.NotionalForVolume(10, 0, 1))
	assert.Zero(t, tradecalc.NotionalForVolume(10, 100, 0))

	assert.InDelta(t, 1000.0, tradecalc.NotionalForVolume(10, 100, 1), 1e-9)
}

func TestGetPeakPrice(t *testing.T) {
	t.Parallel()
	assert.InDelta(t, 102.0, tradecalc.GetPeakPrice(tradecalc.SideOpenLong, 98, 102), 1e-9)
	assert.InDelta(t, 98.0, tradecalc.GetPeakPrice(tradecalc.SideOpenShort, 98, 102), 1e-9)
}

func TestFindWallPrice(t *testing.T) {
	t.Parallel()
	levels := []tradecalc.OrderBookEntry{
		{Price: 10, Volume: 5},
		{Price: 11, Volume: 5},
		{Price: 12, Volume: 50}, // Wall
	}

	// len(levels) < minWallLevels
	assert.Zero(t, tradecalc.FindWallPrice(10, levels[:2], tradecalc.SideOpenLong, 3, 3.0))

	// entryPrice <= 0
	assert.Zero(t, tradecalc.FindWallPrice(0, levels, tradecalc.SideOpenLong, 3, 3.0))

	// SideOpenLong -> price > entry
	assert.InDelta(t, 12.0, tradecalc.FindWallPrice(10, levels, tradecalc.SideOpenLong, 3, 2.0), 1e-9)

	// SideOpenShort -> price < entry
	levelsShort := []tradecalc.OrderBookEntry{
		{Price: 12, Volume: 5},
		{Price: 11, Volume: 5},
		{Price: 10, Volume: 50}, // Wall
	}
	assert.InDelta(t, 10.0, tradecalc.FindWallPrice(12, levelsShort, tradecalc.SideOpenShort, 3, 2.0), 1e-9)

	// No wall found due to threshold
	assert.Zero(t, tradecalc.FindWallPrice(10, levels, tradecalc.SideOpenLong, 3, 20.0))

	// No wall satisfies the direction filter
	assert.Zero(t, tradecalc.FindWallPrice(15, levels, tradecalc.SideOpenLong, 3, 3.0))
	assert.Zero(t, tradecalc.FindWallPrice(8, levelsShort, tradecalc.SideOpenShort, 3, 3.0))
}

func TestExitPrices(t *testing.T) {
	t.Parallel()
	// CalculateStopLossPrice inputs zero
	assert.Zero(t, tradecalc.CalculateStopLossPrice(tradecalc.SideOpenLong, 0, 0.02, 0.1, 1))
	assert.Zero(t, tradecalc.CalculateStopLossPrice(tradecalc.SideOpenLong, 100, 0, 0.1, 1))
	assert.Zero(t, tradecalc.CalculateStopLossPrice(tradecalc.SideOpenLong, 100, 0.02, 0, 1))

	// CalculateStaticTakeProfitPrice inputs zero
	assert.Zero(t, tradecalc.CalculateStaticTakeProfitPrice(tradecalc.SideOpenLong, 0, 0.02, 0.1, 1))
	assert.Zero(t, tradecalc.CalculateStaticTakeProfitPrice(tradecalc.SideOpenLong, 100, 0, 0.1, 1))
	assert.Zero(t, tradecalc.CalculateStaticTakeProfitPrice(tradecalc.SideOpenLong, 100, 0.02, 0, 1))

	// LONG SL below entry, snapped ceil
	got1 := tradecalc.CalculateStopLossPrice(tradecalc.SideOpenLong, 100.0, 0.025, 0.2, 1)
	assert.InDelta(t, 97.6, got1, 1e-9)

	// SHORT SL above entry, snapped floor
	got2 := tradecalc.CalculateStopLossPrice(tradecalc.SideOpenShort, 100.0, 0.025, 0.2, 1)
	assert.InDelta(t, 102.4, got2, 1e-9)

	// LONG TP above entry, snapped floor
	got3 := tradecalc.CalculateStaticTakeProfitPrice(tradecalc.SideOpenLong, 100.0, 0.02, 0.1, 1)
	assert.InDelta(t, 102.0, got3, 1e-9)

	// SHORT TP below entry, snapped ceil
	got4 := tradecalc.CalculateStaticTakeProfitPrice(tradecalc.SideOpenShort, 100.0, 0.02, 0.1, 1)
	assert.InDelta(t, 98.0, got4, 1e-9)
}
