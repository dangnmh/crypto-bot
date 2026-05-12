package application

import (
	"context"
	"fmt"
	"time"

	"crypto-bot/internal/bots/funding_reversion/config"
	"crypto-bot/internal/bots/funding_reversion/domain"
	"crypto-bot/internal/infrastructure/store"
)

// GetNextSettleTime resolves the next funding settlement time, preferring
// a simulated value from config when present.
func GetNextSettleTime(ctx context.Context, simulateSettle, symbol string, fundingStore store.FundingReader) (time.Time, error) {
	if simulateSettle != "" {
		sim, err := time.Parse(time.RFC3339, simulateSettle)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid simulateSettle datetime %q: %w", simulateSettle, err)
		}
		if sim.After(time.Now().Add(time.Minute)) {
			return sim, nil
		}
	}

	st, err := fundingStore.GetSettleTime(ctx, symbol)
	if err != nil {
		return time.Time{}, fmt.Errorf("settle time: %w", err)
	}
	return st, nil
}

// toTradeConfig converts a config.SymbolConfig to a domain.TradeConfig.
// Since config now reuses domain types for DynamicPricing and TrailingConfig,
// these can be assigned directly without field-by-field copying.
func toTradeConfig(sc config.SymbolConfig) domain.TradeConfig {
	return domain.TradeConfig{
		Symbol:              sc.Symbol,
		SimulateSettle:      sc.SimulateSettle,
		MaxPriceDiffPercent: sc.MaxPriceDiffPercent,
		MarginUSDT:          sc.MarginUSDT,
		Leverage:            sc.Leverage,
		FundingReversion:    sc.FundingReversion,
		FundingTrap:         sc.FundingTrap,
		ParsedOpenType:      sc.ParsedOpenType,
		ParsedPositionMode:  sc.ParsedPositionMode,
	}
}
