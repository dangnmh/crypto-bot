package domain

import (
	"math"

	"crypto-bot/internal/bots/funding_reversion/config"
	"crypto-bot/internal/infrastructure/exchange"
)

// SlippageCalculator defines the strategy interface for computing slippage.
// Each implementation encapsulates a different market-adaptive pricing algorithm.
type SlippageCalculator interface {
	Calculate(refPrice float64, ob *exchange.OrderBook) float64
}

// staticSlippage implements fixed-percentage slippage (fallback mode).
type staticSlippage struct {
	maxDiffPct float64
	priceUnit  float64
}

func (s *staticSlippage) Calculate(refPrice float64, _ *exchange.OrderBook) float64 {
	return math.Max(refPrice*(s.maxDiffPct/100.0), s.priceUnit*2)
}

// spreadSlippage implements spread-multiplier slippage with hard cap.
type spreadSlippage struct {
	maxDiffPct     float64
	multiplier     float64
	maxSlippagePct float64
	bestBid        float64
	bestAsk        float64
	priceUnit      float64
}

func (s *spreadSlippage) Calculate(refPrice float64, _ *exchange.OrderBook) float64 {
	maxDiff := s.maxDiffPct
	if s.bestBid > 0 {
		spreadPct := (s.bestAsk - s.bestBid) / s.bestBid * 100.0
		dynDiff := spreadPct * s.multiplier
		if dynDiff > maxDiff {
			maxDiff = dynDiff
		}

		// Hard cap: prevent extreme slippage when spread is abnormally wide.
		if s.maxSlippagePct > 0 && maxDiff > s.maxSlippagePct {
			maxDiff = s.maxSlippagePct
		}
	}
	return math.Max(refPrice*(maxDiff/100.0), s.priceUnit*2)
}

// obImbalanceSlippage implements orderbook-sweep slippage with hard cap.
type obImbalanceSlippage struct {
	volume         float64
	side           int
	bufferPct      float64
	maxSlippagePct float64
	priceUnit      float64
}

func (o *obImbalanceSlippage) Calculate(refPrice float64, ob *exchange.OrderBook) float64 {
	if ob == nil {
		// Fallback: if no OB data, return minimum tick slippage
		return o.priceUnit * 2
	}

	sweepPrice := refPrice
	remVol := o.volume

	if o.side == exchange.SideOpenLong {
		for _, ask := range ob.Asks {
			sweepPrice = ask.Price
			remVol -= ask.Volume
			if remVol <= 0 {
				break
			}
		}
		sweepPrice *= (1 + o.bufferPct/100.0)

		maxPrice := refPrice * (1 + o.maxSlippagePct/100.0)
		if sweepPrice > maxPrice {
			sweepPrice = maxPrice
		}
		return sweepPrice - refPrice
	}

	// SHORT → Eat Bids (Descending)
	for _, bid := range ob.Bids {
		sweepPrice = bid.Price
		remVol -= bid.Volume
		if remVol <= 0 {
			break
		}
	}
	sweepPrice *= (1 - o.bufferPct/100.0)

	minPrice := refPrice * (1 - o.maxSlippagePct/100.0)
	if sweepPrice < minPrice {
		sweepPrice = minPrice
	}
	return refPrice - sweepPrice
}

// newSlippageCalculator is the factory that selects the appropriate strategy
// based on the candidate's dynamic pricing configuration.
func newSlippageCalculator(c *Candidate, dyn config.DynamicPricingConfig) SlippageCalculator {
	if !dyn.Enabled {
		return &staticSlippage{
			maxDiffPct: c.Config.MaxPriceDiffPercent,
			priceUnit:  c.PriceUnit,
		}
	}

	switch dyn.SlippageMode {
	case "OB_IMBALANCE":
		return &obImbalanceSlippage{
			volume:         c.Volume,
			side:           c.Side,
			bufferPct:      dyn.ObBufferPct,
			maxSlippagePct: dyn.ObMaxSlippagePct,
			priceUnit:      c.PriceUnit,
		}
	default: // "SPREAD_MULTIPLIER" or fallback
		return &spreadSlippage{
			maxDiffPct:     c.Config.MaxPriceDiffPercent,
			multiplier:     dyn.SpreadMultiplier,
			maxSlippagePct: dyn.ObMaxSlippagePct,
			bestBid:        c.BestBid,
			bestAsk:        c.BestAsk,
			priceUnit:      c.PriceUnit,
		}
	}
}
