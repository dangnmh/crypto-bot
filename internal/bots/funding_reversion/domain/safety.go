package domain

import (
	"fmt"
	"math"

	"crypto-bot/pkg/decmath"
)

// SafetyResult holds the result of safety evaluation for a candidate.
type SafetyResult struct {
	Passed           bool
	RejectReason     string
	PositionSizeUSDT float64
	ImpactRatio      float64
	EstSlippage      float64
	ExpectedProfit   float64
}

// EvaluateSafety evaluates whether a candidate is safe to trade based on its own config and global safety limits.
func (c *Candidate) EvaluateSafety(maxImpactRatio float64) *SafetyResult {
	result := &SafetyResult{Passed: true}

	// Position size (USDT)
	positionSize := decmath.Mul(c.Config.MarginUSDT, float64(c.Config.Leverage))
	result.PositionSizeUSDT = positionSize

	// Impact ratio
	if c.Amount24 > 0 {
		result.ImpactRatio = decmath.Div(positionSize, c.Amount24)
	}

	if result.ImpactRatio > maxImpactRatio {
		result.Passed = false
		result.RejectReason = fmt.Sprintf("impact ratio too high (%.4f > %.4f)", result.ImpactRatio, maxImpactRatio)
		return result
	}

	// Minimum trade volume filter based on margin limit
	if c.Volume < float64(c.MinVol) {
		result.Passed = false
		result.RejectReason = fmt.Sprintf("insufficient marginUSDT (vol %.4f < minimum %d)", c.Volume, c.MinVol)
		return result
	}

	// Estimate actual slippage from the already-calculated Slippage field if available,
	// otherwise fall back to the static config value.
	result.EstSlippage = c.Config.MaxPriceDiffPercent
	if c.Slippage > 0 {
		result.EstSlippage = c.Slippage
	}

	// Calculate ExpectedProfit
	// Gross profit is the absolute funding rate percentage
	// We subtract the estimated slippage (which includes the entry/exit cost if using OB sweep)
	// and round-trip taker fees.
	feePct := decmath.Mul(decmath.Mul(c.TakerFeeRate, 100.0), 2) // round-trip
	grossProfitPct := math.Abs(decmath.Mul(c.FundingRate, 100.0))
	result.ExpectedProfit = decmath.Sub(decmath.Sub(grossProfitPct, result.EstSlippage), feePct)

	return result
}
