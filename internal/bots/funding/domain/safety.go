package domain

import (
	"fmt"
	"math"

	"crypto-bot/pkg/decmath"
)

// SafetyResult holds the result of safety evaluation for a candidate.
type SafetyResult struct {
	Passed              bool
	RejectReason        string
	DesiredNotionalUSDT float64
	ActualNotionalUSDT  float64
	MaxSafeNotionalUSDT float64
	AvgMinuteVolumeUSDT float64
	ImpactRatio         float64
	EstSlippage         float64
	ExpectedProfit      float64
	SizedDown           bool
}

// SafetyLimits defines global safety constraints in domain terms.
type SafetyLimits struct {
	MaxImpactRatio float64
	MinVol24USD    float64
}

const minutesPerDay = 24 * 60

// EvaluateSafety evaluates whether a candidate is safe to trade based on its own config and global safety limits.
func (c *Candidate) EvaluateSafety(limits SafetyLimits) *SafetyResult {
	result := &SafetyResult{Passed: true}

	if c.Vol24USDT == 0 && c.Volume24 > 0 && c.LastPrice > 0 {
		c.Vol24USDT = c.Volume24 * c.LastPrice
	}

	desiredNotional := c.ReversionNotionalUSDT()

	result.DesiredNotionalUSDT = desiredNotional
	result.ActualNotionalUSDT = desiredNotional

	if limits.MinVol24USD > 0 && c.Vol24USDT < limits.MinVol24USD {
		result.Passed = false
		result.RejectReason = fmt.Sprintf("24h volume %.4f below minimum %.4f", c.Vol24USDT, limits.MinVol24USD)
		return result
	}

	// Impact ratio is measured against average one-minute turnover:
	// maxNotional = amount24hUSD / 1440 * maxImpactRatio.
	if c.Vol24USDT > 0 {
		result.AvgMinuteVolumeUSDT = decmath.Div(c.Vol24USDT, minutesPerDay)
		result.MaxSafeNotionalUSDT = decmath.Mul(result.AvgMinuteVolumeUSDT, limits.MaxImpactRatio)
		if result.MaxSafeNotionalUSDT > 0 && desiredNotional > result.MaxSafeNotionalUSDT {
			result.ActualNotionalUSDT = result.MaxSafeNotionalUSDT
			result.SizedDown = true
		}
		if result.AvgMinuteVolumeUSDT > 0 {
			result.ImpactRatio = decmath.Div(result.ActualNotionalUSDT, result.AvgMinuteVolumeUSDT)
		}
	}

	return c.evaluateTradeSafety(result)
}

// ApplySafetySizing caps candidate volume to the liquidity-safe notional.
func (c *Candidate) ApplySafetySizing(limits SafetyLimits) *SafetyResult {
	result := c.EvaluateSafety(limits)
	if !result.Passed {
		return result
	}

	refPrice := c.ExecutionRefPrice()
	if refPrice <= 0 {
		result.Passed = false
		result.RejectReason = "invalid execution reference price"
		return result
	}

	if result.SizedDown {
		c.Volume = c.CalculateVolumeForNotional(result.ActualNotionalUSDT, refPrice)
	}

	result.ActualNotionalUSDT = c.NotionalForVolume(c.Volume, refPrice)
	if result.MaxSafeNotionalUSDT > 0 && result.ActualNotionalUSDT > result.MaxSafeNotionalUSDT {
		result.Passed = false
		result.RejectReason = fmt.Sprintf(
			"minimum volume notional %.4f exceeds max safe notional %.4f USDT",
			result.ActualNotionalUSDT,
			result.MaxSafeNotionalUSDT,
		)
		return result
	}
	if result.AvgMinuteVolumeUSDT > 0 {
		result.ImpactRatio = decmath.Div(result.ActualNotionalUSDT, result.AvgMinuteVolumeUSDT)
	}

	return c.evaluateTradeSafety(result)
}

func (c *Candidate) evaluateTradeSafety(result *SafetyResult) *SafetyResult {
	// Check if the required notional to trade the minimum volume exceeds our budget.
	refPrice := c.ExecutionRefPrice()
	requiredNotional := c.NotionalForVolume(c.Volume, refPrice)
	budgetNotional := c.ReversionNotionalUSDT()

	if budgetNotional > 0 && requiredNotional > budgetNotional*1.01 {
		result.Passed = false
		result.RejectReason = fmt.Sprintf("insufficient budget (required notional %.4f exceeds budget %.4f)", requiredNotional, budgetNotional)
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
	// We subtract the estimated static slippage and round-trip taker fees.
	feePct := decmath.Mul(decmath.Mul(c.TakerFeeRate, 100.0), 2) // round-trip
	grossProfitPct := math.Abs(decmath.Mul(c.FundingRate, 100.0))
	result.ExpectedProfit = decmath.Sub(decmath.Sub(grossProfitPct, result.EstSlippage), feePct)

	return result
}
