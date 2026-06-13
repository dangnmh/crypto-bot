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
				TradeIntent: domain.TradeIntent{Side: tt.side},
				ContractSpec: domain.ContractSpec{
					PriceUnit:  tt.priceUnit,
					PriceScale: tt.scale,
				},
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
				TradeIntent: domain.TradeIntent{Side: tt.side},
				ContractSpec: domain.ContractSpec{
					PriceUnit:  tt.priceUnit,
					PriceScale: tt.scale,
				},
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
