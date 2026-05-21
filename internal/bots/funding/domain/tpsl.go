package domain

import (
	"math"

	shared "crypto-bot/internal/domain"
	"crypto-bot/pkg/decmath"
)

// CalculateStaticTakeProfitPrice computes the Reversion server-side TP from
// the configured static TakeProfitPct only. It intentionally ignores orderbook
// walls because settlement liquidity can disappear before the TP is reached.
func (c *Candidate) CalculateStaticTakeProfitPrice(entryPrice float64) float64 {
	if c.Config.FundingReversion.TakeProfitPct <= 0 || entryPrice <= 0 || c.PriceUnit <= 0 {
		return 0
	}
	return c.calculateStaticReversionExitPrice(entryPrice, c.Config.FundingReversion.TakeProfitPct, true)
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

// CalculateStopLossPrice computes a server-side SL price relative to entryPrice.
// LONG: SL below entry (ceil to avoid triggering too early).
// SHORT: SL above entry (floor to avoid triggering too early).
// Returns 0 if StopLossPct is not configured or entryPrice is invalid.
func (c *Candidate) CalculateStopLossPrice(entryPrice float64) float64 {
	if c.Config.FundingReversion.StopLossPct <= 0 || entryPrice <= 0 || c.PriceUnit <= 0 {
		return 0
	}

	return c.calculateStaticReversionExitPrice(entryPrice, c.Config.FundingReversion.StopLossPct, false)
}

func (c *Candidate) calculateStaticReversionExitPrice(entryPrice, pct float64, favorable bool) float64 {
	longUp := c.Side == shared.SideOpenLong && favorable
	shortStop := c.Side == shared.SideOpenShort && !favorable
	if longUp || shortStop {
		price := decmath.Mul(entryPrice, decmath.Add(1, pct))
		return decmath.RoundToScale(decmath.SnapToTickFloor(price, c.PriceUnit), c.PriceScale)
	}

	price := decmath.Mul(entryPrice, decmath.Sub(1, pct))
	return decmath.RoundToScale(decmath.SnapToTickCeil(price, c.PriceUnit), c.PriceScale)
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
