package domain_test

import (
	"testing"

	domain "crypto-bot/internal/bots/funding/domain"

	shared "crypto-bot/internal/domain"
)

// Black-box slippage tests: exercise slippage behavior through the public
// CalculateIOCPrice API rather than testing internal calculator types directly.

func TestStaticSlippage_ViaIOCPrice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		side       shared.Side
		bestBid    float64
		bestAsk    float64
		priceUnit  float64
		maxDiffPct float64
		priceScale int
		wantMin    float64
		wantMax    float64
	}{
		{
			name:       "LONG static slippage applies correctly",
			side:       shared.SideOpenLong,
			bestBid:    1000,
			bestAsk:    1000,
			priceUnit:  0.01,
			maxDiffPct: 0.1, // 0.1% → slippage = max(1000*0.1/100, 0.02) = 1.0
			priceScale: 2,
			wantMin:    1000.01, // at least above bestAsk
			wantMax:    1002.0,
		},
		{
			name:       "SHORT static slippage applies correctly",
			side:       shared.SideOpenShort,
			bestBid:    1000,
			bestAsk:    1001,
			priceUnit:  0.01,
			maxDiffPct: 0.1,
			priceScale: 2,
			wantMin:    998.0,
			wantMax:    999.99, // at least below bestBid
		},
		{
			name:       "2-tick minimum enforced when percentage is tiny",
			side:       shared.SideOpenLong,
			bestBid:    10,
			bestAsk:    10,
			priceUnit:  0.01,
			maxDiffPct: 0.0001, // tiny → 2-tick minimum = 0.02
			priceScale: 4,
			wantMin:    10.01,
			wantMax:    10.03,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := &domain.Candidate{
				Side:       tt.side,
				BestBid:    tt.bestBid,
				BestAsk:    tt.bestAsk,
				PriceUnit:  tt.priceUnit,
				PriceScale: tt.priceScale,
				Config: domain.TradeConfig{
					MaxPriceDiffPercent: tt.maxDiffPct,
				},
			}
			got, err := c.CalculateIOCPrice()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("got %v, want in [%v, %v]", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}
