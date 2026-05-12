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

// CalculateTakeProfitPrice computes a dynamic TP based on orderbook wall detection.
//
// Algorithm (from depth.md §3):
//   - LONG: scan Ask side (price will pump through asks) → find wall → TP just before wall
//   - SHORT: scan Bid side (price will dump through bids) → find wall → TP just before wall
//   - If no wall found or OB is nil/empty → fallback to maxTPPct from entry price
//   - Result is always clamped to maxTPPct safety rail
//
// Returns 0 if TP cannot be calculated (e.g., invalid inputs).
func (c *Candidate) CalculateTakeProfitPrice(ob *shared.OrderBook, maxTPPct float64) float64 {
	if maxTPPct <= 0 || c.PriceUnit <= 0 {
		return 0
	}

	entryPrice := c.entryRefPrice()
	if entryPrice <= 0 {
		return 0
	}

	// Determine which side of the OB to scan and the price boundary.
	var levels []shared.OrderBookEntry
	var maxTP float64 // absolute price limit

	switch c.Side {
	case shared.SideOpenLong:
		// Price pumps UP → scan asks for walls blocking upside.
		maxTP = decmath.Mul(entryPrice, decmath.Add(1, maxTPPct/100.0))
		if ob != nil {
			levels = ob.Asks
		}
	case shared.SideOpenShort:
		// Price dumps DOWN → scan bids for walls blocking downside.
		maxTP = decmath.Mul(entryPrice, decmath.Sub(1, maxTPPct/100.0))
		if ob != nil {
			levels = ob.Bids
		}
	default:
		return 0
	}

	wallPrice := c.FindWallPrice(levels, c.Side)

	var rawTP float64
	if wallPrice > 0 {
		rawTP = c.snapTPBeforeWall(wallPrice, c.Side)
		// Clamp: don't exceed maxTP safety rail.
		rawTP = c.clampTP(rawTP, maxTP, c.Side)
	} else {
		// No wall found → use maxTP directly.
		rawTP = maxTP
	}

	// Tick-snap: LONG TP is above entry (floor to avoid overshoot),
	// SHORT TP is below entry (ceil to avoid undershoot).
	rawTP = c.snapTPToTick(rawTP)
	rawTP = decmath.RoundToScale(rawTP, c.PriceScale)

	if rawTP <= 0 {
		return 0
	}

	// Final sanity: TP must be on the correct side of entry price.
	if c.Side == shared.SideOpenLong && rawTP <= entryPrice {
		return 0
	}
	if c.Side == shared.SideOpenShort && rawTP >= entryPrice {
		return 0
	}

	return rawTP
}

// entryRefPrice returns the reference price used as the "entry" for TP calculation.
// LONG: BestAsk (we buy at ask), SHORT: BestBid (we sell at bid).
func (c *Candidate) entryRefPrice() float64 {
	if c.Side == shared.SideOpenLong {
		return c.BestAsk
	}
	return c.BestBid
}

// FindWallPrice scans OB levels and returns the price of the first liquidity wall.
// A wall is a level with volume >= wallMultiplier × average volume of scanned levels.
// Returns 0 if no wall is found.
func (c *Candidate) FindWallPrice(levels []shared.OrderBookEntry, side shared.Side) float64 {
	if len(levels) < minWallLevels {
		return 0
	}

	// Compute average volume across all levels.
	var totalVol float64
	for _, l := range levels {
		totalVol += l.Volume
	}
	avgVol := totalVol / float64(len(levels))
	threshold := avgVol * wallMultiplier

	entry := c.entryRefPrice()
	if entry <= 0 {
		return 0
	}

	for _, l := range levels {
		if l.Volume < threshold {
			continue
		}

		// Wall must be in the direction price is traveling.
		if side == shared.SideOpenLong && l.Price > entry {
			return l.Price
		}
		if side == shared.SideOpenShort && l.Price < entry {
			return l.Price
		}
	}

	return 0
}

// FindTrapWallPrice identifies the correct wall price for placing a Trap order
// based on the original IOC entry side.
func (c *Candidate) FindTrapWallPrice(ob *shared.OrderBook) float64 {
	if c.Side == shared.SideOpenLong {
		// IOC was LONG. Trap will be SHORT. Look for ASK wall.
		return c.FindWallPrice(ob.Asks, c.Side)
	}
	// IOC was SHORT. Trap will be LONG. Look for BID wall.
	return c.FindWallPrice(ob.Bids, c.Side)
}

// CalculateOBTrapPrice computes the actual trap limit price, stepping 1 tick in front of the wall.
func (c *Candidate) CalculateOBTrapPrice(wallPrice float64) float64 {
	trapSide := c.Side.Opposite()
	if trapSide == shared.SideOpenLong {
		// Trap LONG: buy slightly higher than the wall.
		return decmath.Add(wallPrice, c.PriceUnit)
	}
	// Trap SHORT: sell slightly lower than the wall.
	return decmath.Sub(wallPrice, c.PriceUnit)
}

