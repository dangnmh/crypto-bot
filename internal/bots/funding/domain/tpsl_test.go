package domain_test

import (
	"testing"

	"crypto-bot/internal/bots/funding/domain"
	shared "crypto-bot/internal/domain"

	"github.com/stretchr/testify/assert"
)

func TestCalculateStopLossPrice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		side       shared.Side
		entryPrice float64
		slPct      float64
		priceUnit  float64
		scale      int
		wantZero   bool
		wantPrice  float64
	}{
		{
			name:       "LONG SL — below entry, snapped ceil",
			side:       shared.SideOpenLong,
			entryPrice: 100.0,
			slPct:      0.02, // 2%
			priceUnit:  0.1,
			scale:      1,
			wantPrice:  98.0, // 100 * (1 - 0.02)
		},
		{
			name:       "SHORT SL — above entry, snapped floor",
			side:       shared.SideOpenShort,
			entryPrice: 100.0,
			slPct:      0.02, // 2%
			priceUnit:  0.1,
			scale:      1,
			wantPrice:  102.0, // 100 * (1 + 0.02)
		},
		{
			name:       "LONG SL — snap ceil test",
			side:       shared.SideOpenLong,
			entryPrice: 100.0,
			slPct:      0.025, // 2.5% -> 97.5
			priceUnit:  0.2,   // Ticks: 97.4, 97.6
			scale:      1,
			wantPrice:  97.6, // Ceil to avoid early trigger
		},
		{
			name:       "SHORT SL — snap floor test",
			side:       shared.SideOpenShort,
			entryPrice: 100.0,
			slPct:      0.025, // 2.5% -> 102.5
			priceUnit:  0.2,   // Ticks: 102.4, 102.6
			scale:      1,
			wantPrice:  102.4, // Floor to avoid early trigger
		},
		{
			name:       "Invalid entry price — return 0",
			side:       shared.SideOpenLong,
			entryPrice: 0.0,
			slPct:      0.02,
			priceUnit:  0.1,
			wantZero:   true,
		},
		{
			name:       "SL Pct zero — return 0",
			side:       shared.SideOpenLong,
			entryPrice: 100.0,
			slPct:      0.0,
			priceUnit:  0.1,
			wantZero:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := domain.Candidate{
				Side:       tt.side,
				PriceUnit:  tt.priceUnit,
				PriceScale: tt.scale,
				Config: domain.TradeConfig{
					FundingReversion: domain.FundingReversionConfig{
						StopLossPct: tt.slPct,
					},
				},
			}
			got := c.CalculateStopLossPrice(tt.entryPrice)
			if tt.wantZero {
				assert.Zero(t, got)
			} else {
				assert.InDelta(t, tt.wantPrice, got, 0.001)
			}
		})
	}
}

//nolint:dupl // TP and SL tests are structurally identical by design.
func TestCalculateStaticTakeProfitPrice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		side       shared.Side
		entryPrice float64
		tpPct      float64
		priceUnit  float64
		scale      int
		wantZero   bool
		wantPrice  float64
	}{
		{
			name:       "LONG TP — above entry, snapped floor",
			side:       shared.SideOpenLong,
			entryPrice: 100.0,
			tpPct:      0.02,
			priceUnit:  0.1,
			scale:      1,
			wantPrice:  102.0,
		},
		{
			name:       "SHORT TP — below entry, snapped ceil",
			side:       shared.SideOpenShort,
			entryPrice: 100.0,
			tpPct:      0.02,
			priceUnit:  0.1,
			scale:      1,
			wantPrice:  98.0,
		},
		{
			name:       "Invalid entry price — return 0",
			side:       shared.SideOpenLong,
			entryPrice: 0.0,
			tpPct:      0.02,
			priceUnit:  0.1,
			wantZero:   true,
		},
		{
			name:       "TP Pct zero — return 0",
			side:       shared.SideOpenLong,
			entryPrice: 100.0,
			tpPct:      0.0,
			priceUnit:  0.1,
			wantZero:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := domain.Candidate{
				Side:       tt.side,
				PriceUnit:  tt.priceUnit,
				PriceScale: tt.scale,
				Config: domain.TradeConfig{
					FundingReversion: domain.FundingReversionConfig{
						TakeProfitPct: tt.tpPct,
					},
				},
			}
			got := c.CalculateStaticTakeProfitPrice(tt.entryPrice)
			if tt.wantZero {
				assert.Zero(t, got)
			} else {
				assert.InDelta(t, tt.wantPrice, got, 0.001)
			}
		})
	}
}

