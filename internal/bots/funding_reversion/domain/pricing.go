package domain

import (
	"fmt"
	"math"

	"crypto-bot/internal/infrastructure/exchange"
)

// CalculateIOCPrice calculates the IOC limit price based on side and market conditions.
//
// LONG: iocPrice = bestAsk + slippage  (buy at higher price, floor to tick)
// SHORT: iocPrice = bestBid - slippage  (sell at lower price, ceil to tick)
//
// Prices are rounded to the nearest valid tick (priceUnit) then snapped to PriceScale decimals.
func (c *Candidate) CalculateIOCPrice(ob *exchange.OrderBook) (float64, error) {
	if c.PriceUnit <= 0 {
		return 0, fmt.Errorf("%w: %f", ErrInvalidPriceUnit, c.PriceUnit)
	}

	var refPrice float64
	var direction float64 // +1 for LONG (pay more), -1 for SHORT (accept less)
	var snap func(float64) float64

	switch c.Side {
	case exchange.SideOpenLong:
		refPrice, direction = c.BestAsk, 1
		snap = math.Floor // LONG → floor (don't overshoot)
	case exchange.SideOpenShort:
		refPrice, direction = c.BestBid, -1
		snap = math.Ceil // SHORT → ceil (don't undershoot)
	default:
		return 0, fmt.Errorf("%w: %d", ErrInvalidSide, c.Side)
	}

	if refPrice <= 0 {
		return 0, fmt.Errorf("%w: %f", ErrZeroRefPrice, refPrice)
	}

	slippage := c.calculateSlippage(ob, refPrice)

	iocPrice := snap((refPrice+direction*slippage)/c.PriceUnit) * c.PriceUnit
	iocPrice = roundToScale(iocPrice, c.PriceScale)

	// Sanity check: ensure IOC price is at least as aggressive as the reference price.
	// After Floor rounding (LONG) or Ceil rounding (SHORT), the price may have
	// snapped back below/above the current best price, causing guaranteed No-Fill.
	if c.Side == exchange.SideOpenLong && iocPrice < refPrice {
		iocPrice = roundToScale(refPrice+c.PriceUnit, c.PriceScale)
	} else if c.Side == exchange.SideOpenShort && iocPrice > refPrice {
		iocPrice = roundToScale(refPrice-c.PriceUnit, c.PriceScale)
	}

	if iocPrice <= 0 {
		return 0, ErrZeroIOCPrice
	}

	return iocPrice, nil
}

func (c *Candidate) calculateSlippage(ob *exchange.OrderBook, refPrice float64) float64 {
	calc := newSlippageCalculator(c, c.Config.DynamicPricing)
	slippage := calc.Calculate(refPrice, ob)

	// Ensure minimum tick slippage regardless of strategy
	if slippage < c.PriceUnit*2 {
		slippage = c.PriceUnit * 2
	}
	return slippage
}

// CalculateVolume calculates the number of contracts to trade based on configuration.
// It uses the side-appropriate reference price (BestAsk for LONG, BestBid for SHORT)
// to avoid margin overcommit when LastPrice lags behind the orderbook.
// Falls back to LastPrice if BestBid/BestAsk is unavailable.
func (c *Candidate) CalculateVolume() float64 {
	if c.ContractSize <= 0 || c.LastPrice <= 0 {
		return 0
	}

	// Use the price we'll actually pay, not the stale LastPrice
	refPrice := c.LastPrice // fallback
	if c.Side == exchange.SideOpenLong && c.BestAsk > 0 {
		refPrice = c.BestAsk
	} else if c.Side == exchange.SideOpenShort && c.BestBid > 0 {
		refPrice = c.BestBid
	}

	vol := (c.Config.MarginUSDT * float64(c.Config.Leverage)) / (c.ContractSize * refPrice)
	vol = floorToScale(vol, c.VolScale)

	if vol < float64(c.MinVol) {
		vol = float64(c.MinVol)
	}

	return vol
}

// GetPeakPrice returns the reference extreme price right before firing IOC logic.
func (c *Candidate) GetPeakPrice() float64 {
	if c.Side == exchange.SideOpenLong {
		// Shooter LONG -> Reversion Trap SHORT (Sell High) -> need bestAsk peak
		return c.BestAsk
	}
	// Shooter SHORT -> Reversion Trap LONG (Buy Low) -> need bestBid trough
	return c.BestBid
}

