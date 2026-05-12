package domain_test

import (
	"math"
	"testing"

	domain "crypto-bot/internal/bots/funding_reversion/domain"

	shared "crypto-bot/internal/domain"
	"crypto-bot/pkg/decmath"
)

func almostEqual(a, b, eps float64) bool {
	return math.Abs(a-b) < eps
}

func TestCalculateIOCPrice(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                string
		side                shared.Side
		bestBid             float64
		bestAsk             float64
		priceUnit           float64
		maxPriceDiffPercent float64
		priceScale          int
		wantPrice           float64
		wantErr             bool
	}{
		{
			name:                "LONG basic",
			side:                shared.SideOpenLong,
			bestBid:             65000,
			bestAsk:             65010,
			priceUnit:           0.1,
			maxPriceDiffPercent: 0.002,
			priceScale:          1,
			wantPrice:           65011.3, // 65010 + max(65010*0.002/100, 0.2) = 65010 + 1.302 → floor → 65011.3
		},
		{
			name:                "SHORT basic",
			side:                shared.SideOpenShort,
			bestBid:             65000,
			bestAsk:             65010,
			priceUnit:           0.1,
			maxPriceDiffPercent: 0.002,
			priceScale:          1,
			wantPrice:           64998.7, // 65000 - max(65000*0.002/100, 0.2) = 65000 - 1.3 → ceil → 64998.7
		},
		{
			name:                "min 2-tick buffer enforced",
			side:                shared.SideOpenLong,
			bestBid:             10,
			bestAsk:             10,
			priceUnit:           0.01,
			maxPriceDiffPercent: 0.0001,
			priceScale:          4,
			wantPrice:           10.02,
		},
		{
			name:                "PriceScale cleans float artifacts",
			side:                shared.SideOpenLong,
			bestBid:             0.4070,
			bestAsk:             0.4074,
			priceUnit:           0.0001,
			maxPriceDiffPercent: 0.002,
			priceScale:          4,
			wantPrice:           0.4082,
		},
		{
			name:                "invalid side",
			side:                99,
			bestBid:             100,
			bestAsk:             101,
			priceUnit:           0.01,
			maxPriceDiffPercent: 0.002,
			priceScale:          2,
			wantErr:             true,
		},
		{
			name:                "zero bestAsk for LONG",
			side:                shared.SideOpenLong,
			bestBid:             100,
			bestAsk:             0,
			priceUnit:           0.01,
			maxPriceDiffPercent: 0.002,
			priceScale:          2,
			wantErr:             true,
		},
		{
			name:                "zero priceUnit",
			side:                shared.SideOpenLong,
			bestBid:             100,
			bestAsk:             101,
			priceUnit:           0,
			maxPriceDiffPercent: 0.002,
			priceScale:          2,
			wantErr:             true,
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
				Config: domain.TradeConfig{MaxPriceDiffPercent: tt.maxPriceDiffPercent},
			}
			got, err := c.CalculateIOCPrice(nil)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !almostEqual(got, tt.wantPrice, 0.01) {
				t.Errorf("got %.6f, want %.6f", got, tt.wantPrice)
			}
		})
	}
}

func TestCalculateVolume(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		marginUSDT   float64
		leverage     int
		contractSize float64
		lastPrice    float64
		minVol       float64
		volScale     int
		want         float64
	}{
		{
			name:         "basic BTC",
			marginUSDT:   10,
			leverage:     20,
			contractSize: 0.0001,
			lastPrice:    65000,
			minVol:       1,
			volScale:     0,
			want:         30, // 200 / 6.5 = 30.769 → floor → 30
		},
		{
			name:         "clamps to minVol",
			marginUSDT:   1,
			leverage:     1,
			contractSize: 1.0,
			lastPrice:    50000,
			minVol:       5,
			volScale:     0,
			want:         5, // 1 / 50000 = 0.00002 → floor → 0 → clamped to 5
		},
		{
			name:         "VolScale=2 fractional lots",
			marginUSDT:   100,
			leverage:     10,
			contractSize: 1.0,
			lastPrice:    3.5,
			minVol:       1,
			volScale:     2,
			want:         285.71, // 1000 / 3.5 = 285.714... → floor to 2dp → 285.71
		},
		{
			name:         "zero contractSize",
			marginUSDT:   10,
			leverage:     20,
			contractSize: 0,
			lastPrice:    100,
			minVol:       1,
			want:         0,
		},
		{
			name:         "zero lastPrice",
			marginUSDT:   10,
			leverage:     20,
			contractSize: 0.0001,
			lastPrice:    0,
			minVol:       1,
			want:         0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := &domain.Candidate{
				Config: domain.TradeConfig{MarginUSDT: tt.marginUSDT, Leverage: tt.leverage},
				ContractSpec: domain.ContractSpec{
					ContractSize: tt.contractSize,
					MinVol:       int(tt.minVol),
					VolScale:     tt.volScale,
				},
				MarketData: domain.MarketData{
					LastPrice: tt.lastPrice,
				},
			}
			got := c.CalculateVolume()
			if !almostEqual(got, tt.want, 0.001) {
				t.Errorf("got %.4f, want %.4f", got, tt.want)
			}
		})
	}
}

