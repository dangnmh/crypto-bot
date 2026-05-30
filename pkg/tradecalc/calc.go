package tradecalc

import (
	"errors"
	"math"

	"crypto-bot/pkg/decmath"
)

// Side represents the trade direction in a generic way.
type Side int

const (
	SideUnknown    Side = 0
	SideOpenLong   Side = 1
	SideCloseShort Side = 2
	SideOpenShort  Side = 3
	SideCloseLong  Side = 4
)

// OrderBookEntry represents a single price level in the order book.
type OrderBookEntry struct {
	Price  float64
	Volume float64
}

// Sentinel errors for trade calculations.
var (
	ErrInvalidSide      = errors.New("invalid side")
	ErrInvalidPriceUnit = errors.New("invalid price unit")
	ErrZeroRefPrice     = errors.New("reference price is zero")
	ErrZeroIOCPrice     = errors.New("calculated IOC price <= 0")
)

// CalculateIOCPrice calculates the IOC limit price based on side and market conditions.
// LONG: iocPrice = bestAsk + slippage  (buy at higher price, floor to tick)
// SHORT: iocPrice = bestBid - slippage  (sell at lower price, ceil to tick).
func CalculateIOCPrice(side Side, bestBid, bestAsk, maxPriceDiffPercent, priceUnit float64, priceScale int) (float64, error) {
	if priceUnit <= 0 {
		return 0, ErrInvalidPriceUnit
	}

	var refPrice float64
	var direction float64 // +1 for LONG (pay more), -1 for SHORT (accept less)
	var snapToTick func(float64, float64) float64

	switch side {
	case SideOpenLong:
		refPrice, direction = bestAsk, 1
		snapToTick = decmath.SnapToTickFloor // LONG → floor (don't overshoot)
	case SideOpenShort:
		refPrice, direction = bestBid, -1
		snapToTick = decmath.SnapToTickCeil // SHORT → ceil (don't undershoot)
	default:
		return 0, ErrInvalidSide
	}

	if refPrice <= 0 {
		return 0, ErrZeroRefPrice
	}

	slippage := CalculateSlippage(refPrice, maxPriceDiffPercent, priceUnit)

	// Use decimal math for tick-snapping to avoid float64 rounding errors.
	offsetPrice := decmath.Add(refPrice, decmath.Mul(direction, slippage))
	iocPrice := snapToTick(offsetPrice, priceUnit)
	iocPrice = decmath.RoundToScale(iocPrice, priceScale)

	// Sanity check: ensure IOC price is at least as aggressive as the reference price.
	if side == SideOpenLong && decmath.LessThan(iocPrice, refPrice) {
		iocPrice = decmath.RoundToScale(decmath.Add(refPrice, priceUnit), priceScale)
	} else if side == SideOpenShort && decmath.GreaterThan(iocPrice, refPrice) {
		iocPrice = decmath.RoundToScale(decmath.Sub(refPrice, priceUnit), priceScale)
	}

	if iocPrice <= 0 {
		return 0, ErrZeroIOCPrice
	}

	return iocPrice, nil
}

// CalculateSlippage computes price slippage tolerance.
func CalculateSlippage(refPrice, maxPriceDiffPercent, priceUnit float64) float64 {
	slippage := math.Max(decmath.Mul(refPrice, maxPriceDiffPercent/100.0), decmath.Mul(priceUnit, 2))
	return slippage
}

// CalculateVolume calculates the number of contracts to trade.
func CalculateVolume(margin, leverage, contractSize, refPrice, minVol float64, volScale int) float64 {
	if contractSize <= 0 || refPrice <= 0 {
		return 0
	}

	notional := decmath.Mul(margin, leverage)
	denom := decmath.Mul(contractSize, refPrice)
	vol := decmath.FloorToScale(decmath.Div(notional, denom), volScale)

	if vol < minVol {
		vol = minVol
	}

	return vol
}

// ExecutionRefPrice returns the side-appropriate reference price for sizing.
func ExecutionRefPrice(side Side, lastPrice, bestBid, bestAsk float64) float64 {
	refPrice := lastPrice
	if side == SideOpenLong && bestAsk > 0 {
		refPrice = bestAsk
	} else if side == SideOpenShort && bestBid > 0 {
		refPrice = bestBid
	}
	return refPrice
}