// snapTPBeforeWall places TP 2 ticks before the wall so the order fills
// before price hits the wall and bounces.
func (c *Candidate) snapTPBeforeWall(wallPrice float64, side shared.Side) float64 {
	offset := decmath.Mul(c.PriceUnit, 2) // 2 ticks before wall
	if side == shared.SideOpenLong {
		// Wall is above entry → TP below the wall.
		return decmath.Sub(wallPrice, offset)
	}
	// Wall is below entry → TP above the wall.
	return decmath.Add(wallPrice, offset)
}

// clampTP ensures TP does not exceed the maxTP safety rail.
func (c *Candidate) clampTP(tp, maxTP float64, side shared.Side) float64 {
	if side == shared.SideOpenLong {
		// LONG: TP is above entry. maxTP is the ceiling.
		if tp > maxTP {
			return maxTP
		}
	} else {
		// SHORT: TP is below entry. maxTP is the floor.
		if tp < maxTP {
			return maxTP
		}
	}
	return tp
}

// snapTPToTick aligns TP to the nearest valid tick in the conservative direction.
// LONG TP (above entry) → floor; SHORT TP (below entry) → ceil.
func (c *Candidate) snapTPToTick(tp float64) float64 {
	if c.Side == shared.SideOpenLong {
		return decmath.SnapToTickFloor(tp, c.PriceUnit)
	}
	return decmath.SnapToTickCeil(tp, c.PriceUnit)
}

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

// CalculateStopLossPrice computes a server-side SL price relative to entryPrice.
// LONG: SL below entry (ceil to avoid triggering too early).
// SHORT: SL above entry (floor to avoid triggering too early).
// Returns 0 if StopLossPct is not configured or entryPrice is invalid.
func (c *Candidate) CalculateStopLossPrice(entryPrice float64) float64 {
	if c.Config.FundingReversion.StopLossPct <= 0 || entryPrice <= 0 || c.PriceUnit <= 0 {
		return 0
	}

	var sl float64
	if c.Side == shared.SideOpenLong {
		sl = decmath.Mul(entryPrice, decmath.Sub(1, c.Config.FundingReversion.StopLossPct))
		sl = decmath.SnapToTickCeil(sl, c.PriceUnit)
	} else {
		sl = decmath.Mul(entryPrice, decmath.Add(1, c.Config.FundingReversion.StopLossPct))
		sl = decmath.SnapToTickFloor(sl, c.PriceUnit)
	}
	return decmath.RoundToScale(sl, c.PriceScale)
}

// CalculateTrapTPPrice computes a server-side Take Profit price for a trap order.
// trapPrice is the limit entry price of the trap.
// LONG trap: TP above trapPrice (floor). SHORT trap: TP below trapPrice (ceil).
// Returns 0 if Trap TakeProfitPct is not configured.
func (c *Candidate) CalculateTrapTPPrice(trapPrice float64) float64 {
	tpPct := c.Config.FundingTrap.TakeProfitPct
	if tpPct <= 0 || trapPrice <= 0 || c.PriceUnit <= 0 {
		return 0
	}

	var tp float64
	if c.Side == shared.SideOpenLong {
		tp = decmath.Mul(trapPrice, decmath.Add(1, tpPct))
		tp = decmath.SnapToTickFloor(tp, c.PriceUnit)
	} else {
		tp = decmath.Mul(trapPrice, decmath.Sub(1, tpPct))
		tp = decmath.SnapToTickCeil(tp, c.PriceUnit)
	}
	return decmath.RoundToScale(tp, c.PriceScale)
}

// CalculateTrapSLPrice computes a server-side Stop Loss price for a trap order.
// trapPrice is the limit entry price of the trap.
// LONG trap: SL below trapPrice (ceil). SHORT trap: SL above trapPrice (floor).
// Returns 0 if Trap StopLossPct is not configured.
func (c *Candidate) CalculateTrapSLPrice(trapPrice float64) float64 {
	slPct := c.Config.FundingTrap.StopLossPct
	if slPct <= 0 || trapPrice <= 0 || c.PriceUnit <= 0 {
		return 0
	}

	var sl float64
	if c.Side == shared.SideOpenLong {
		sl = decmath.Mul(trapPrice, decmath.Sub(1, slPct))
		sl = decmath.SnapToTickCeil(sl, c.PriceUnit)
	} else {
		sl = decmath.Mul(trapPrice, decmath.Add(1, slPct))
		sl = decmath.SnapToTickFloor(sl, c.PriceUnit)
	}
	return decmath.RoundToScale(sl, c.PriceScale)
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

	// ── TP/SL (existing logic) ──
	if c.ATR > 0 {
		tp := (frPct * dp.TpFundingMultiplier) + (atrPct * dp.TpAtrMultiplier)
		sl := math.Max(frPct*dp.SlFundingMultiplier, atrPct*dp.SlAtrMultiplier)

		if tp > 0 {
			c.Config.FundingReversion.TakeProfitPct = tp
		}
		if sl > 0 {
			c.Config.FundingReversion.StopLossPct = sl
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