func TestRoundToScale(t *testing.T) {
	t.Parallel()
	tests := []struct {
		v    float64
		n    int
		want float64
	}{
		{1.23456, 2, 1.23},
		{1.235, 2, 1.24},
		{1.2, 0, 1},
		{0.4074, 4, 0.4074},
		{100.0, 0, 100},
	}
	for _, tt := range tests {
		if got := decmath.RoundToScale(tt.v, tt.n); !almostEqual(got, tt.want, 1e-10) {
			t.Errorf("decmath.RoundToScale(%v, %d) = %v, want %v", tt.v, tt.n, got, tt.want)
		}
	}
}

func TestFloorToScale(t *testing.T) {
	t.Parallel()
	tests := []struct {
		v    float64
		n    int
		want float64
	}{
		{1.23999, 2, 1.23},
		{1.239, 2, 1.23},
		{285.714285, 2, 285.71},
		{30.999, 0, 30},
		{0.12345, 4, 0.1234},
	}
	for _, tt := range tests {
		if got := decmath.FloorToScale(tt.v, tt.n); !almostEqual(got, tt.want, 1e-10) {
			t.Errorf("decmath.FloorToScale(%v, %d) = %v, want %v", tt.v, tt.n, got, tt.want)
		}
	}
}

func TestCalculateTrapPrice(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		side         shared.Side
		peakPrice    float64
		trapDepthPct float64
		priceScale   int
		priceUnit    float64
		wantPrice    float64
	}{
		{
			name:         "Sniper SHORT -> Trap LONG (Buy Dump) - Real Bug Scenario",
			side:         shared.SideOpenShort,
			peakPrice:    0.1465,
			trapDepthPct: 0.05, // 5%
			priceScale:   4,
			priceUnit:    0.0001,
			wantPrice:    0.1391, // 0.1465 * 0.95 = 0.139175 -> Floor -> 0.1391
		},
		{
			name:         "Sniper LONG -> Trap SHORT (Sell Pump)",
			side:         shared.SideOpenLong,
			peakPrice:    65000,
			trapDepthPct: 0.02, // 2%
			priceScale:   1,
			priceUnit:    0.1,
			wantPrice:    66300.0, // 65000 * 1.02 = 66300 -> Ceil -> 66300.0
		},
		{
			name:         "Sniper SHORT -> Trap LONG (Buy Dump) with Fractional Snapping",
			side:         shared.SideOpenShort,
			peakPrice:    0.00001465,
			trapDepthPct: 0.05,
			priceScale:   8,
			priceUnit:    0.00000001, // 8 decimals
			wantPrice:    0.00001391,
		},
		{
			name:         "Sniper LONG -> Trap SHORT (Sell Pump) with Fractional Snapping",
			side:         shared.SideOpenLong,
			peakPrice:    0.00001465,
			trapDepthPct: 0.05,
			priceScale:   8,
			priceUnit:    0.00000001,
			wantPrice:    0.00001539,
		},
		{
			name:      "Invalid Side",
			side:      999, // Invalid
			wantPrice: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := &domain.Candidate{
				TradeIntent: domain.TradeIntent{Side: tt.side},
				MarketData: domain.MarketData{
					BestBid: tt.peakPrice,
					BestAsk: tt.peakPrice,
				},
				ContractSpec: domain.ContractSpec{
					PriceUnit:  tt.priceUnit,
					PriceScale: tt.priceScale,
				},
				Config: domain.TradeConfig{FundingTrap: domain.FundingTrapConfig{DepthPct: tt.trapDepthPct}},
			}
			got := c.CalculateTrapPrice()
			if !almostEqual(got, tt.wantPrice, 1e-10) {
				t.Errorf("got %.8f, want %.8f", got, tt.wantPrice)
			}
		})
	}
}