// CalculateTrapVolume calculates trap contracts from the dedicated trap sizing controls.
func CalculateTrapVolume(
	hasTrapSizing bool,
	candidateVolume float64,
	reversionNotional float64,
	sizeRatio float64,
	maxNotional float64,
	trapPrice float64,
	contractSize float64,
	minVol float64,
	volScale int,
) float64 {
	if !hasTrapSizing {
		return candidateVolume
	}

	notional := TrapTargetNotionalUSDT(reversionNotional, sizeRatio, maxNotional)
	if notional <= 0 {
		return 0
	}

	return CalculateVolumeForNotional(notional, trapPrice, contractSize, minVol, volScale)
}

// CalculateVolumeForNotional converts a USDT notional budget into exchange contract volume.
func CalculateVolumeForNotional(notional, refPrice, contractSize, minVol float64, volScale int) float64 {
	if contractSize <= 0 || refPrice <= 0 || notional <= 0 {
		return 0
	}

	denom := decmath.Mul(contractSize, refPrice)
	vol := decmath.FloorToScale(decmath.Div(notional, denom), volScale)
	if vol < minVol {
		vol = minVol
	}

	return vol
}

// ReversionNotionalUSDT returns the configured IOC notional budget.
func ReversionNotionalUSDT(margin float64, leverage int) float64 {
	return decmath.Mul(margin, float64(leverage))
}

// TrapTargetNotionalUSDT returns the configured trap notional budget after applying size ratio and cap.
func TrapTargetNotionalUSDT(reversionNotional, sizeRatio, maxNotional float64) float64 {
	ratio := sizeRatio
	if ratio <= 0 {
		ratio = 1
	}

	notional := decmath.Mul(reversionNotional, ratio)
	if maxNotional > 0 && notional > maxNotional {
		notional = maxNotional
	}

	return notional
}

// NotionalForVolume estimates USDT notional for an exchange contract volume at the reference price.
func NotionalForVolume(volume, refPrice, contractSize float64) float64 {
	if contractSize <= 0 || volume <= 0 || refPrice <= 0 {
		return 0
	}

	return decmath.Mul(decmath.Mul(volume, contractSize), refPrice)
}

// GetPeakPrice returns the reference extreme price right before firing IOC logic.
func GetPeakPrice(side Side, bestBid, bestAsk float64) float64 {
	if side == SideOpenLong {
		return bestAsk
	}
	return bestBid
}

// CalculateTrapPrice calculates the post-only Trap price, snapped to precision.
func CalculateTrapPrice(side Side, peakPrice, depthPct, priceUnit float64, priceScale int) float64 {
	var rawPrice float64
	var snapToTick func(float64, float64) float64

	switch side {
	case SideOpenShort:
		rawPrice = decmath.Mul(peakPrice, decmath.Sub(1, depthPct))
		snapToTick = decmath.SnapToTickFloor
	case SideOpenLong:
		rawPrice = decmath.Mul(peakPrice, decmath.Add(1, depthPct))
		snapToTick = decmath.SnapToTickCeil
	default:
		return 0
	}

	if priceUnit > 0 {
		rawPrice = snapToTick(rawPrice, priceUnit)
	}

	trapPrice := decmath.RoundToScale(rawPrice, priceScale)

	if trapPrice <= 0 {
		return 0
	}
	if side == SideOpenShort && trapPrice >= peakPrice {
		return 0
	}
	if side == SideOpenLong && trapPrice <= peakPrice {
		return 0
	}

	return trapPrice
}

// FindWallPrice scans OB levels and returns the price of the first liquidity wall.
func FindWallPrice(entryPrice float64, levels []OrderBookEntry, side Side, minWallLevels int, wallMultiplier float64) float64 {
	if len(levels) < minWallLevels {
		return 0
	}

	var totalVol float64
	for _, l := range levels {
		totalVol += l.Volume
	}
	avgVol := totalVol / float64(len(levels))
	threshold := avgVol * wallMultiplier

	if entryPrice <= 0 {
		return 0
	}

	for _, l := range levels {
		if l.Volume < threshold {
			continue
		}

		if side == SideOpenLong && l.Price > entryPrice {
			return l.Price
		}
		if side == SideOpenShort && l.Price < entryPrice {
			return l.Price
		}
	}

	return 0
}

