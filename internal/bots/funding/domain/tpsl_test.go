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

//nolint:dupl // TP and SL tests are structurally identical by design.
func TestCalculateTrapTPPrice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		side      shared.Side
		trapPrice float64
		tpPct     float64
		priceUnit float64
		scale     int
		wantZero  bool
		wantPrice float64
	}{
		{
			name:      "LONG TRAP TP — above trap, snapped floor",
			side:      shared.SideOpenLong,
			trapPrice: 100.0,
			tpPct:     0.02, // 2% -> 102.0
			priceUnit: 0.1,
			scale:     1,
			wantPrice: 102.0,
		},
		{
			name:      "SHORT TRAP TP — below trap, snapped ceil",
			side:      shared.SideOpenShort,
			trapPrice: 100.0,
			tpPct:     0.02, // 2% -> 98.0
			priceUnit: 0.1,
			scale:     1,
			wantPrice: 98.0,
		},
		{
			name:      "Invalid trap price — return 0",
			side:      shared.SideOpenLong,
			trapPrice: 0.0,
			tpPct:     0.02,
			priceUnit: 0.1,
			wantZero:  true,
		},
		{
			name:      "TP Pct zero — return 0",
			side:      shared.SideOpenLong,
			trapPrice: 100.0,
			tpPct:     0.0,
			priceUnit: 0.1,
			wantZero:  true,
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
					FundingTrap: domain.FundingTrapConfig{
						TakeProfitPct: tt.tpPct,
					},
				},
			}
			got := c.CalculateTrapTPPrice(tt.trapPrice)
			if tt.wantZero {
				assert.Zero(t, got)
			} else {
				assert.InDelta(t, tt.wantPrice, got, 0.001)
			}
		})
	}
}

//nolint:dupl // TP and SL tests are structurally identical by design.
func TestCalculateTrapSLPrice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		side      shared.Side
		trapPrice float64
		slPct     float64
		priceUnit float64
		scale     int
		wantZero  bool
		wantPrice float64
	}{
		{
			name:      "LONG TRAP SL — below trap, snapped ceil",
			side:      shared.SideOpenLong,
			trapPrice: 100.0,
			slPct:     0.02, // 2% -> 98.0
			priceUnit: 0.1,
			scale:     1,
			wantPrice: 98.0,
		},
		{
			name:      "SHORT TRAP SL — above trap, snapped floor",
			side:      shared.SideOpenShort,
			trapPrice: 100.0,
			slPct:     0.02, // 2% -> 102.0
			priceUnit: 0.1,
			scale:     1,
			wantPrice: 102.0,
		},
		{
			name:      "Invalid trap price — return 0",
			side:      shared.SideOpenLong,
			trapPrice: 0.0,
			slPct:     0.02,
			priceUnit: 0.1,
			wantZero:  true,
		},
		{
			name:      "SL Pct zero — return 0",
			side:      shared.SideOpenLong,
			trapPrice: 100.0,
			slPct:     0.0,
			priceUnit: 0.1,
			wantZero:  true,
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
					FundingTrap: domain.FundingTrapConfig{
						StopLossPct: tt.slPct,
					},
				},
			}
			got := c.CalculateTrapSLPrice(tt.trapPrice)
			if tt.wantZero {
				assert.Zero(t, got)
			} else {
				assert.InDelta(t, tt.wantPrice, got, 0.001)
			}
		})
	}
}

func TestFindTrapWallPrice(t *testing.T) {
	t.Parallel()

	// FindTrapWallPrice looks at the opposite side from the sniper entry.
	// IOC LONG -> Trap SHORT -> look for ASK wall.
	// IOC SHORT -> Trap LONG -> look for BID wall.

	tests := []struct {
		name      string
		side      shared.Side
		bestBid   float64
		bestAsk   float64
		ob        *shared.OrderBook
		wantPrice float64
	}{
		{
			name:    "LONG sniper -> ASK wall",
			side:    shared.SideOpenLong,
			bestBid: 17.90,
			bestAsk: 18.00, // IOC buys at 18.00, pumps up
			ob: &shared.OrderBook{
				Asks: []shared.OrderBookEntry{
					{Price: 18.02, Volume: 10},
					{Price: 18.05, Volume: 10},
					{Price: 18.10, Volume: 10},
					{Price: 18.15, Volume: 200}, // Wall
				},
			},
			wantPrice: 18.15,
		},
		{
			name:    "SHORT sniper -> BID wall",
			side:    shared.SideOpenShort,
			bestBid: 18.00, // IOC sells at 18.00, dumps down
			bestAsk: 18.10,
			ob: &shared.OrderBook{
				Bids: []shared.OrderBookEntry{
					{Price: 17.98, Volume: 10},
					{Price: 17.95, Volume: 10},
					{Price: 17.90, Volume: 10},
					{Price: 17.85, Volume: 200}, // Wall
				},
			},
			wantPrice: 17.85,
		},
		{
			name:    "Invalid entry ref price -> return 0",
			side:    shared.SideOpenShort,
			bestBid: 0, // entry = 0 -> FindWallPrice returns 0
			bestAsk: 18.10,
			ob: &shared.OrderBook{
				Bids: []shared.OrderBookEntry{
					{Price: 17.98, Volume: 10},
					{Price: 17.95, Volume: 10},
					{Price: 17.90, Volume: 10},
					{Price: 17.85, Volume: 200}, // Wall
				},
			},
			wantPrice: 0,
		},
		{
			name:    "Invalid side -> no wall",
			side:    shared.Side(99),
			bestBid: 18.00,
			bestAsk: 18.10,
			ob: &shared.OrderBook{
				Bids: []shared.OrderBookEntry{
					{Price: 17.85, Volume: 200},
				},
			},
			wantPrice: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := domain.Candidate{
				TradeIntent: domain.TradeIntent{Side: tt.side},
				MarketData: domain.MarketData{
					BestBid: tt.bestBid,
					BestAsk: tt.bestAsk,
				},
			}
			got := c.FindTrapWallPrice(tt.ob)
			assert.InDelta(t, tt.wantPrice, got, 0.001)
		})
	}
}

func TestCalculateOBTrapPrice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		side      shared.Side
		wallPrice float64
		priceUnit float64
		wantPrice float64
	}{
		{
			name:      "LONG sniper -> Trap SHORT (sell) -> slightly lower than ask wall",
			side:      shared.SideOpenLong,
			wallPrice: 18.15,
			priceUnit: 0.01,
			wantPrice: 18.14,
		},
		{
			name:      "SHORT sniper -> Trap LONG (buy) -> slightly higher than bid wall",
			side:      shared.SideOpenShort,
			wallPrice: 17.85,
			priceUnit: 0.01,
			wantPrice: 17.86,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := domain.Candidate{
				TradeIntent:  domain.TradeIntent{Side: tt.side},
				ContractSpec: domain.ContractSpec{PriceUnit: tt.priceUnit},
			}
			got := c.CalculateOBTrapPrice(tt.wallPrice)
			assert.InDelta(t, tt.wantPrice, got, 0.001)
		})
	}
}

func TestTrapWallDistancePct(t *testing.T) {
	t.Parallel()

	c := domain.Candidate{
		MarketData: domain.MarketData{LastPrice: 100},
	}

	assert.InDelta(t, 5.0, c.TrapWallDistancePct(105), 1e-9)
	assert.Zero(t, c.TrapWallDistancePct(0))
}
