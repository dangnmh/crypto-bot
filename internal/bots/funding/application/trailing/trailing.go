package trailing

import (
	"context"
	"log/slog"

	"crypto-bot/internal/bots/funding/application/strategy"
	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/infrastructure/app"
)

const FlowTrailing = "trailing"

// Strategy implements strategy.BackgroundStrategy for the trailing stop flow.
type Strategy struct{}

// NewStrategy creates a new instance of the trailing strategy.
func NewStrategy(
	engine *app.Engine,
	global *config.Config,
	log *slog.Logger,
) *Strategy {
	return &Strategy{}
}

var _ strategy.BackgroundStrategy = (*Strategy)(nil)

func (s *Strategy) Flow() string {
	return FlowTrailing
}

func (s *Strategy) Enabled(cfg config.SymbolConfig) bool {
	return (&cfg).IsHedgeTrapEnabled() && cfg.FundingTrap.Trailing.Enabled
}

// Start implements strategy.BackgroundStrategy.
func (s *Strategy) Start(ctx context.Context, stores map[string]strategy.FundingStoreSet) error {
	return nil
}

// Stop implements strategy.BackgroundStrategy.
func (s *Strategy) Stop(ctx context.Context) error {
	return nil
}
