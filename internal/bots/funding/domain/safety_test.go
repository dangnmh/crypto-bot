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
		FundingRate: 0.02, // 2%
		Volume:      50, Slippage: 0,
		MarketData: domain.MarketData{
			LastPrice: 100,
			BestAsk:   101,
			Vol24USDT: 10000000000, // Large liquidity
		},
		ContractSpec: domain.ContractSpec{
			ContractSize: 0.01,
			MinVol:       10,
			TakerFeeRate: 0.0006, // 0.06%
		},
	}

	limits := domain.SafetyLimits{MaxImpactRatio: 0.05}

	t.Run("Passed Safety", func(t *testing.T) {
		t.Parallel()
		res := c.EvaluateSafety(limits)
		if !res.Passed {
			t.Errorf("expected safety to pass, got rejected: %s", res.RejectReason)
		}
		if res.DesiredNotionalUSDT != 1000 {
			t.Errorf("expected desired notional 1000, got %f", res.DesiredNotionalUSDT)
		}
		if math.Abs(res.AvgMinuteVolumeUSDT-6944444.444444444) > 1e-6 {
			t.Errorf("expected avg one-minute volume 6944444.444444444, got %f", res.AvgMinuteVolumeUSDT)
		}
		if math.Abs(res.MaxSafeNotionalUSDT-347222.22222222225) > 1e-6 {
			t.Errorf("expected max safe notional 347222.22222222225, got %f", res.MaxSafeNotionalUSDT)
		}
		if math.Abs(res.ImpactRatio-0.000144) > 1e-9 {
			t.Errorf("expected impact ratio 0.000144, got %f", res.ImpactRatio)
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

	t.Run("Failed Low 24h Volume", func(t *testing.T) {
		t.Parallel()
		c5 := c
		c5.Vol24USDT = 999999
		res := c5.EvaluateSafety(domain.SafetyLimits{MinVol24USD: 1000000})
		if res.Passed {
			t.Error("expected safety to fail due to low 24h USD volume")
		}
	})

	t.Run("Failed Low Volume", func(t *testing.T) {
		t.Parallel()
		c3 := c
		c3.Volume = 5 // < MinVol 10
		res := c3.EvaluateSafety(limits)
		if res.Passed {
			t.Error("expected safety to fail due to low volume (minVol constraint)")
		}
	})

	t.Run("Slippage Override from Orderbook", func(t *testing.T) {
		t.Parallel()
		c4 := c
		c4.Slippage = 1.2 // Calculated slippage takes precedence over config (0.5)
		res := c4.EvaluateSafety(limits)
		if res.EstSlippage != 1.2 {
			t.Errorf("expected EstSlippage 1.2, got %f", res.EstSlippage)
		}
		// ExpectedProfit = 2.0 - 1.2 - 0.12 = 0.68
		if math.Abs(res.ExpectedProfit-0.68) > 1e-6 {
			t.Errorf("expected ExpectedProfit 0.68, got %f", res.ExpectedProfit)
		}
	})
}

func TestApplySafetySizing_SizesDownHighImpactRatio(t *testing.T) {
	t.Parallel()

	c := domain.Candidate{
		Config: domain.TradeConfig{
			MarginUSDT:          100,
			Leverage:            10,
			MaxPriceDiffPercent: 0.5,
		},
		FundingRate: 0.02,
		Volume:      50,
		MarketData: domain.MarketData{
			LastPrice: 100,
			BestAsk:   101,
			Vol24USDT: 1000000, // max = 1000000 / 1440 * 5% = 34.7222
		},
		ContractSize: 0.01,
		MinVol:       10,
		TakerFeeRate: 0.0006,
	}

	res := c.ApplySafetySizing(domain.SafetyLimits{MaxImpactRatio: 0.05})
	if !res.Passed {
		t.Fatalf("expected safety to pass by sizing down, got rejected: %s", res.RejectReason)
	}
	if !res.SizedDown {
		t.Fatal("expected safety to size down")
	}
	if math.Abs(res.MaxSafeNotionalUSDT-34.72222222222222) > 1e-9 {
		t.Errorf("expected max safe notional 34.72222222222222, got %f", res.MaxSafeNotionalUSDT)
	}
	if c.Volume <= 0 || c.Volume > 35 {
		t.Errorf("expected capped volume in (0, 35], got %f", c.Volume)
	}
}

func TestApplySafetySizing_InvalidRefPrice(t *testing.T) {
	t.Parallel()

	c := domain.Candidate{
		Config: domain.TradeConfig{
			MarginUSDT:          100,
			Leverage:            10,
			MaxPriceDiffPercent: 0.5,
		},
		FundingRate:  0.02,
		Volume:       50,
		LastPrice:    0, // Invalid refPrice
		BestAsk:      0,
		Vol24USDT:    1000000,
		ContractSize: 0.01,
		MinVol:       10,
		TakerFeeRate: 0.0006,
	}

	res := c.ApplySafetySizing(domain.SafetyLimits{MaxImpactRatio: 0.05})
	if res.Passed {
		t.Fatal("expected safety sizing to fail due to invalid refPrice")
	}
	if res.RejectReason != "invalid execution reference price" {
		t.Errorf("expected reject reason 'invalid execution reference price', got %q", res.RejectReason)
	}
}
