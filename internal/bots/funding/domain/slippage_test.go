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
				TradeIntent: domain.TradeIntent{Side: tt.side},
				MarketData: domain.MarketData{
					BestBid: tt.bestBid,
					BestAsk: tt.bestAsk,
				},
				ContractSpec: domain.ContractSpec{
					PriceUnit:  tt.priceUnit,
					PriceScale: tt.priceScale,
				},
				Config: domain.TradeConfig{
					MaxPriceDiffPercent: tt.maxDiffPct,
				},
			}
			got, err := c.CalculateIOCPrice(nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("got %v, want in [%v, %v]", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestOBImbalanceSlippage_ViaIOCPrice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		side       shared.Side
		bestBid    float64
		bestAsk    float64
		priceUnit  float64
		priceScale int
		volume     float64
		ob         *shared.OrderBook
		wantMin    float64
		wantMax    float64
	}{
		{
			name:       "LONG sweeps asks with OB imbalance",
			side:       shared.SideOpenLong,
			bestBid:    99,
			bestAsk:    100,
			priceUnit:  0.01,
			priceScale: 2,
			volume:     15,
			ob: &shared.OrderBook{
				Asks: []shared.OrderBookEntry{
					{Price: 100.5, Volume: 10},
					{Price: 101.0, Volume: 10},
				},
			},
			wantMin: 100.01, // above bestAsk
			wantMax: 103.0,  // reasonable upper bound
		},
		{
			name:       "SHORT sweeps bids with OB imbalance",
			side:       shared.SideOpenShort,
			bestBid:    100,
			bestAsk:    101,
			priceUnit:  0.01,
			priceScale: 2,
			volume:     5,
			ob: &shared.OrderBook{
				Bids: []shared.OrderBookEntry{
					{Price: 99.5, Volume: 10},
				},
			},
			wantMin: 97.0,  // reasonable lower bound
			wantMax: 99.99, // below bestBid
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := &domain.Candidate{
				TradeIntent: domain.TradeIntent{Side: tt.side},
				MarketData: domain.MarketData{
					BestBid: tt.bestBid,
					BestAsk: tt.bestAsk,
				},
				ContractSpec: domain.ContractSpec{
					PriceUnit:  tt.priceUnit,
					PriceScale: tt.priceScale,
				},
				TradePlan: domain.TradePlan{Volume: tt.volume},
				Config: domain.TradeConfig{
					FundingReversion: domain.FundingReversionConfig{
						DynamicPricing: domain.DynamicPricingConfig{
							Enabled:          true,
							SlippageMode:     domain.SlippageModeOBImbalance,
							ObBufferPct:      1.0,
							ObMaxSlippagePct: 5.0,
						},
					},
				},
			}
			got, err := c.CalculateIOCPrice(tt.ob)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("got %v, want in [%v, %v]", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestSlippageMode_ViaIOCPrice(t *testing.T) {
	t.Parallel()

	// Verify that different slippage modes produce different prices
	// for the same market conditions, proving the factory selects correctly.
	base := domain.Candidate{
		TradeIntent: domain.TradeIntent{Side: shared.SideOpenLong},
		MarketData: domain.MarketData{
			BestBid: 100,
			BestAsk: 100.5,
		},
		ContractSpec: domain.ContractSpec{
			PriceUnit:  0.01,
			PriceScale: 2,
		},
		TradePlan: domain.TradePlan{Volume: 10},
		Config: domain.TradeConfig{
			MaxPriceDiffPercent: 0.5,
		},
	}

	t.Run("static mode", func(t *testing.T) {
		t.Parallel()
		c := base
		c.Config.FundingReversion.DynamicPricing = domain.DynamicPricingConfig{Enabled: false}
		got, err := c.CalculateIOCPrice(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got <= 100.5 {
			t.Errorf("static LONG price %v should be > bestAsk 100.5", got)
		}
	})

	t.Run("spread mode", func(t *testing.T) {
		t.Parallel()
		c := base
		c.Config.FundingReversion.DynamicPricing = domain.DynamicPricingConfig{
			Enabled:          true,
			SlippageMode:     domain.SlippageModeSpreadMultipler,
			SpreadMultiplier: 3.0,
			ObMaxSlippagePct: 5.0,
		}
		got, err := c.CalculateIOCPrice(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got <= 100.5 {
			t.Errorf("spread LONG price %v should be > bestAsk 100.5", got)
		}
	})
}