func TestGetPeakPrice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		side    shared.Side
		bestBid float64
		bestAsk float64
		want    float64
	}{
		{"LONG returns bestAsk", shared.SideOpenLong, 100, 101, 101},
		{"SHORT returns bestBid", shared.SideOpenShort, 100, 101, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := &domain.Candidate{
				TradeIntent: domain.TradeIntent{Side: tt.side},
				MarketData:  domain.MarketData{BestBid: tt.bestBid, BestAsk: tt.bestAsk},
			}
			if got := c.GetPeakPrice(); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCalculateVolume_UsesRefPrice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		side      shared.Side
		lastPrice float64
		bestBid   float64
		bestAsk   float64
		wantPrice float64 // the ref price used
	}{
		{
			name:      "LONG uses BestAsk when available",
			side:      shared.SideOpenLong,
			lastPrice: 100,
			bestBid:   99,
			bestAsk:   102,
			wantPrice: 102,
		},
		{
			name:      "SHORT uses BestBid when available",
			side:      shared.SideOpenShort,
			lastPrice: 100,
			bestBid:   98,
			bestAsk:   102,
			wantPrice: 98,
		},
		{
			name:      "LONG falls back to LastPrice when BestAsk is 0",
			side:      shared.SideOpenLong,
			lastPrice: 100,
			bestBid:   99,
			bestAsk:   0,
			wantPrice: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := &domain.Candidate{
				TradeIntent: domain.TradeIntent{Side: tt.side},
				Config:      domain.TradeConfig{MarginUSDT: 100, Leverage: 10},
				ContractSpec: domain.ContractSpec{
					ContractSize: 1.0,
					MinVol:       1,
					VolScale:     2,
				},
				MarketData: domain.MarketData{
					LastPrice: tt.lastPrice,
					BestBid:   tt.bestBid,
					BestAsk:   tt.bestAsk,
				},
			}
			got := c.CalculateVolume()
			// notional = 100*10 = 1000, vol = 1000 / (1.0 * refPrice)
			expectedVol := decmath.FloorToScale(1000.0/tt.wantPrice, 2)
			if !almostEqual(got, expectedVol, 0.01) {
				t.Errorf("got %v, want %v (ref price %v)", got, expectedVol, tt.wantPrice)
			}
		})
	}
}