// CalculateTrapPrice calculates the post-only Trap price, snapped to precision.
// IMPORTANT: side MUST represent the original IOC Sniper entry direction (LONG or SHORT).
func (c *Candidate) CalculateTrapPrice() float64 {
	var rawPrice float64
	var snap func(float64) float64

	// Translate Sniper Side directly mathematically without an intermediate TrapSide variable
	switch c.Side {
	case exchange.SideOpenShort:
		// Sniper SHORT (Entered Short). Crowd is Long. At T=0, crowd sells.
		// Market DUMPS (reverts downward).
		// Trap must be LONG placed BELOW the PeakPrice.
		rawPrice = c.GetPeakPrice() * (1 - c.Config.TrapDepthPct)
		snap = math.Floor
	case exchange.SideOpenLong:
		// Sniper LONG (Entered Long). Crowd is Short. At T=0, crowd buys.
		// Market PUMPS (reverts upward).
		// Trap must be SHORT placed ABOVE the PeakPrice.
		rawPrice = c.GetPeakPrice() * (1 + c.Config.TrapDepthPct)
		snap = math.Ceil
	default:
		// Invalid Side
		return 0
	}

	if c.PriceUnit > 0 {
		rawPrice = snap(rawPrice/c.PriceUnit) * c.PriceUnit
	}

	trapPrice := roundToScale(rawPrice, c.PriceScale)

	// Sanity check: trap price must be positive and on the correct side of peak
	if trapPrice <= 0 {
		return 0
	}
	peak := c.GetPeakPrice()
	if c.Side == exchange.SideOpenShort && trapPrice >= peak {
		// Trap LONG should be BELOW peak, not above
		return 0
	}
	if c.Side == exchange.SideOpenLong && trapPrice <= peak {
		// Trap SHORT should be ABOVE peak, not below
		return 0
	}

	return trapPrice
}

// PrepareDynamicPricing calculates and overwrites TP/SL, Trap, and Trailing params
// based on the live Funding Rate and ATR. Should be called after ATR is set.
func (c *Candidate) PrepareDynamicPricing() {
	if !c.Config.DynamicPricing.Enabled {
		return
	}

	// Ticker's FundingRate is usually decimal (0.001 = 0.1%). So frPct = c.FundingRate * 100.
	frPct := math.Abs(c.FundingRate * 100.0)

	atrPct := 0.0
	if c.LastPrice > 0 && c.ATR > 0 {
		atrPct = (c.ATR / c.LastPrice) * 100.0
	}

	dp := c.Config.DynamicPricing

	// ── TP/SL (existing logic) ──
	if c.ATR > 0 {
		tp := (frPct * dp.TpFundingMultiplier) + (atrPct * dp.TpAtrMultiplier)
		sl := math.Max(frPct*dp.SlFundingMultiplier, atrPct*dp.SlAtrMultiplier)

		if tp > 0 {
			c.Config.TakeProfitPct = tp
		}
		if sl > 0 {
			c.Config.StopLossPct = sl
		}
	}

	// ── FR-Dynamic Trap Parameters ──
	if c.Config.IsHedgeTrapEnabled() && dp.TrapDepthMultiplier > 0 {
		c.Config.TrapDepthPct = clampPct(frPct*dp.TrapDepthMultiplier, dp.MinTrapDepth, dp.MaxTrapDepth)
		c.Config.TrapTakeProfitPct = clampPct(frPct*dp.TrapTpMultiplier, dp.MinTrapTP, dp.MaxTrapTP)
		c.Config.TrapStopLossPct = clampPct(frPct*dp.TrapSlMultiplier, dp.MinTrapSL, dp.MaxTrapSL)
	}

	// ── FR-Dynamic Trailing Parameters ──
	if c.Config.TrailingConfig.Enabled && dp.TrailingActivationMultiplier > 0 {
		c.Config.TrailingConfig.ActivationPct = clampPct(frPct*dp.TrailingActivationMultiplier, dp.MinActivation, dp.MaxActivation)
		c.Config.TrailingConfig.CallbackPct = clampPct(frPct*dp.TrailingCallbackMultiplier, dp.MinCallback, dp.MaxCallback)
	}
}

// clampPct clamps a percentage value between min and max, then converts to ratio (÷100).
func clampPct(v, min, max float64) float64 {
	if v < min {
		v = min
	}
	if v > max {
		v = max
	}
	return v / 100.0
}

// roundToScale rounds v to n decimal places (nearest).
func roundToScale(v float64, n int) float64 {
	p := math.Pow10(n)
	return math.Round(v*p) / p
}

// floorToScale truncates v down to n decimal places.
func floorToScale(v float64, n int) float64 {
	p := math.Pow10(n)
	return math.Floor(v*p) / p
}
