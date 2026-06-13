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
