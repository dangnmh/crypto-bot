package domain

import (
	"fmt"

	shared "crypto-bot/internal/domain"
	"crypto-bot/pkg/decmath"
)

// DepthImbalanceResult represents the orderbook depth imbalance within a target percentage of BBO.
type DepthImbalanceResult struct {
	BidNotional float64
	AskNotional float64
	Ratio       float64 // BidNotional / AskNotional
	SuggestSkip bool
	SkipReason  string
}

// EvaluateDepthImbalance calculates near-market orderbook depth imbalance (e.g. within 1% of BBO)
// and evaluates if the liquidity distribution warns against executing the planned trade side.
//
// For Short trades: If BidNotional / AskNotional >= threshold (e.g. 1.5), strong buying support suggests
// high risk of short squeeze or failure to dump, triggering SuggestSkip = true.
//
// For Long trades: If AskNotional / BidNotional >= threshold (Ratio <= 1/threshold), strong selling resistance
// suggests high dump risk, triggering SuggestSkip = true.
func EvaluateDepthImbalance(ob *shared.OrderBook, side shared.Side, depthPct, threshold float64) DepthImbalanceResult {
	if ob == nil || len(ob.Bids) == 0 || len(ob.Asks) == 0 {
		return DepthImbalanceResult{Ratio: 1.0}
	}
	if depthPct <= 0 {
		depthPct = 0.01 // Default 1%
	}
	if threshold <= 1.0 {
		threshold = 1.5 // Default 1.5x
	}

	bidNotional, askNotional := calcDepthNotionals(ob, depthPct)
	ratio := calculateDepthRatio(bidNotional, askNotional)
	suggestSkip, skipReason := checkSkipCondition(side, bidNotional, askNotional, ratio, threshold, depthPct)

	return DepthImbalanceResult{
		BidNotional: bidNotional,
		AskNotional: askNotional,
		Ratio:       ratio,
		SuggestSkip: suggestSkip,
		SkipReason:  skipReason,
	}
}

func calcDepthNotionals(ob *shared.OrderBook, depthPct float64) (float64, float64) {
	bestBid := ob.Bids[0].Price
	bestAsk := ob.Asks[0].Price
	if bestBid <= 0 || bestAsk <= 0 {
		return 0, 0
	}

	bidThreshold := decmath.Mul(bestBid, decmath.Sub(1.0, depthPct))
	askThreshold := decmath.Mul(bestAsk, decmath.Add(1.0, depthPct))

	bidNotional := 0.0
	for _, b := range ob.Bids {
		if b.Price < bidThreshold {
			break
		}
		bidNotional = decmath.Add(bidNotional, decmath.Mul(b.Price, b.Volume))
	}

	askNotional := 0.0
	for _, a := range ob.Asks {
		if a.Price > askThreshold {
			break
		}
		askNotional = decmath.Add(askNotional, decmath.Mul(a.Price, a.Volume))
	}

	return bidNotional, askNotional
}

func calculateDepthRatio(bidNotional, askNotional float64) float64 {
	if askNotional > 0 {
		return decmath.Div(bidNotional, askNotional)
	}
	if bidNotional > 0 {
		return 10.0
	}
	return 1.0
}

func checkSkipCondition(side shared.Side, bidNotional, askNotional, ratio, threshold, depthPct float64) (bool, string) {
	if side.IsShort() && ratio >= threshold {
		reason := fmt.Sprintf(
			"bid depth (%.2f USDT) is %.1fx heavier than ask depth (%.2f USDT) within %.1f%% - high squeeze risk",
			bidNotional, ratio, askNotional, depthPct*100,
		)
		return true, reason
	}

	if side.IsLong() {
		invRatio := calculateDepthRatio(askNotional, bidNotional)
		if invRatio >= threshold {
			reason := fmt.Sprintf(
				"ask depth (%.2f USDT) is %.1fx heavier than bid depth (%.2f USDT) within %.1f%% - high dump risk",
				askNotional, invRatio, bidNotional, depthPct*100,
			)
			return true, reason
		}
	}

	return false, ""
}
