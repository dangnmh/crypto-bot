package domain_test

import (
	"context"
	"errors"
	"testing"

	"crypto-bot/internal/bots/funding/domain"

	"github.com/stretchr/testify/assert"
)

type mockLeverageClient struct {
	supportOnOrder bool
	riskLimitLev   int
	riskLimitErr   error
	maxLev         int
	maxLevErr      error
}

func (m *mockLeverageClient) SupportLeverageOnOrder() bool {
	return m.supportOnOrder
}

func (m *mockLeverageClient) GetMaxLeverageForValue(ctx context.Context, symbol string, value float64) (int, error) {
	return m.riskLimitLev, m.riskLimitErr
}

func (m *mockLeverageClient) GetMaxLeverage(ctx context.Context, symbol string) (int, error) {
	return m.maxLev, m.maxLevErr
}

func TestDetermineCandidateLeverage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("nil candidate", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, 1, domain.DetermineCandidateLeverage(ctx, nil, nil, nil))
	})

	t.Run("leverage <= 0", func(t *testing.T) {
		t.Parallel()
		c := &domain.Candidate{Config: domain.TradeConfig{Leverage: 0}}
		assert.Equal(t, 1, domain.DetermineCandidateLeverage(ctx, nil, c, nil))
	})

	t.Run("capped by symbol max leverage", func(t *testing.T) {
		t.Parallel()
		c := &domain.Candidate{
			Config:      domain.TradeConfig{Leverage: 20},
			MaxLeverage: 10,
		}
		client := &mockLeverageClient{supportOnOrder: true}
		assert.Equal(t, 10, domain.DetermineCandidateLeverage(ctx, client, c, nil))
	})

	t.Run("client supports leverage on order", func(t *testing.T) {
		t.Parallel()
		c := &domain.Candidate{
			Config:      domain.TradeConfig{Leverage: 15},
			MaxLeverage: 20,
		}
		client := &mockLeverageClient{supportOnOrder: true}
		assert.Equal(t, 15, domain.DetermineCandidateLeverage(ctx, client, c, nil))
	})

	t.Run("risk limit leverage provider caps leverage", func(t *testing.T) {
		t.Parallel()
		c := &domain.Candidate{
			Config:      domain.TradeConfig{Leverage: 20, MarginUSDT: 10},
			MaxLeverage: 50, ContractSize: 1.0,
			LastPrice: 100.0,
		}
		client := &mockLeverageClient{
			supportOnOrder: false,
			riskLimitLev:   10,
		}
		assert.Equal(t, 10, domain.DetermineCandidateLeverage(ctx, client, c, nil))
	})

	t.Run("risk limit provider error fallback", func(t *testing.T) {
		t.Parallel()
		c := &domain.Candidate{
			Config:      domain.TradeConfig{Leverage: 20, MarginUSDT: 10},
			MaxLeverage: 50,
		}
		client := &mockLeverageClient{
			supportOnOrder: false,
			riskLimitErr:   errors.New("provider error"),
		}
		assert.Equal(t, 20, domain.DetermineCandidateLeverage(ctx, client, c, nil))
	})

	t.Run("max leverage provider caps leverage", func(t *testing.T) {
		t.Parallel()
		structClient := struct {
			domain.MaxLeverageProvider
		}{
			MaxLeverageProvider: &mockLeverageClient{maxLev: 5},
		}

		c := &domain.Candidate{
			Config:      domain.TradeConfig{Leverage: 10},
			MaxLeverage: 20,
		}
		assert.Equal(t, 5, domain.DetermineCandidateLeverage(ctx, structClient, c, nil))
	})
}
