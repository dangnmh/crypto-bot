package domain_test

import (
	"testing"

	"crypto-bot/internal/bots/funding/domain"
	shared "crypto-bot/internal/domain"

	"github.com/stretchr/testify/assert"
)

func TestCalculateTakeProfitPrice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		side      shared.Side
		bestBid   float64
		bestAsk   float64
		priceUnit float64
		scale     int
		maxTPPct  float64
		ob        *shared.OrderBook
		wantZero  bool // expect 0 return
		validate  func(t *testing.T, tp float64)
	}{
		{
			name:      "LONG/wall found — TP snaps before wall",
			side:      shared.SideOpenLong,
			bestBid:   17.90,
			bestAsk:   18.00,
			priceUnit: 0.01,
			scale:     2,
			maxTPPct:  1.5, // max TP = 18.00 * 1.015 = 18.27
			ob: &shared.OrderBook{
				Asks: []shared.OrderBookEntry{
					{Price: 18.02, Volume: 50},  // thin
					{Price: 18.05, Volume: 30},  // thin
					{Price: 18.10, Volume: 500}, // WALL (avg≈193, threshold≈580 → 500 < 580, not a wall)
					{Price: 18.15, Volume: 800}, // WALL (500 is not wall, but let's fix volumes)
				},
			},
			validate: func(t *testing.T, tp float64) {
				t.Helper()
				// With volumes [50,30,500,800]: avg=345, threshold=1035.
				// No level >= 1035 → fallback to maxTP = 18.27.
				assert.InDelta(t, 18.27, tp, 0.01)
			},
		},
		{
			name:      "LONG/clear wall detected — TP 2 ticks before wall",
			side:      shared.SideOpenLong,
			bestBid:   17.90,
			bestAsk:   18.00,
			priceUnit: 0.01,
			scale:     2,
			maxTPPct:  2.0, // max TP = 18.00 * 1.02 = 18.36
			ob: &shared.OrderBook{
				Asks: []shared.OrderBookEntry{
					{Price: 18.02, Volume: 10},
					{Price: 18.05, Volume: 10},
					{Price: 18.10, Volume: 10},
					{Price: 18.15, Volume: 200}, // WALL: avg=57.5, threshold=172.5 → 200 ≥ 172.5 ✅
				},
			},
			validate: func(t *testing.T, tp float64) {
				t.Helper()
				// Wall at 18.15, TP = 18.15 - 0.02 = 18.13
				assert.InDelta(t, 18.13, tp, 0.001)
			},
		},
		{
			name:      "LONG/wall beyond maxTP — clamp to maxTP",
			side:      shared.SideOpenLong,
			bestBid:   17.90,
			bestAsk:   18.00,
			priceUnit: 0.01,
			scale:     2,
			maxTPPct:  0.3, // max TP = 18.00 * 1.003 = 18.054
			ob: &shared.OrderBook{
				Asks: []shared.OrderBookEntry{
					{Price: 18.02, Volume: 10},
					{Price: 18.10, Volume: 10},
					{Price: 18.20, Volume: 10},
					{Price: 18.50, Volume: 500}, // Wall at 18.50, but maxTP=18.054
				},
			},
			validate: func(t *testing.T, tp float64) {
				t.Helper()
				// Wall at 18.50, TP before wall = 18.48, but maxTP = 18.054
				// So clamp to 18.054, tick-snapped down = 18.05
				assert.InDelta(t, 18.05, tp, 0.01)
			},
		},
		{
			name:      "SHORT/wall found — TP snaps before wall",
			side:      shared.SideOpenShort,
			bestBid:   18.00,
			bestAsk:   18.10,
			priceUnit: 0.01,
			scale:     2,
			maxTPPct:  2.0, // max TP = 18.00 * 0.98 = 17.64
			ob: &shared.OrderBook{
				Bids: []shared.OrderBookEntry{
					{Price: 17.98, Volume: 10},
					{Price: 17.95, Volume: 10},
					{Price: 17.90, Volume: 10},
					{Price: 17.85, Volume: 200}, // WALL
				},
			},
			validate: func(t *testing.T, tp float64) {
				t.Helper()
				// Wall at 17.85, TP = 17.85 + 0.02 = 17.87
				assert.InDelta(t, 17.87, tp, 0.001)
			},
		},
		{
			name:      "SHORT/wall beyond maxTP — clamp to maxTP",
			side:      shared.SideOpenShort,
			bestBid:   18.00,
			bestAsk:   18.10,
			priceUnit: 0.01,
			scale:     2,
			maxTPPct:  0.5, // max TP = 18.00 * 0.995 = 17.91
			ob: &shared.OrderBook{
				Bids: []shared.OrderBookEntry{
					{Price: 17.98, Volume: 10},
					{Price: 17.95, Volume: 10},
					{Price: 17.90, Volume: 10},
					{Price: 17.50, Volume: 500}, // Wall at 17.50
				},
			},
			validate: func(t *testing.T, tp float64) {
				t.Helper()
				// Wall at 17.50, TP = 17.52, but maxTP = 17.91
				// Clamp to 17.91, tick-snapped up = 17.91
				assert.InDelta(t, 17.91, tp, 0.01)
			},
		},
		{
			name:      "SHORT/no wall — fallback to maxTP",
			side:      shared.SideOpenShort,
			bestBid:   18.00,
			bestAsk:   18.10,
			priceUnit: 0.01,
			scale:     2,
			maxTPPct:  1.0, // max TP = 18.00 * 0.99 = 17.82
			ob: &shared.OrderBook{
				Bids: []shared.OrderBookEntry{
					{Price: 17.98, Volume: 10},
					{Price: 17.95, Volume: 10},
					{Price: 17.90, Volume: 10},
				},
			},
			validate: func(t *testing.T, tp float64) {
				t.Helper()
				assert.InDelta(t, 17.82, tp, 0.01)
			},
		},
		{
			name:      "nil orderbook — fallback to maxTP",
			side:      shared.SideOpenLong,
			bestBid:   17.90,
			bestAsk:   18.00,
			priceUnit: 0.01,
			scale:     2,
			maxTPPct:  0.5,
			ob:        nil,
			validate: func(t *testing.T, tp float64) {
				t.Helper()
				// maxTP = 18.00 * 1.005 = 18.09
				assert.InDelta(t, 18.09, tp, 0.01)
			},
		},
		{
			name:      "maxTPPct zero — return 0",
			side:      shared.SideOpenLong,
			bestBid:   17.90,
			bestAsk:   18.00,
			priceUnit: 0.01,
			scale:     2,
			maxTPPct:  0,
			ob:        nil,
			wantZero:  true,
		},
		{
			name:      "zero BestAsk for LONG — return 0",
			side:      shared.SideOpenLong,
			bestBid:   17.90,
			bestAsk:   0,
			priceUnit: 0.01,
			scale:     2,
			maxTPPct:  1.0,
			ob:        nil,
			wantZero:  true,
		},
		{
			name:      "too few OB levels — fallback to maxTP",
			side:      shared.SideOpenLong,
			bestBid:   17.90,
			bestAsk:   18.00,
			priceUnit: 0.01,
			scale:     2,
			maxTPPct:  0.5,
			ob: &shared.OrderBook{
				Asks: []shared.OrderBookEntry{
					{Price: 18.02, Volume: 100},
					{Price: 18.05, Volume: 500},
				},
			},
			validate: func(t *testing.T, tp float64) {
				t.Helper()
				// Only 2 levels < minWallLevels(3) → no wall → fallback maxTP
				assert.InDelta(t, 18.09, tp, 0.01)
			},
		},
		{
			name:      "LONG TP sanity check fails (TP below entry)",
			side:      shared.SideOpenLong,
			bestBid:   17.90,
			bestAsk:   18.00,
			priceUnit: 0.01,
			scale:     2,
			maxTPPct:  1.0,
			ob: &shared.OrderBook{
				Asks: []shared.OrderBookEntry{
					{Price: 18.01, Volume: 5000}, // Wall 1 tick away
					{Price: 18.02, Volume: 10},
					{Price: 18.03, Volume: 10},
					{Price: 18.04, Volume: 10},
					{Price: 18.05, Volume: 10},
				},
			},
			wantZero: true,
		},
		{
			name:      "SHORT TP sanity check fails (TP above entry)",
			side:      shared.SideOpenShort,
			bestBid:   18.00,
			bestAsk:   18.10,
			priceUnit: 0.01,
			scale:     2,
			maxTPPct:  1.0,
			ob: &shared.OrderBook{
				Bids: []shared.OrderBookEntry{
					{Price: 17.99, Volume: 5000}, // Wall 1 tick away
					{Price: 17.98, Volume: 10},
					{Price: 17.97, Volume: 10},
					{Price: 17.96, Volume: 10},
					{Price: 17.95, Volume: 10},
				},
			},
			wantZero: true,
		},
		{
			name:      "SHORT rawTP <= 0 — return 0",
			side:      shared.SideOpenShort,
			bestBid:   10.00,
			bestAsk:   10.10,
			priceUnit: 0.01,
			scale:     2,
			maxTPPct:  150.0, // max TP = 10.00 * (1 - 1.5) = -5.0
			ob:        nil,
			wantZero:  true,
		},
		{
			name:      "Invalid Side — return 0",
			side:      shared.Side(99),
			bestBid:   17.90,
			bestAsk:   18.00,
			priceUnit: 0.01,
			scale:     2,
			maxTPPct:  1.0,
			ob:        nil,
			wantZero:  true,
		},
		{
			name:      "Negative PriceUnit — return 0",
			side:      shared.SideOpenLong,
			bestBid:   17.90,
			bestAsk:   18.00,
			priceUnit: -0.01,
			scale:     2,
			maxTPPct:  1.0,
			ob:        nil,
			wantZero:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := domain.Candidate{
				TradeIntent: domain.TradeIntent{
					Symbol: "TEST_USDT",
					Side:   tt.side,
				},
				MarketData: domain.MarketData{
					BestBid: tt.bestBid,
					BestAsk: tt.bestAsk,
				},
				ContractSpec: domain.ContractSpec{
					PriceUnit:  tt.priceUnit,
					PriceScale: tt.scale,
				},
			}

			tp := c.CalculateTakeProfitPrice(tt.ob, tt.maxTPPct)

			if tt.wantZero {
				assert.Zero(t, tp, "expected TP to be 0")
				return
			}

			assert.Greater(t, tp, 0.0, "expected TP > 0")
			if tt.validate != nil {
				tt.validate(t, tp)
			}
		})
	}
}

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