func TestPrepareDynamicPricing(t *testing.T) {
	t.Parallel()

	t.Run("disabled does nothing", func(t *testing.T) {
		t.Parallel()
		c := &domain.Candidate{
			Config: domain.TradeConfig{
				FundingReversion: domain.FundingReversionConfig{
					TakeProfitPct: 0.2,
					StopLossPct:   0.05,
					DynamicPricing: domain.DynamicPricingConfig{
						Enabled: false,
					},
				},
			},
		}
		c.PrepareDynamicPricing()
		if c.Config.FundingReversion.TakeProfitPct != 0.2 {
			t.Errorf("TP changed: got %v", c.Config.FundingReversion.TakeProfitPct)
		}
	})

	t.Run("calculates TP and SL from ATR and FR", func(t *testing.T) {
		t.Parallel()
		c := &domain.Candidate{
			TradeIntent: domain.TradeIntent{FundingRate: 0.001}, // 0.1%
			MarketData:  domain.MarketData{LastPrice: 100},
			TradePlan:   domain.TradePlan{ATR: 2.0}, // 2% ATR
			Config: domain.TradeConfig{
				FundingReversion: domain.FundingReversionConfig{
					TakeProfitPct: 0.01,
					StopLossPct:   0.01,
					DynamicPricing: domain.DynamicPricingConfig{
						Enabled:             true,
						TpFundingMultiplier: 10.0,
						TpAtrMultiplier:     2.0,
						SlFundingMultiplier: 5.0,
						SlAtrMultiplier:     1.5,
					},
				},
			},
		}
		c.PrepareDynamicPricing()

		// frPct = 0.1, atrPct = 2.0
		// tp = 0.1*10 + 2.0*2 = 5.0
		// sl = max(0.1*5, 2.0*1.5) = max(0.5, 3.0) = 3.0
		if c.Config.FundingReversion.TakeProfitPct != 5.0 {
			t.Errorf("TP: got %v, want 5.0", c.Config.FundingReversion.TakeProfitPct)
		}
		if c.Config.FundingReversion.StopLossPct != 3.0 {
			t.Errorf("SL: got %v, want 3.0", c.Config.FundingReversion.StopLossPct)
		}
	})

	t.Run("calculates trap params when hedge trap enabled", func(t *testing.T) {
		t.Parallel()
		c := &domain.Candidate{
			TradeIntent: domain.TradeIntent{FundingRate: -0.002}, // -0.2%
			MarketData:  domain.MarketData{LastPrice: 50},
			TradePlan:   domain.TradePlan{ATR: 1.0},
			Config: domain.TradeConfig{
				FundingTrap: domain.FundingTrapConfig{
					Enabled:         true,
					DepthMultiplier: 4.0,
					MinDepth:        1.5,
					MaxDepth:        6.0,
					TpMultiplier:    2.5,
					MinTP:           1.0,
					MaxTP:           5.0,
					SlMultiplier:    2.0,
					MinSL:           1.0,
					MaxSL:           4.0,
				},
				FundingReversion: domain.FundingReversionConfig{
					DynamicPricing: domain.DynamicPricingConfig{
						Enabled:             true,
						TpFundingMultiplier: 5.0,
						TpAtrMultiplier:     1.0,
						SlFundingMultiplier: 2.0,
						SlAtrMultiplier:     1.0,
					},
				},
			},
		}
		c.PrepareDynamicPricing()

		// frPct = 0.2
		// Trap.DepthPct = ClampPct(0.2*4.0, 1.5, 6.0) = ClampPct(0.8, 1.5, 6.0) = 1.5/100 = 0.015
		if !almostEqual(c.Config.FundingTrap.DepthPct, 0.015, 1e-6) {
			t.Errorf("Trap.DepthPct: got %v, want 0.015", c.Config.FundingTrap.DepthPct)
		}
	})

	t.Run("calculates trailing params", func(t *testing.T) {
		t.Parallel()
		c := &domain.Candidate{
			TradeIntent: domain.TradeIntent{FundingRate: 0.005}, // 0.5%
			MarketData:  domain.MarketData{LastPrice: 100},
			TradePlan:   domain.TradePlan{ATR: 3.0},
			Config: domain.TradeConfig{
				FundingReversion: domain.FundingReversionConfig{
					Trailing: domain.TrailingConfig{
						Enabled:              true,
						ActivationMultiplier: 1.5,
						MinActivation:        0.2,
						MaxActivation:        3.0,
						CallbackMultiplier:   0.7,
						MinCallback:          0.3,
						MaxCallback:          1.5,
					},
					DynamicPricing: domain.DynamicPricingConfig{
						Enabled:             true,
						TpFundingMultiplier: 5.0,
						TpAtrMultiplier:     1.0,
						SlFundingMultiplier: 2.0,
						SlAtrMultiplier:     1.0,
					},
				},
			},
		}
		c.PrepareDynamicPricing()

		// frPct = 0.5
		// ActivationPct = ClampPct(0.5*1.5, 0.2, 3.0) = ClampPct(0.75, 0.2, 3.0) = 0.75/100 = 0.0075
		if !almostEqual(c.Config.FundingReversion.Trailing.ActivationPct, 0.0075, 1e-6) {
			t.Errorf("ActivationPct: got %v, want 0.0075", c.Config.FundingReversion.Trailing.ActivationPct)
		}

		// CallbackPct = ClampPct(0.5*0.7, 0.3, 1.5) = ClampPct(0.35, 0.3, 1.5) = 0.35/100 = 0.0035
		if !almostEqual(c.Config.FundingReversion.Trailing.CallbackPct, 0.0035, 1e-6) {
			t.Errorf("CallbackPct: got %v, want 0.0035", c.Config.FundingReversion.Trailing.CallbackPct)
		}
	})
}
