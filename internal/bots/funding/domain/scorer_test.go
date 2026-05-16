package domain_test

import (
	"testing"

	domain "crypto-bot/internal/bots/funding/domain"
)

func TestScoreAndRank(t *testing.T) {
	t.Parallel()
	candidates := []domain.Candidate{
		{
			TradeIntent: domain.TradeIntent{Symbol: "COIN_A"},
			TradePlan: domain.TradePlan{
				SafetyResult: &domain.SafetyResult{
					Passed:         true,
					ExpectedProfit: 1.5,
				},
			},
			MarketData: domain.MarketData{
				Amount24: 1000000, // 10^6 -> log10=6 -> score=0.6 -> CoinScore = 1.5 * 10000 * 0.6 = 9000
			},
		},
		{
			TradeIntent: domain.TradeIntent{Symbol: "COIN_B"},
			TradePlan: domain.TradePlan{
				SafetyResult: &domain.SafetyResult{
					Passed:         true,
					ExpectedProfit: 2.0,
				},
			},
			MarketData: domain.MarketData{
				Amount24: 100, // 10^2 -> log10=2 -> score=0.2 -> CoinScore = 2.0 * 10000 * 0.2 = 4000
			},
		},
		{
			TradeIntent: domain.TradeIntent{Symbol: "COIN_C"},
			TradePlan: domain.TradePlan{
				SafetyResult: &domain.SafetyResult{
					Passed:         true,
					ExpectedProfit: 0.5,
				},
			},
			MarketData: domain.MarketData{
				Amount24: 10000000000, // 10^10 -> log10=10 -> score=1.0 (capped) -> CoinScore = 0.5 * 10000 * 1.0 = 5000
			},
		},
		{
			TradeIntent: domain.TradeIntent{Symbol: "COIN_FAIL"},
			TradePlan: domain.TradePlan{
				SafetyResult: &domain.SafetyResult{
					Passed:         false, // Should be filtered out
					ExpectedProfit: 10.0,
				},
			},
			MarketData: domain.MarketData{
				Amount24: 1000000,
			},
		},
		{
			TradeIntent: domain.TradeIntent{Symbol: "COIN_NIL_SAFETY"},
			TradePlan:   domain.TradePlan{SafetyResult: nil}, // Should be filtered out
			MarketData: domain.MarketData{
				Amount24: 1000000,
			},
		},
	}

	ranked := domain.ScoreAndRank(candidates)

	if len(ranked) != 3 {
		t.Fatalf("expected 3 ranked candidates, got %d", len(ranked))
	}

	// Expected order by CoinScore descending:
	// COIN_A (9000 approx)
	// COIN_C (5000 approx)
	// COIN_B (4000 approx)

	if ranked[0].Symbol != "COIN_A" {
		t.Errorf("expected rank 1 to be COIN_A, got %s", ranked[0].Symbol)
	}
	if ranked[1].Symbol != "COIN_C" {
		t.Errorf("expected rank 2 to be COIN_C, got %s", ranked[1].Symbol)
	}
	if ranked[2].Symbol != "COIN_B" {
		t.Errorf("expected rank 3 to be COIN_B, got %s", ranked[2].Symbol)
	}

	for _, c := range ranked {
		if c.CoinScore <= 0 {
			t.Errorf("expected candidate %s to keep positive score", c.Symbol)
		}
	}
}
