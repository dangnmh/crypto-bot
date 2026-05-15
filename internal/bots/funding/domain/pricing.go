package domain

import (
	"fmt"
	"math"

	shared "crypto-bot/internal/domain"
	"crypto-bot/pkg/decmath"
)

// CalculateIOCPrice calculates the IOC limit price based on side and market conditions.
//
// LONG: iocPrice = bestAsk + slippage  (buy at higher price, floor to tick)
// SHORT: iocPrice = bestBid - slippage  (sell at lower price, ceil to tick)
//
// Prices are rounded to the nearest valid tick (priceUnit) then snapped to PriceScale decimals.
func (c *Candidate) CalculateIOCPrice(ob *shared.OrderBook) (float64, error) {
	if c.PriceUnit <= 0 {
		return 0, fmt.Errorf("%w: %f", ErrInvalidPriceUnit, c.PriceUnit)
	}

	var refPrice float64
	var direction float64 // +1 for LONG (pay more), -1 for SHORT (accept less)
	var snapToTick func(float64, float64) float64

	switch c.Side {
	case shared.SideOpenLong:
		refPrice, direction = c.BestAsk, 1
		snapToTick = decmath.SnapToTickFloor // LONG → floor (don't overshoot)
	case shared.SideOpenShort:
		refPrice, direction = c.BestBid, -1
		snapToTick = decmath.SnapToTickCeil // SHORT → ceil (don't undershoot)
	default:
		return 0, fmt.Errorf("%w: %d", ErrInvalidSide, c.Side)
	}

	if refPrice <= 0 {
		return 0, fmt.Errorf("%w: %f", ErrZeroRefPrice, refPrice)
	}

	slippage := c.calculateSlippage(ob, refPrice)

	// Use decimal math for tick-snapping to avoid float64 rounding errors.
	offsetPrice := decmath.Add(refPrice, decmath.Mul(direction, slippage))
	iocPrice := snapToTick(offsetPrice, c.PriceUnit)
	iocPrice = decmath.RoundToScale(iocPrice, c.PriceScale)

	// Sanity check: ensure IOC price is at least as aggressive as the reference price.
	// After Floor rounding (LONG) or Ceil rounding (SHORT), the price may have
	// snapped back below/above the current best price, causing guaranteed No-Fill.
	if c.Side == shared.SideOpenLong && decmath.LessThan(iocPrice, refPrice) {
		iocPrice = decmath.RoundToScale(decmath.Add(refPrice, c.PriceUnit), c.PriceScale)
	} else if c.Side == shared.SideOpenShort && decmath.GreaterThan(iocPrice, refPrice) {
		iocPrice = decmath.RoundToScale(decmath.Sub(refPrice, c.PriceUnit), c.PriceScale)
	}

	if iocPrice <= 0 {
		return 0, ErrZeroIOCPrice
	}

	return iocPrice, nil
}

// wallMultiplier defines the threshold for detecting a liquidity wall.
// A price level with volume >= wallMultiplier × average volume is a wall.
const wallMultiplier = 3.0

// minWallLevels is the minimum number of OB levels needed for wall detection.
const minWallLevels = 3

func (c *Candidate) calculateSlippage(ob *shared.OrderBook, refPrice float64) float64 {
	calc := newSlippageCalculator(c, c.Config.FundingReversion.DynamicPricing)
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
	if c.Side == shared.SideOpenLong && c.BestAsk > 0 {
		refPrice = c.BestAsk
	} else if c.Side == shared.SideOpenShort && c.BestBid > 0 {
		refPrice = c.BestBid
	}

	notional := decmath.Mul(c.Config.MarginUSDT, float64(c.Config.Leverage))
	denom := decmath.Mul(c.ContractSize, refPrice)
	vol := decmath.FloorToScale(decmath.Div(notional, denom), c.VolScale)

	if vol < float64(c.MinVol) {
		vol = float64(c.MinVol)
	}

	return vol
}

// CalculateTrapVolume calculates trap contracts from the dedicated trap sizing
// controls. If no trap sizing is configured, it preserves the candidate volume
// for tests and legacy direct domain callers; loaded configs default sizeRatio.
func (c *Candidate) CalculateTrapVolume(trapPrice float64) float64 {
	if c.Config.FundingTrap.SizeRatio <= 0 && c.Config.FundingTrap.MaxNotionalUSDT <= 0 {
		return c.Volume
	}

	notional := c.TrapTargetNotionalUSDT()
	if notional <= 0 {
		return 0
	}

	return c.CalculateVolumeForNotional(notional, trapPrice)
}

// CalculateVolumeForNotional converts a USDT notional budget into exchange
// contract volume using the provided reference price.
func (c *Candidate) CalculateVolumeForNotional(notional, refPrice float64) float64 {
	if c.ContractSize <= 0 || refPrice <= 0 || notional <= 0 {
		return 0
	}

	denom := decmath.Mul(c.ContractSize, refPrice)
	vol := decmath.FloorToScale(decmath.Div(notional, denom), c.VolScale)
	if vol < float64(c.MinVol) {
		vol = float64(c.MinVol)
	}

	return vol
}

// ReversionNotionalUSDT returns the configured IOC notional budget.
func (c *Candidate) ReversionNotionalUSDT() float64 {
	return decmath.Mul(c.Config.MarginUSDT, float64(c.Config.Leverage))
}