func TestResolveTakeProfitPct(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		fundingRate float64
		cfg         domain.FundingReversionConfig
		wantRatio   float64
	}{
		{
			name:        "Static fallback when dynamic TP disabled",
			fundingRate: -0.04, // -4%
			cfg: domain.FundingReversionConfig{
				TakeProfitPct: 0.01,
				DynamicTP: domain.DynamicTPConfig{
					Enabled:      false,
					TPMultiplier: 2.0,
				},
			},
			wantRatio: 0.01,
		},
		{
			name:        "Static fallback when TPMultiplier is zero",
			fundingRate: -0.04,
			cfg: domain.FundingReversionConfig{
				TakeProfitPct: 0.015,
				DynamicTP: domain.DynamicTPConfig{
					Enabled:      true,
					TPMultiplier: 0.0,
				},
			},
			wantRatio: 0.015,
		},
		{
			name:        "Dynamic scaling within bounds",
			fundingRate: -0.015, // -1.5%
			cfg: domain.FundingReversionConfig{
				TakeProfitPct: 0.01,
				DynamicTP: domain.DynamicTPConfig{
					Enabled:          true,
					TPMultiplier:     2.0,
					MinTakeProfitPct: 0.01, // 1%
					MaxTakeProfitPct: 0.07, // 7%
				},
			},
			wantRatio: 0.03, // 1.5% * 2.0 = 3%
		},
		{
			name:        "Dynamic scaling clamped to MinTakeProfitPct",
			fundingRate: -0.003, // -0.3%
			cfg: domain.FundingReversionConfig{
				TakeProfitPct: 0.01,
				DynamicTP: domain.DynamicTPConfig{
					Enabled:          true,
					TPMultiplier:     2.0,
					MinTakeProfitPct: 0.01, // 1%
					MaxTakeProfitPct: 0.07, // 7%
				},
			},
			wantRatio: 0.01, // 0.3% * 2.0 = 0.6% < 1% -> 1%
		},
		{
			name:        "Dynamic scaling clamped to MaxTakeProfitPct",
			fundingRate: -0.045, // -4.5%
			cfg: domain.FundingReversionConfig{
				TakeProfitPct: 0.01,
				DynamicTP: domain.DynamicTPConfig{
					Enabled:          true,
					TPMultiplier:     2.0,
					MinTakeProfitPct: 0.01, // 1%
					MaxTakeProfitPct: 0.07, // 7%
				},
			},
			wantRatio: 0.07, // 4.5% * 2.0 = 9% > 7% -> 7%
		},
		{
			name:        "Positive funding rate scaling",
			fundingRate: 0.02, // +2%
			cfg: domain.FundingReversionConfig{
				TakeProfitPct: 0.01,
				DynamicTP: domain.DynamicTPConfig{
					Enabled:          true,
					TPMultiplier:     1.8,
					MinTakeProfitPct: 0.01,
					MaxTakeProfitPct: 0.08,
				},
			},
			wantRatio: 0.036, // 2% * 1.8 = 3.6%
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := domain.Candidate{
				FundingRate: tt.fundingRate,
				Config: domain.TradeConfig{
					FundingReversion: tt.cfg,
				},
			}
			got := c.ResolveTakeProfitPct()
			assert.InDelta(t, tt.wantRatio, got, 1e-9)
		})
	}
}

func TestCalculateOrderTPSL_DynamicTP(t *testing.T) {
	t.Parallel()

	// Short trade with 2% negative funding rate:
	// Dynamic TP = 2% * 2.0 = 4.0%
	// Entry = 100.0, Short TP = 100 * (1 - 0.04) = 96.0
	c := domain.Candidate{
		PriceUnit:   0.1,
		PriceScale:  1,
		LastPrice:   100.0,
		BestBid:     100.0,
		BestAsk:     100.1,
		Side:        shared.SideOpenShort,
		FundingRate: -0.02,
		Config: domain.TradeConfig{
			FundingReversion: domain.FundingReversionConfig{
				TakeProfitPct: 0.01,
				DynamicTP: domain.DynamicTPConfig{
					Enabled:          true,
					TPMultiplier:     2.0,
					MinTakeProfitPct: 0.01,
					MaxTakeProfitPct: 0.07,
				},
				StopLossPct: 0.02,
			},
		},
	}

	tpPrice, slPrice := c.CalculateOrderTPSL(t.Context(), 100.0, nil)
	assert.InDelta(t, 96.0, tpPrice, 0.001)
	assert.InDelta(t, 102.0, slPrice, 0.001)
}
