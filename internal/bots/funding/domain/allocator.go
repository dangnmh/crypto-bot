package domain

import (
	"context"
	"log/slog"
	"sort"
)

// MarginAllocator defines the domain strategy interface for allocating wallet margin
// and computing contract trade volumes across scanned trading candidates.
type MarginAllocator interface {
	// AllocateMargins sequentially allocates available wallet margin pool (totalMarginUSD)
	// across candidates, enforcing candidate margin limits, market impact caps, and exchange risk limits.
	// Returns a slice containing only tradeable candidates with valid contract volume (> 0).
	AllocateMargins(
		ctx context.Context,
		candidates []Candidate,
		totalMarginUSD float64,
		maxCandidateMargin float64,
		maxImpactRatio float64,
		client any,
		logger *slog.Logger,
	) []Candidate
}

// AscendingVolumeMarginAllocator allocates available margin sequentially to candidates
// sorted in ascending order of 24h volume (Vol24USDT).
// Sorting ascending allows candidates requiring smaller position sizes to be funded first,
// maximizing the number of orders executed across portfolio opportunities.
type AscendingVolumeMarginAllocator struct{}

// NewAscendingVolumeMarginAllocator constructs a new AscendingVolumeMarginAllocator.
func NewAscendingVolumeMarginAllocator() *AscendingVolumeMarginAllocator {
	return &AscendingVolumeMarginAllocator{}
}

// AllocateMargins allocates available total margin sequentially across candidates in ascending 24h volume order.
// Filters out candidates that receive zero allocated margin or contract volume.
func (a *AscendingVolumeMarginAllocator) AllocateMargins(
	ctx context.Context,
	candidates []Candidate,
	totalMarginUSD float64,
	maxCandidateMargin float64,
	maxImpactRatio float64,
	client any,
	logger *slog.Logger,
) []Candidate {
	if len(candidates) == 0 || totalMarginUSD <= 0 {
		return nil
	}

	result := make([]Candidate, len(candidates))
	copy(result, candidates)

	// Step 1: Sort candidates in ascending order by 24h USDT volume (Vol24USDT asc).
	// Lower volume candidates have smaller market impact caps and require less margin pool,
	// allowing more total candidates/orders to be placed.
	sort.Slice(result, func(i, j int) bool {
		return result[i].Vol24USDT < result[j].Vol24USDT
	})

	remainingMargin := totalMarginUSD

	for i := range result {
		if remainingMargin <= 0 {
			result[i].Config.MarginUSDT = 0
			result[i].Config.Leverage = 1
			result[i].Volume = 0
			continue
		}

		needMarginUSDT := allocateSingleCandidateMargin(
			ctx,
			&result[i],
			remainingMargin,
			maxCandidateMargin,
			maxImpactRatio,
			client,
			logger,
		)

		remainingMargin -= needMarginUSDT
	}

	// Step 9: Filter and return only tradeable candidates with valid contract volume and margin (> 0).
	validCandidates := make([]Candidate, 0, len(result))
	for i := range result {
		if result[i].Volume > 0 && result[i].Config.MarginUSDT > 0 {
			validCandidates = append(validCandidates, result[i])
		}
	}

	if len(validCandidates) == 0 {
		return nil
	}

	return validCandidates
}

func allocateSingleCandidateMargin(
	ctx context.Context,
	c *Candidate,
	remainingMargin float64,
	maxCandidateMargin float64,
	maxImpactRatio float64,
	client any,
	logger *slog.Logger,
) float64 {
	// Step 2: Cap available margin for this candidate by maxCandidateMargin if configured.
	candidateMarginLimit := remainingMargin
	if maxCandidateMargin > 0 && candidateMarginLimit > maxCandidateMargin {
		candidateMarginLimit = maxCandidateMargin
	}

	// Step 3: Compute market impact volume ceiling (maxImpactRatio % of 1-minute volume).
	volMinuteUSDT := c.Vol24USDT / 1440.0
	var maxTradeVolUSDT float64
	if maxImpactRatio > 0 && volMinuteUSDT > 0 {
		maxTradeVolUSDT = (maxImpactRatio / 100.0) * volMinuteUSDT
	}

	// Step 4: Calculate target trade position volume based on candidate leverage & market impact cap.
	targetTradeVolUSDT := candidateMarginLimit * float64(c.Config.Leverage)
	actualTradeVolUSDT := targetTradeVolUSDT
	if maxTradeVolUSDT > 0 && actualTradeVolUSDT > maxTradeVolUSDT {
		actualTradeVolUSDT = maxTradeVolUSDT
	}

	// Step 5: Temporarily set candidate margin limit & resolve exchange risk-limit leverage.
	c.Config.MarginUSDT = candidateMarginLimit

	actualLeverage := DetermineCandidateLeverage(ctx, client, c, logger)
	if actualLeverage <= 0 {
		actualLeverage = 1
	}

	// Step 6: Compute required margin for position size under resolved exchange leverage.
	needMarginUSDT := actualTradeVolUSDT / float64(actualLeverage)
	if needMarginUSDT > candidateMarginLimit {
		needMarginUSDT = candidateMarginLimit
	}

	// Step 7: Update final candidate trade parameters and calculate exchange lot contract volume.
	c.Config.MarginUSDT = needMarginUSDT
	c.Config.Leverage = actualLeverage
	c.Volume = c.CalculateVolume()

	return needMarginUSDT
}
