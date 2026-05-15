package domain

import (
	"math"

	shared "crypto-bot/internal/domain"
	"crypto-bot/pkg/decmath"
)

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

// TrapWallDistancePct returns the wall distance from the current candidate
// price context in percent units.
func (c *Candidate) TrapWallDistancePct(wallPrice float64) float64 {
	if wallPrice <= 0 {
		return 0
	}
	ref := c.LastPrice
	if ref <= 0 {
		ref = c.entryRefPrice()
	}
	if ref <= 0 {
		return 0
	}
	return decmath.Mul(decmath.Div(math.Abs(decmath.Sub(wallPrice, ref)), ref), 100.0)
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
