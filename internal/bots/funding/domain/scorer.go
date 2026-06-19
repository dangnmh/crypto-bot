package domain

import (
	"math"
	"sort"
)

// ScoreAndRank scores candidates and returns the sorted list.
func ScoreAndRank(candidates []Candidate) []Candidate {
	// Calculate scores
	for i := range candidates {
		c := &candidates[i]
		if c.SafetyResult == nil || !c.SafetyResult.Passed {
			c.CoinScore = 0
			continue
		}

		// Score = expectedProfit × liquidityScore
		// liquidityScore: higher volume = more liquid = better
		liquidityScore := math.Log10(c.AmountUSDT24+1) / 10.0
		if liquidityScore > 1 {
			liquidityScore = 1
		}

		c.ExpectedProfit = c.SafetyResult.ExpectedProfit
		c.ImpactRatio = c.SafetyResult.ImpactRatio

		c.CoinScore = c.ExpectedProfit * 10000 * liquidityScore
	}

	// Filter passed candidates
	var passed []Candidate
	for i := range candidates {
		if candidates[i].CoinScore > 0 {
			passed = append(passed, candidates[i])
		}
	}

	// Sort by score descending
	sort.Slice(passed, func(i, j int) bool {
		return passed[i].CoinScore > passed[j].CoinScore
	})

	return passed
}