// TrapTargetNotionalUSDT returns the configured trap notional budget after
// applying the trap size ratio and optional absolute cap.
func (c *Candidate) TrapTargetNotionalUSDT() float64 {
	base := c.ReversionNotionalUSDT()
	ratio := c.Config.FundingTrap.SizeRatio
	if ratio <= 0 {
		ratio = 1
	}

	notional := decmath.Mul(base, ratio)
	if maxNotional := c.Config.FundingTrap.MaxNotionalUSDT; maxNotional > 0 && notional > maxNotional {
		notional = maxNotional
	}

	return notional
}

// NotionalForVolume estimates USDT notional for an exchange contract volume at
// the provided reference price.
func (c *Candidate) NotionalForVolume(volume, refPrice float64) float64 {
	if c.ContractSize <= 0 || volume <= 0 || refPrice <= 0 {
		return 0
	}

	return decmath.Mul(decmath.Mul(volume, c.ContractSize), refPrice)
}

// GetPeakPrice returns the reference extreme price right before firing IOC logic.
func (c *Candidate) GetPeakPrice() float64 {
	if c.Side == shared.SideOpenLong {
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
	var snapToTick func(float64, float64) float64

	// Translate Sniper Side directly mathematically without an intermediate TrapSide variable
	switch c.Side {
	case shared.SideOpenShort:
		// Sniper SHORT (Entered Short). Crowd is Long. At T=0, crowd sells.
		// Market DUMPS (reverts downward).
		// Trap must be LONG placed BELOW the PeakPrice.
		rawPrice = decmath.Mul(c.GetPeakPrice(), decmath.Sub(1, c.Config.FundingTrap.DepthPct))
		snapToTick = decmath.SnapToTickFloor
	case shared.SideOpenLong:
		// Sniper LONG (Entered Long). Crowd is Short. At T=0, crowd buys.
		// Market PUMPS (reverts upward).
		// Trap must be SHORT placed ABOVE the PeakPrice.
		rawPrice = decmath.Mul(c.GetPeakPrice(), decmath.Add(1, c.Config.FundingTrap.DepthPct))
		snapToTick = decmath.SnapToTickCeil
	default:
		// Invalid Side
		return 0
	}

	if c.PriceUnit > 0 {
		rawPrice = snapToTick(rawPrice, c.PriceUnit)
	}

	trapPrice := decmath.RoundToScale(rawPrice, c.PriceScale)

	// Sanity check: trap price must be positive and on the correct side of peak
	if trapPrice <= 0 {
		return 0
	}
	peak := c.GetPeakPrice()
	if c.Side == shared.SideOpenShort && trapPrice >= peak {
		// Trap LONG should be BELOW peak, not above
		return 0
	}
	if c.Side == shared.SideOpenLong && trapPrice <= peak {
		// Trap SHORT should be ABOVE peak, not below
		return 0
	}

	return trapPrice
}

// PrepareDynamicPricing calculates and overwrites TP/SL, Trap, and Trailing params
// based on the live Funding Rate and ATR. Should be called after ATR is set.
func (c *Candidate) PrepareDynamicPricing() {
	if !c.Config.FundingReversion.DynamicPricing.Enabled {
		return
	}

	// Ticker's FundingRate is usually decimal (0.001 = 0.1%). So frPct = c.FundingRate * 100.
	frPct := math.Abs(c.FundingRate * 100.0)

	atrPct := 0.0
	if c.LastPrice > 0 && c.ATR > 0 {
		atrPct = (c.ATR / c.LastPrice) * 100.0
	}

	dp := c.Config.FundingReversion.DynamicPricing

	// ── TP/SL ──
	// Values are computed in percentage (e.g. 0.6 = 0.6%), then converted to ratio (÷100)
	// to match the unit convention of TakeProfitPct/StopLossPct fields.
	if c.ATR > 0 {
		tpPct := (frPct * dp.TpFundingMultiplier) + (atrPct * dp.TpAtrMultiplier)
		slPct := math.Max(frPct*dp.SlFundingMultiplier, atrPct*dp.SlAtrMultiplier)

		if tpPct > 0 {
			c.Config.FundingReversion.TakeProfitPct = tpPct / 100.0
		}
		if slPct > 0 {
			c.Config.FundingReversion.StopLossPct = slPct / 100.0
		}
	}

	// ── FR-Dynamic Trap Parameters ──
	trap := c.Config.FundingTrap
	if c.Config.IsHedgeTrapEnabled() && trap.DepthMultiplier > 0 {
		c.Config.FundingTrap.DepthPct = clampPct(frPct*trap.DepthMultiplier, trap.MinDepth, trap.MaxDepth)
		c.Config.FundingTrap.TakeProfitPct = clampPct(frPct*trap.TpMultiplier, trap.MinTP, trap.MaxTP)
		c.Config.FundingTrap.StopLossPct = clampPct(frPct*trap.SlMultiplier, trap.MinSL, trap.MaxSL)
	}

	// ── FR-Dynamic Trailing Parameters ──
	trail := c.Config.FundingReversion.Trailing
	if trail.Enabled && trail.ActivationMultiplier > 0 {
		c.Config.FundingReversion.Trailing.ActivationPct = clampPct(frPct*trail.ActivationMultiplier, trail.MinActivation, trail.MaxActivation)
		c.Config.FundingReversion.Trailing.CallbackPct = clampPct(frPct*trail.CallbackMultiplier, trail.MinCallback, trail.MaxCallback)
	}
}

// clampPct clamps a percentage value between lo and hi, then converts to ratio (÷100).
func clampPct(v, lo, hi float64) float64 {
	if v < lo {
		v = lo
	}
	if v > hi {
		v = hi
	}
	return v / 100.0
}
