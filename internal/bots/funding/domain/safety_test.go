package domain_test

import (
	"math"
	"testing"

	domain "crypto-bot/internal/bots/funding/domain"
)

func TestEvaluateSafety(t *testing.T) {
	t.Parallel()
	c := domain.Candidate{
		Config: domain.TradeConfig{
			MarginUSDT:          100,
			Leverage:            10,
			MaxPriceDiffPercent: 0.5,
		},
		TradeIntent: domain.TradeIntent{FundingRate: 0.02}, // 2%
		TradePlan:   domain.TradePlan{Volume: 50, Slippage: 0},
		MarketData: domain.MarketData{
			Amount24: 1000000, // Large liquidity
		},
		ContractSpec: domain.ContractSpec{
			MinVol:       10,
			TakerFeeRate: 0.0006, // 0.06%
		},
	}

	maxImpact := 0.05

	t.Run("Passed Safety", func(t *testing.T) {
		t.Parallel()
		res := c.EvaluateSafety(maxImpact)
		if !res.Passed {
			t.Errorf("expected safety to pass, got rejected: %s", res.RejectReason)
		}
		if res.PositionSizeUSDT != 1000 {
			t.Errorf("expected position size 1000, got %f", res.PositionSizeUSDT)
		}
		if res.ImpactRatio != 0.001 {
			t.Errorf("expected impact ratio 0.001, got %f", res.ImpactRatio)
		}
		if res.EstSlippage != 0.5 {
			t.Errorf("expected EstSlippage 0.5 (from config), got %f", res.EstSlippage)
		}
		// Gross = 2.0
		// Slippage = 0.5
		// Fee = 0.06 * 2 = 0.12
		// ExpectedProfit = 2.0 - 0.5 - 0.12 = 1.38
		if math.Abs(res.ExpectedProfit-1.38) > 1e-6 {
			t.Errorf("expected ExpectedProfit 1.38, got %f", res.ExpectedProfit)
		}
	})

	t.Run("Failed High Impact Ratio", func(t *testing.T) {
		t.Parallel()
		c2 := c
		c2.Amount24 = 10000 // 1000 / 10000 = 0.1 > maxImpact 0.05
		res := c2.EvaluateSafety(maxImpact)
		if res.Passed {
			t.Error("expected safety to fail due to high impact ratio")
		}
	})

	t.Run("Failed Low Volume", func(t *testing.T) {
		t.Parallel()
		c3 := c
		c3.Volume = 5 // < MinVol 10
		res := c3.EvaluateSafety(maxImpact)
		if res.Passed {
			t.Error("expected safety to fail due to low volume (minVol constraint)")
		}
	})

	t.Run("Slippage Override from Orderbook", func(t *testing.T) {
		t.Parallel()
		c4 := c
		c4.Slippage = 1.2 // Calculated slippage takes precedence over config (0.5)
		res := c4.EvaluateSafety(maxImpact)
		if res.EstSlippage != 1.2 {
			t.Errorf("expected EstSlippage 1.2, got %f", res.EstSlippage)
		}
		// ExpectedProfit = 2.0 - 1.2 - 0.12 = 0.68
		if math.Abs(res.ExpectedProfit-0.68) > 1e-6 {
			t.Errorf("expected ExpectedProfit 0.68, got %f", res.ExpectedProfit)
		}
	})
}