// CalculateOBTrapPrice computes the actual trap limit price, stepping 1 tick in front of the wall.
func CalculateOBTrapPrice(trapSide Side, wallPrice, priceUnit float64) float64 {
	if trapSide == SideOpenLong {
		return decmath.Add(wallPrice, priceUnit)
	}
	return decmath.Sub(wallPrice, priceUnit)
}

// TrapWallDistancePct returns the wall distance from the current candidate context in percent.
func TrapWallDistancePct(wallPrice, refPrice float64) float64 {
	if wallPrice <= 0 || refPrice <= 0 {
		return 0
	}
	return decmath.Mul(decmath.Div(math.Abs(decmath.Sub(wallPrice, refPrice)), refPrice), 100.0)
}

// CalculateStaticExitPrice calculates static reversion/exit price.
func CalculateStaticExitPrice(side Side, entryPrice, pct float64, favorable bool, priceUnit float64, priceScale int) float64 {
	longUp := side == SideOpenLong && favorable
	shortStop := side == SideOpenShort && !favorable
	if longUp || shortStop {
		price := decmath.Mul(entryPrice, decmath.Add(1, pct))
		return decmath.RoundToScale(decmath.SnapToTickFloor(price, priceUnit), priceScale)
	}

	price := decmath.Mul(entryPrice, decmath.Sub(1, pct))
	return decmath.RoundToScale(decmath.SnapToTickCeil(price, priceUnit), priceScale)
}

// CalculateStopLossPrice computes a server-side SL price relative to entryPrice.
func CalculateStopLossPrice(side Side, entryPrice, slPct, priceUnit float64, priceScale int) float64 {
	if slPct <= 0 || entryPrice <= 0 || priceUnit <= 0 {
		return 0
	}
	return CalculateStaticExitPrice(side, entryPrice, slPct, false, priceUnit, priceScale)
}

// CalculateStaticTakeProfitPrice computes the static server-side TP.
func CalculateStaticTakeProfitPrice(side Side, entryPrice, tpPct, priceUnit float64, priceScale int) float64 {
	if tpPct <= 0 || entryPrice <= 0 || priceUnit <= 0 {
		return 0
	}
	return CalculateStaticExitPrice(side, entryPrice, tpPct, true, priceUnit, priceScale)
}

// CalculateTrapTPPrice computes a server-side Take Profit price for a trap order.
func CalculateTrapTPPrice(side Side, trapPrice, tpPct, priceUnit float64, priceScale int) float64 {
	if tpPct <= 0 || trapPrice <= 0 || priceUnit <= 0 {
		return 0
	}

	var tp float64
	if side == SideOpenLong {
		tp = decmath.Mul(trapPrice, decmath.Add(1, tpPct))
		tp = decmath.SnapToTickFloor(tp, priceUnit)
	} else {
		tp = decmath.Mul(trapPrice, decmath.Sub(1, tpPct))
		tp = decmath.SnapToTickCeil(tp, priceUnit)
	}
	return decmath.RoundToScale(tp, priceScale)
}

// CalculateTrapSLPrice computes a server-side Stop Loss price for a trap order.
func CalculateTrapSLPrice(side Side, trapPrice, slPct, priceUnit float64, priceScale int) float64 {
	if slPct <= 0 || trapPrice <= 0 || priceUnit <= 0 {
		return 0
	}

	var sl float64
	if side == SideOpenLong {
		sl = decmath.Mul(trapPrice, decmath.Sub(1, slPct))
		sl = decmath.SnapToTickCeil(sl, priceUnit)
	} else {
		sl = decmath.Mul(trapPrice, decmath.Add(1, slPct))
		sl = decmath.SnapToTickFloor(sl, priceUnit)
	}
	return decmath.RoundToScale(sl, priceScale)
}
