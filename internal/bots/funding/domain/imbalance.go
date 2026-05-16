package domain

import (
	"fmt"

	shared "crypto-bot/internal/domain"
	"crypto-bot/pkg/decmath"
)

// ImbalanceFilterResult captures the optional near-book imbalance filter outcome.
type ImbalanceFilterResult struct {
	Enabled      bool
	Passed       bool
	Ratio        float64
	NearPct      float64
	RejectReason string
}

// CalculateImbalanceRatio returns bid volume near price divided by ask volume near price.
func CalculateImbalanceRatio(ob *shared.OrderBook, refPrice, nearPct float64) (float64, bool) {
	if ob == nil || refPrice <= 0 || nearPct <= 0 {
		return 0, false
	}

	minBid := decmath.Mul(refPrice, decmath.Sub(1, nearPct))
	maxAsk := decmath.Mul(refPrice, decmath.Add(1, nearPct))

	bidVol := nearVolume(ob.Bids, minBid, refPrice)
	askVol := nearVolume(ob.Asks, refPrice, maxAsk)
	if bidVol <= 0 || askVol <= 0 {
		return 0, false
	}
	return decmath.Div(bidVol, askVol), true
}

func nearVolume(levels []shared.OrderBookEntry, minPrice, maxPrice float64) float64 {
	var volume float64
	for _, level := range levels {
		if level.Price <= 0 || level.Volume <= 0 {
			continue
		}
		if level.Price >= minPrice && level.Price <= maxPrice {
			volume = decmath.Add(volume, level.Volume)
		}
	}
	return volume
}

// EvaluateImbalanceFilter applies the optional secondary imbalance filter.
func (c *Candidate) EvaluateImbalanceFilter(ob *shared.OrderBook) ImbalanceFilterResult {
	cfg := c.Config.FundingReversion.ImbalanceFilter
	result := ImbalanceFilterResult{
		Enabled: cfg.Enabled,
		Passed:  true,
		NearPct: cfg.NearPct,
	}
	if !cfg.Enabled {
		return result
	}

	refPrice := c.imbalanceRefPrice()
	ratio, ok := CalculateImbalanceRatio(ob, refPrice, cfg.NearPct)
	if !ok {
		result.Passed = false
		result.RejectReason = "imbalance ratio unavailable"
		return result
	}
	result.Ratio = ratio
	c.ImbalanceRatio = ratio

	switch c.Side {
	case shared.SideOpenLong:
		if cfg.MinLongRatio > 0 && ratio < cfg.MinLongRatio {
			result.Passed = false
			result.RejectReason = fmt.Sprintf("imbalance ratio too low for long (%.4f < %.4f)", ratio, cfg.MinLongRatio)
		}
	case shared.SideOpenShort:
		if cfg.MaxShortRatio > 0 && ratio > cfg.MaxShortRatio {
			result.Passed = false
			result.RejectReason = fmt.Sprintf("imbalance ratio too high for short (%.4f > %.4f)", ratio, cfg.MaxShortRatio)
		}
	default:
		result.Passed = false
		result.RejectReason = "imbalance ratio side unknown"
	}

	return result
}

func (c *Candidate) imbalanceRefPrice() float64 {
	if c.BestBid > 0 && c.BestAsk > 0 {
		return decmath.Div(decmath.Add(c.BestBid, c.BestAsk), 2)
	}
	return c.LastPrice
}
