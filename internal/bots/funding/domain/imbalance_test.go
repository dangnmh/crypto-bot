package domain_test

import (
	"strings"
	"testing"

	"crypto-bot/internal/bots/funding/domain"
	shared "crypto-bot/internal/domain"
)

func TestCalculateImbalanceRatio_NearLevelsOnly(t *testing.T) {
	t.Parallel()

	ob := &shared.OrderBook{
		Bids: []shared.OrderBookEntry{
			{Price: 99.95, Volume: 10},
			{Price: 99.80, Volume: 1000},
		},
		Asks: []shared.OrderBookEntry{
			{Price: 100.05, Volume: 5},
			{Price: 100.30, Volume: 1000},
		},
	}

	got, ok := domain.CalculateImbalanceRatio(ob, 100, 0.001)
	if !ok {
		t.Fatal("expected ratio")
	}
	if got != 2 {
		t.Fatalf("ratio = %v, want 2", got)
	}
}

func TestEvaluateImbalanceFilter_RejectsDirectionalImbalance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		side       shared.Side
		filter     domain.ImbalanceFilterConfig
		bidVolume  float64
		askVolume  float64
		rejectText string
	}{
		{
			name: "long low ratio",
			side: shared.SideOpenLong,
			filter: domain.ImbalanceFilterConfig{
				Enabled:      true,
				NearPct:      0.001,
				MinLongRatio: 1.2,
			},
			bidVolume:  5,
			askVolume:  10,
			rejectText: "too low for long",
		},
		{
			name: "short high ratio",
			side: shared.SideOpenShort,
			filter: domain.ImbalanceFilterConfig{
				Enabled:       true,
				NearPct:       0.001,
				MaxShortRatio: 0.8,
			},
			bidVolume:  10,
			askVolume:  5,
			rejectText: "too high for short",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := domain.Candidate{
				Config: domain.TradeConfig{
					FundingReversion: domain.FundingReversionConfig{
						ImbalanceFilter: tt.filter,
					},
				},
				TradeIntent: domain.TradeIntent{
					Side: tt.side,
				},
				MarketData: domain.MarketData{BestBid: 99.9, BestAsk: 100.1},
			}
			ob := &shared.OrderBook{
				Bids: []shared.OrderBookEntry{{Price: 99.95, Volume: tt.bidVolume}},
				Asks: []shared.OrderBookEntry{{Price: 100.05, Volume: tt.askVolume}},
			}

			got := c.EvaluateImbalanceFilter(ob)
			if got.Passed {
				t.Fatal("expected imbalance filter to fail")
			}
			if !strings.Contains(got.RejectReason, tt.rejectText) {
				t.Fatalf("unexpected reject reason: %q", got.RejectReason)
			}
		})
	}
}
