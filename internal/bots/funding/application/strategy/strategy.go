package strategy

import (
	"context"
	"time"

	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/bots/funding/domain"
)

// Strategy owns one funding flow registration and lifecycle policy.
type Strategy interface {
	Flow() string
	Enabled(cfg config.SymbolConfig) bool
	// Execute runs the strategy for the given candidate sequentially.
	Execute(ctx context.Context, settleTime time.Time, candidate domain.Candidate) error
	// CleanupOpenExposure ensures any open positions are closed on abort.
	CleanupOpenExposure(ctx context.Context) error
}

// Registry wires enabled funding strategies into a cycle runtime.
type Registry struct {
	strategies []Strategy
}

func NewRegistry(strategies ...Strategy) *Registry {
	return &Registry{strategies: strategies}
}

func (r *Registry) Enabled(cfg config.SymbolConfig) []Strategy {
	enabled := make([]Strategy, 0, len(r.strategies))
	for _, s := range r.strategies {
		if s.Enabled(cfg) {
			enabled = append(enabled, s)
		}
	}
	return enabled
}

func (r *Registry) ExecuteAll(ctx context.Context, settleTime time.Time, cfg config.SymbolConfig, candidate domain.Candidate) error {
	for _, s := range r.Enabled(cfg) {
		if err := s.Execute(ctx, settleTime, candidate); err != nil {
			return err // Stop execution on first error
		}
	}
	return nil
}

func (r *Registry) CleanupOpenExposure(ctx context.Context, cfg config.SymbolConfig) {
	for _, s := range r.Enabled(cfg) {
		_ = s.CleanupOpenExposure(ctx)
	}
}
