package domain_test

import (
	"math"
	"testing"

	domain "crypto-bot/internal/bots/funding/domain"

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
			got, err := c.CalculateIOCPrice()

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

func TestVolumeAndNotionalInvalidInputs(t *testing.T) {
	t.Parallel()

	c := &domain.Candidate{
		ContractSpec: domain.ContractSpec{
			ContractSize: 1,
			MinVol:       2,
			VolScale:     0,
		},
		MarketData: domain.MarketData{LastPrice: 100},
	}

	assertZero := func(name string, got float64) {
		t.Helper()
		if got != 0 {
			t.Fatalf("%s = %.4f, want 0", name, got)
		}
	}

	assertZero("zero notional volume", c.CalculateVolumeForNotional(0, 100))
	assertZero("zero ref volume", c.CalculateVolumeForNotional(100, 0))
	assertZero("zero contract volume", (&domain.Candidate{}).CalculateVolumeForNotional(100, 100))
	assertZero("zero volume notional", c.NotionalForVolume(0, 100))
	assertZero("zero ref notional", c.NotionalForVolume(1, 0))
	assertZero("zero contract notional", (&domain.Candidate{}).NotionalForVolume(1, 100))

	got := c.CalculateVolumeForNotional(1, 100)
	if got != 2 {
		t.Fatalf("volume clamped to minVol = %.4f, want 2", got)
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

func TestCalculateVolumeMaxVolCapping(t *testing.T) {
	t.Parallel()

	c := &domain.Candidate{
		Config: domain.TradeConfig{MarginUSDT: 100, Leverage: 10},
		ContractSpec: domain.ContractSpec{
			ContractSize: 1.0,
			MinVol:       1,
			MaxVol:       5,
			VolScale:     0,
		},
		MarketData: domain.MarketData{
			LastPrice: 10.0,
		},
	}

	// Without MaxVol limit, vol would be 100 * 10 / 10 = 100.
	// With MaxVol = 5, it should be capped to 5.
	got := c.CalculateVolume()
	if got != 5 {
		t.Errorf("CalculateVolume got %.4f, want 5 (capped by MaxVol)", got)
	}

	// For CalculateVolumeForNotional, desired notional is 500, refPrice 10.
	// Vol would be 500 / 10 = 50. Capped to 5.
	gotNotional := c.CalculateVolumeForNotional(500, 10)
	if gotNotional != 5 {
		t.Errorf("CalculateVolumeForNotional got %.4f, want 5 (capped by MaxVol)", gotNotional)
	}
}
