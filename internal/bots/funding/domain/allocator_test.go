package domain_test

import (
	"context"
	"testing"

	"crypto-bot/internal/bots/funding/domain"

	"github.com/stretchr/testify/assert"
)

type mockRiskLimitClient struct {
	riskLimitLev int
	riskLimitErr error
}

func (m *mockRiskLimitClient) SupportLeverageOnOrder() bool {
	return false
}

func (m *mockRiskLimitClient) GetMaxLeverageForValue(ctx context.Context, symbol string, value float64) (int, error) {
	return m.riskLimitLev, m.riskLimitErr
}

func TestScoreMarginAllocator_AllocateMargins(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	allocator := domain.NewScoreMarginAllocator()

	t.Run("empty candidates", func(t *testing.T) {
		t.Parallel()
		res := allocator.AllocateMargins(ctx, nil, 100.0, 50.0, 5.0, nil, nil)
		assert.Nil(t, res)
	})

	t.Run("zero or negative margin pool", func(t *testing.T) {
		t.Parallel()
		candidates := []domain.Candidate{
			{Symbol: "BTCUSDT"},
		}
		resZero := allocator.AllocateMargins(ctx, candidates, 0, 50.0, 5.0, nil, nil)
		assert.Nil(t, resZero)

		resNeg := allocator.AllocateMargins(ctx, candidates, -10.0, 50.0, 5.0, nil, nil)
		assert.Nil(t, resNeg)
	})

	t.Run("sorts candidates ascending by 24h volume and allocates sequentially", func(t *testing.T) {
		t.Parallel()

		c1 := domain.Candidate{
			Config:    domain.TradeConfig{Leverage: 10},
			Symbol:    "HIGHVOL",
			Vol24USDT: 10_000_000, LastPrice: 1.0,
			ContractSize: 1.0, MaxLeverage: 10,
		}
		c2 := domain.Candidate{
			Config:    domain.TradeConfig{Leverage: 10},
			Symbol:    "LOWVOL",
			Vol24USDT: 1_000_000, LastPrice: 1.0,
			ContractSize: 1.0, MaxLeverage: 10,
		}

		candidates := []domain.Candidate{c1, c2}
		res := allocator.AllocateMargins(ctx, candidates, 1000.0, 500.0, 5.0, nil, nil)

		assert.Len(t, res, 2)
		assert.Equal(t, "HIGHVOL", res[0].Symbol)
		assert.Equal(t, "LOWVOL", res[1].Symbol)

		assert.Greater(t, res[0].Config.MarginUSDT, 0.0)
		assert.Greater(t, res[1].Config.MarginUSDT, 0.0)
		assert.LessOrEqual(t, res[0].Config.MarginUSDT, 500.0)
		assert.LessOrEqual(t, res[1].Config.MarginUSDT, 500.0)
	})

	t.Run("per candidate margin capping", func(t *testing.T) {
		t.Parallel()

		c := domain.Candidate{
			Config:    domain.TradeConfig{Leverage: 10},
			Symbol:    "BTCUSDT",
			Vol24USDT: 100_000_000, LastPrice: 1.0,
			ContractSize: 1.0, MaxLeverage: 10,
		}

		// Total pool = 1000 USDT, max per candidate = 200 USDT
		res := allocator.AllocateMargins(ctx, []domain.Candidate{c}, 1000.0, 200.0, 5.0, nil, nil)

		assert.Len(t, res, 1)
		assert.InDelta(t, 200.0, res[0].Config.MarginUSDT, 0.001)
	})

	t.Run("market impact limit caps volume and margin", func(t *testing.T) {
		t.Parallel()

		// 24h volume = 144,000 USDT -> minute volume = 100 USDT/min.
		// 5% max impact ratio -> 5 USDT max position volume.
		// 10x leverage -> 0.5 USDT required margin.
		c := domain.Candidate{
			Config:    domain.TradeConfig{Leverage: 10},
			Symbol:    "TINYVOL",
			Vol24USDT: 144_000, LastPrice: 1.0,
			ContractSize: 1.0, MaxLeverage: 10,
		}

		res := allocator.AllocateMargins(ctx, []domain.Candidate{c}, 100.0, 100.0, 0.05, nil, nil)

		assert.Len(t, res, 1)
		assert.InDelta(t, 0.5, res[0].Config.MarginUSDT, 0.001)
	})

	t.Run("exchange risk limit leverage adjustment", func(t *testing.T) {
		t.Parallel()

		c := domain.Candidate{
			Config:    domain.TradeConfig{Leverage: 20},
			Symbol:    "RISKY",
			Vol24USDT: 100_000_000, LastPrice: 1.0,
			ContractSize: 1.0, MaxLeverage: 50,
		}

		// Risk limit client forces max leverage = 5 for position size
		client := &mockRiskLimitClient{riskLimitLev: 5}

		res := allocator.AllocateMargins(ctx, []domain.Candidate{c}, 100.0, 100.0, 5.0, client, nil)

		assert.Len(t, res, 1)
		assert.Equal(t, 5, res[0].Config.Leverage)
		assert.InDelta(t, 100.0, res[0].Config.MarginUSDT, 0.001)
	})

	t.Run("budget exhaustion filters out unfunded candidates", func(t *testing.T) {
		t.Parallel()

		c1 := domain.Candidate{
			Config:    domain.TradeConfig{Leverage: 10},
			Symbol:    "LOWVOL",
			Vol24USDT: 1_000_000, LastPrice: 1.0,
			ContractSize: 1.0, MaxLeverage: 10,
		}
		c2 := domain.Candidate{
			Config:    domain.TradeConfig{Leverage: 10},
			Symbol:    "HIGHVOL",
			Vol24USDT: 10_000_000, LastPrice: 1.0,
			ContractSize: 1.0, MaxLeverage: 10,
		}

		// Total pool = 100 USDT, max candidate margin = 100 USDT.
		// HIGHVOL consumes the full 100 USDT margin pool due to higher opportunity score.
		// LOWVOL receives 0 USDT and 0 volume, so it gets filtered out of result slice.
		res := allocator.AllocateMargins(ctx, []domain.Candidate{c1, c2}, 100.0, 100.0, 0.0, nil, nil)

		assert.Len(t, res, 1)
		assert.Equal(t, "HIGHVOL", res[0].Symbol)
		assert.InDelta(t, 100.0, res[0].Config.MarginUSDT, 0.001)
	})

	t.Run("untradeable zero contract volume filtered out", func(t *testing.T) {
		t.Parallel()

		// High price (1,000,000 USDT) with integer lot size (VolScale=0) and small margin (1 USDT).
		// Volume = 1 * 10 / (1,000,000 * 1) = 0.00001 -> rounds down to 0.0 contracts.
		c := domain.Candidate{
			Config:    domain.TradeConfig{Leverage: 10},
			Symbol:    "HUGEPRICE",
			Vol24USDT: 10_000_000, LastPrice: 1_000_000.0,
			ContractSize: 1.0, MaxLeverage: 10, VolScale: 0,
		}

		res := allocator.AllocateMargins(ctx, []domain.Candidate{c}, 1.0, 1.0, 0.0, nil, nil)

		// Filtered out because Volume == 0
		assert.Empty(t, res)
	})

	t.Run("unconstrained margin cap when maxCandidateMargin is zero", func(t *testing.T) {
		t.Parallel()

		c := domain.Candidate{
			Config:    domain.TradeConfig{Leverage: 10},
			Symbol:    "UNCONSTRAINED",
			Vol24USDT: 100_000_000, LastPrice: 1.0,
			ContractSize: 1.0, MaxLeverage: 10,
		}

		// maxCandidateMargin = 0 (unconstrained) -> uses all 500 USDT available
		res := allocator.AllocateMargins(ctx, []domain.Candidate{c}, 500.0, 0.0, 0.0, nil, nil)

		assert.Len(t, res, 1)
		assert.InDelta(t, 500.0, res[0].Config.MarginUSDT, 0.001)
	})

	t.Run("independent per candidate custom leverage", func(t *testing.T) {
		t.Parallel()

		c1 := domain.Candidate{
			Config:    domain.TradeConfig{Leverage: 5},
			Symbol:    "SYM5X",
			Vol24USDT: 1_000_000, LastPrice: 1.0,
			ContractSize: 1.0, MaxLeverage: 10,
		}
		c2 := domain.Candidate{
			Config:    domain.TradeConfig{Leverage: 20},
			Symbol:    "SYM20X",
			Vol24USDT: 2_000_000, LastPrice: 1.0,
			ContractSize: 1.0, MaxLeverage: 50,
		}

		res := allocator.AllocateMargins(ctx, []domain.Candidate{c1, c2}, 200.0, 100.0, 0.0, nil, nil)

		assert.Len(t, res, 2)
		assert.Equal(t, 20, res[0].Config.Leverage)
		assert.Equal(t, 5, res[1].Config.Leverage)
		assert.InDelta(t, 100.0, res[0].Config.MarginUSDT, 0.001)
		assert.InDelta(t, 100.0, res[1].Config.MarginUSDT, 0.001)
		assert.InDelta(t, 100.0, res[0].Config.MarginUSDT, 0.001)
		assert.InDelta(t, 100.0, res[1].Config.MarginUSDT, 0.001)
	})
}
