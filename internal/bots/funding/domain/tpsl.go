package domain

import (
	shared "crypto-bot/internal/domain"
	"crypto-bot/pkg/tradecalc"
)

// CalculateStaticTakeProfitPrice computes the Reversion server-side TP from
// the configured static TakeProfitPct only. It intentionally ignores orderbook
// walls because settlement liquidity can disappear before the TP is reached.
func (c *Candidate) CalculateStaticTakeProfitPrice(entryPrice float64) float64 {
	return tradecalc.CalculateStaticTakeProfitPrice(
		tradecalc.Side(c.Side),
		entryPrice,
		c.Config.FundingReversion.TakeProfitPct,
		c.PriceUnit,
		c.PriceScale,
	)
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
	pkgLevels := make([]tradecalc.OrderBookEntry, len(levels))
	for i, l := range levels {
		pkgLevels[i] = tradecalc.OrderBookEntry{Price: l.Price, Volume: l.Volume}
	}
	return tradecalc.FindWallPrice(
		c.entryRefPrice(),
		pkgLevels,
		tradecalc.Side(side),
		minWallLevels,
		wallMultiplier,
	)
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
	return tradecalc.CalculateOBTrapPrice(
		tradecalc.Side(trapSide),
		wallPrice,
		c.PriceUnit,
	)
}

// TrapWallDistancePct returns the wall distance from the current candidate
// price context in percent units.
func (c *Candidate) TrapWallDistancePct(wallPrice float64) float64 {
	ref := c.LastPrice
	if ref <= 0 {
		ref = c.entryRefPrice()
	}
	return tradecalc.TrapWallDistancePct(wallPrice, ref)
}

// CalculateStopLossPrice computes a server-side SL price relative to entryPrice.
// LONG: SL below entry (ceil to avoid triggering too early).
// SHORT: SL above entry (floor to avoid triggering too early).
// Returns 0 if StopLossPct is not configured or entryPrice is invalid.
func (c *Candidate) CalculateStopLossPrice(entryPrice float64) float64 {
	return tradecalc.CalculateStopLossPrice(
		tradecalc.Side(c.Side),
		entryPrice,
		c.Config.FundingReversion.StopLossPct,
		c.PriceUnit,
		c.PriceScale,
	)
}

// CalculateTrapTPPrice computes a server-side Take Profit price for a trap order.
// trapPrice is the limit entry price of the trap.
// LONG trap: TP above trapPrice (floor). SHORT trap: TP below trapPrice (ceil).
// Returns 0 if Trap TakeProfitPct is not configured.
func (c *Candidate) CalculateTrapTPPrice(trapPrice float64) float64 {
	return tradecalc.CalculateTrapTPPrice(
		tradecalc.Side(c.Side),
		trapPrice,
		c.Config.FundingTrap.TakeProfitPct,
		c.PriceUnit,
		c.PriceScale,
	)
}

// CalculateTrapSLPrice computes a server-side Stop Loss price for a trap order.
// trapPrice is the limit entry price of the trap.
// LONG trap: SL below trapPrice (ceil). SHORT trap: SL above trapPrice (floor).
// Returns 0 if Trap StopLossPct is not configured.
func (c *Candidate) CalculateTrapSLPrice(trapPrice float64) float64 {
	return tradecalc.CalculateTrapSLPrice(
		tradecalc.Side(c.Side),
		trapPrice,
		c.Config.FundingTrap.StopLossPct,
		c.PriceUnit,
		c.PriceScale,
	)
}
