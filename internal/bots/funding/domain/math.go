package domain

import (
	shared "crypto-bot/internal/domain"
	"math"
)

// CalculateATR computes the Average True Range for a given slice of klines.
// Uses Wilder's Smoothing Method (RMA).
func CalculateATR(klines []shared.Kline, period int) float64 {
	if len(klines) <= 1 || period <= 0 {
		return 0
	}

	// We need at least 'period' + 1 candles to get a meaningful ATR.
	// If we have fewer, we'll just calculate it over whatever we have.
	n := len(klines)
	if period > n-1 {
		period = n - 1
	}

	var trs []float64
	for i := 1; i < n; i++ {
		curr := klines[i]
		prev := klines[i-1]

		highLow := curr.High - curr.Low
		highPrevClose := math.Abs(curr.High - prev.Close)
		lowPrevClose := math.Abs(curr.Low - prev.Close)

		tr := math.Max(highLow, math.Max(highPrevClose, lowPrevClose))
		trs = append(trs, tr)
	}

	// First ATR is the SMA of the first 'period' TRs
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += trs[i]
	}
	atr := sum / float64(period)

	// Subsequent ATRs use Wilder's Smoothing: ATR = (PrevATR * (period - 1) + TR) / period
	for i := period; i < len(trs); i++ {
		atr = (atr*float64(period-1) + trs[i]) / float64(period)
	}

	return atr
}

// ATRPercent converts absolute ATR value to a percentage of the last close price.
func ATRPercent(klines []shared.Kline, period int) float64 {
	atr := CalculateATR(klines, period)
	if len(klines) == 0 || atr == 0 {
		return 0
	}
	lastClose := klines[len(klines)-1].Close
	if lastClose == 0 {
		return 0
	}
	return (atr / lastClose) * 100.0 // Returns % (e.g. 1.5 for 1.5%)
}
