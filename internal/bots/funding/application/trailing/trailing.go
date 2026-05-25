package trailing

import (
	"context"
	"log/slog"
	"time"

	"crypto-bot/internal/bots/funding/application"
	"crypto-bot/internal/bots/funding/application/strategy"
	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/bots/funding/domain"
)

const FlowTrailing = "trailing"

// Strategy implements strategy.Strategy for the trailing stop flow.
type Strategy struct {
	cfg    config.SymbolConfig
	global *config.Config
	deps   application.Deps
	log    *slog.Logger
}

// NewStrategy creates a new instance of the trailing strategy.
func NewStrategy(
	cfg config.SymbolConfig,
	global *config.Config,
	deps application.Deps,
) *Strategy {
	var logger *slog.Logger
	if deps.Log != nil {
		logger = deps.Log.With("flow", FlowTrailing)
	} else {
		logger = slog.Default().With("flow", FlowTrailing)
	}
	return &Strategy{
		cfg:    cfg,
		global: global,
		deps:   deps,
		log:    logger,
	}
}

var _ strategy.Strategy = (*Strategy)(nil)

func (s *Strategy) Flow() string {
	return FlowTrailing
}

func (s *Strategy) Enabled(cfg config.SymbolConfig) bool {
	return (&cfg).IsHedgeTrapEnabled() && cfg.FundingTrap.Trailing.Enabled
}

func (s *Strategy) Execute(ctx context.Context, settleTime time.Time, candidate domain.Candidate) error {
	// Not implemented yet
	return nil
}

func (s *Strategy) CleanupOpenExposure(context.Context) error {
	return nil
}

func (s *Strategy) RetryWithBackoff(ctx context.Context, attempts int, fn func() error) (int, error) {
	return s.RetryWithBackoffOpts(ctx, attempts, 100*time.Millisecond, 5*time.Second, fn)
}

func (s *Strategy) RetryWithBackoffOpts(ctx context.Context, attempts int, baseDelay, maxDelay time.Duration, fn func() error) (int, error) {
	if attempts <= 0 {
		attempts = 1
	}
	var err error
	delay := baseDelay
	for i := 1; i <= attempts; i++ {
		if err = fn(); err == nil {
			return i, nil
		}
		if i == attempts {
			break
		}
		jitter := delay * 20 / 100
		delayWithJitter := delay + time.Duration((float64(delay)-float64(jitter))*0.5+float64(jitter)*0.5)
		if sleepErr := s.deps.Clock.Sleep(ctx, delayWithJitter); sleepErr != nil {
			return i, err
		}
		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
		}
	}
	return attempts, err
}
