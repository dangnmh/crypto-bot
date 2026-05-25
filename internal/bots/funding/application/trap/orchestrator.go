package trap

import (
	"context"
	"time"

	"crypto-bot/internal/bots/funding/application"
	"crypto-bot/internal/bots/funding/application/strategy"
	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/bots/funding/domain"
)

const FlowTrap = "trap"

// Strategy implements strategy.Strategy for the trap flow.
type Strategy struct {
	cfg    config.SymbolConfig
	global *config.Config
	deps   application.Deps
}

// NewStrategy creates a new instance of the trap strategy.
func NewStrategy(
	cfg config.SymbolConfig,
	global *config.Config,
	deps application.Deps,
) *Strategy {
	return &Strategy{
		cfg:    cfg,
		global: global,
		deps:   deps,
	}
}

var _ strategy.Strategy = (*Strategy)(nil)

func (s *Strategy) Flow() string {
	return FlowTrap
}

func (s *Strategy) Enabled(cfg config.SymbolConfig) bool {
	return false
}

func (s *Strategy) Execute(ctx context.Context, settleTime time.Time, candidate domain.Candidate) error {
	return nil
}

func (s *Strategy) CleanupOpenExposure(ctx context.Context) error {
	return nil
}
