package domain

import (
	"errors"
	"fmt"

	shared "crypto-bot/internal/domain"
	"crypto-bot/pkg/tradecalc"
)

// CalculateIOCPrice calculates the IOC limit price based on side and market conditions.
//
// LONG: iocPrice = bestAsk + slippage  (buy at higher price, floor to tick)
// SHORT: iocPrice = bestBid - slippage  (sell at lower price, ceil to tick)
//
// Prices are rounded to the nearest valid tick (priceUnit) then snapped to PriceScale decimals.
func (c *Candidate) CalculateIOCPrice() (float64, error) {
	price, err := tradecalc.CalculateIOCPrice(
		tradecalc.Side(c.Side),
		c.BestBid,
		c.BestAsk,
		c.Config.MaxPriceDiffPercent,
		c.PriceUnit,
		c.PriceScale,
	)
	if err != nil {
		if errors.Is(err, tradecalc.ErrInvalidSide) {
			return 0, fmt.Errorf("%w: %d", ErrInvalidSide, c.Side)
		}
		if errors.Is(err, tradecalc.ErrInvalidPriceUnit) {
			return 0, fmt.Errorf("%w: %f", ErrInvalidPriceUnit, c.PriceUnit)
		}
		if errors.Is(err, tradecalc.ErrZeroRefPrice) {
			var refPrice float64
			if c.Side == shared.SideOpenLong {
				refPrice = c.BestAsk
			} else {
				refPrice = c.BestBid
			}
			return 0, fmt.Errorf("%w: %f", ErrZeroRefPrice, refPrice)
		}
		if errors.Is(err, tradecalc.ErrZeroIOCPrice) {
			return 0, ErrZeroIOCPrice
		}
		return 0, err
	}
	return price, nil
}

// wallMultiplier defines the threshold for detecting a liquidity wall.
// A price level with volume >= wallMultiplier × average volume is a wall.
const wallMultiplier = 3.0

// minWallLevels is the minimum number of OB levels needed for wall detection.
const minWallLevels = 3

// CalculateVolume calculates the number of contracts to trade based on configuration.
// It uses the side-appropriate reference price (BestAsk for LONG, BestBid for SHORT)
// to avoid margin overcommit when LastPrice lags behind the orderbook.
// Falls back to LastPrice if BestBid/BestAsk is unavailable.
func (c *Candidate) CalculateVolume() float64 {
	return tradecalc.CalculateVolume(
		c.Config.MarginUSDT,
		float64(c.Config.Leverage),
		c.ContractSize,
		c.ExecutionRefPrice(),
		float64(c.MinVol),
		c.VolScale,
	)
}

// ExecutionRefPrice returns the side-appropriate reference price for sizing.
func (c *Candidate) ExecutionRefPrice() float64 {
	return tradecalc.ExecutionRefPrice(
		tradecalc.Side(c.Side),
		c.LastPrice,
		c.BestBid,
		c.BestAsk,
	)
}

// CalculateTrapVolume calculates trap contracts from the dedicated trap sizing
// controls. If no trap sizing is configured, it preserves the candidate volume
// for tests and legacy direct domain callers; loaded configs default sizeRatio.
func (c *Candidate) CalculateTrapVolume(trapPrice float64) float64 {
	hasTrapSizing := c.Config.FundingTrap.SizeRatio > 0 || c.Config.FundingTrap.MaxNotionalUSDT > 0
	return tradecalc.CalculateTrapVolume(
		hasTrapSizing,
		c.Volume,
		c.ReversionNotionalUSDT(),
		c.Config.FundingTrap.SizeRatio,
		c.Config.FundingTrap.MaxNotionalUSDT,
		trapPrice,
		c.ContractSize,
		float64(c.MinVol),
		c.VolScale,
	)
}

// CalculateVolumeForNotional converts a USDT notional budget into exchange
// contract volume using the provided reference price.
func (c *Candidate) CalculateVolumeForNotional(notional, refPrice float64) float64 {
	return tradecalc.CalculateVolumeForNotional(
		notional,
		refPrice,
		c.ContractSize,
		float64(c.MinVol),
		c.VolScale,
	)
}

// ReversionNotionalUSDT returns the configured IOC notional budget.
func (c *Candidate) ReversionNotionalUSDT() float64 {
	return tradecalc.ReversionNotionalUSDT(c.Config.MarginUSDT, c.Config.Leverage)
}

// TrapTargetNotionalUSDT returns the configured trap notional budget after
// applying the trap size ratio and optional absolute cap.
func (c *Candidate) TrapTargetNotionalUSDT() float64 {
	return tradecalc.TrapTargetNotionalUSDT(
		c.ReversionNotionalUSDT(),
		c.Config.FundingTrap.SizeRatio,
		c.Config.FundingTrap.MaxNotionalUSDT,
	)
}

// NotionalForVolume estimates USDT notional for an exchange contract volume at
// the provided reference price.
func (c *Candidate) NotionalForVolume(volume, refPrice float64) float64 {
	return tradecalc.NotionalForVolume(volume, refPrice, c.ContractSize)
}

// GetPeakPrice returns the reference extreme price right before firing IOC logic.
func (c *Candidate) GetPeakPrice() float64 {
	return tradecalc.GetPeakPrice(tradecalc.Side(c.Side), c.BestBid, c.BestAsk)
}

// CalculateTrapPrice calculates the post-only Trap price, snapped to precision.
// IMPORTANT: side MUST represent the original IOC Sniper entry direction (LONG or SHORT).
func (c *Candidate) CalculateTrapPrice() float64 {
	return tradecalc.CalculateTrapPrice(
		tradecalc.Side(c.Side),
		c.GetPeakPrice(),
		c.Config.FundingTrap.DepthPct,
		c.PriceUnit,
		c.PriceScale,
	)
}
