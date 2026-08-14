package domain

import (
	"context"
	"log/slog"

	shared "crypto-bot/internal/domain"
	"crypto-bot/pkg/decmath"
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

// CalculateOrderTPSL computes static TakeProfitPrice and StopLossPrice relative to Candidate.GetPeakPrice(),
// and validates them against iocPrice. If TP or SL violates price direction relative to iocPrice, it is dropped.
func (c *Candidate) CalculateOrderTPSL(ctx context.Context, iocPrice float64, log *slog.Logger) (float64, float64) {
	var tpPrice float64
	maxTPPct := decmath.Mul(c.Config.FundingReversion.TakeProfitPct, 100.0)
	if maxTPPct > 0 {
		tpPrice = c.CalculateStaticTakeProfitPrice(c.GetPeakPrice())
	}

	slPrice := c.CalculateStopLossPrice(c.GetPeakPrice())

	if c.Side == shared.SideOpenLong {
		if tpPrice > 0 && tpPrice <= iocPrice {
			if log != nil {
				log.WarnContext(ctx, "🟡 TP below IOC price (LONG), dropping TP",
					slog.Float64("tp", tpPrice), slog.Float64("ioc", iocPrice))
			}
			tpPrice = 0
		}
		if slPrice > 0 && slPrice >= iocPrice {
			if log != nil {
				log.WarnContext(ctx, "🟡 SL above IOC price (LONG), dropping SL",
					slog.Float64("sl", slPrice), slog.Float64("ioc", iocPrice))
			}
			slPrice = 0
		}
	} else {
		if tpPrice > 0 && tpPrice >= iocPrice {
			if log != nil {
				log.WarnContext(ctx, "🟡 TP above IOC price (SHORT), dropping TP",
					slog.Float64("tp", tpPrice), slog.Float64("ioc", iocPrice))
			}
			tpPrice = 0
		}
		if slPrice > 0 && slPrice <= iocPrice {
			if log != nil {
				log.WarnContext(ctx, "🟡 SL below IOC price (SHORT), dropping SL",
					slog.Float64("sl", slPrice), slog.Float64("ioc", iocPrice))
			}
			slPrice = 0
		}
	}

	return tpPrice, slPrice
}
