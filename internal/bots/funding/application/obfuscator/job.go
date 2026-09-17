package obfuscator

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"crypto-bot/internal/bots/funding/application/strategy"
	fundingconfig "crypto-bot/internal/bots/funding/config"
	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/trading/ordermanager/futures"
	ordermanagerpersistence "crypto-bot/internal/trading/ordermanager/persistence"
	"crypto-bot/pkg/ticker"
)

var _ strategy.BackgroundStrategy = (*ObfuscatorJob)(nil)

// ObfuscatorJob runs background scheduled queries to identify profitable trades and execute obfuscation orders.
type ObfuscatorJob struct {
	accountID       string
	accountExchange string
	cfg             fundingconfig.ObfuscatorConfig
	pnlReader       PnLReportReader
	generator       *OrderGenerator
	runner          *ObfuscatorRunner
	clock           shared.Clock
	logger          *slog.Logger
	cancel          context.CancelFunc
	mu              sync.Mutex
}

// NewObfuscatorJob initializes a new ObfuscatorJob.
func NewObfuscatorJob(
	cfg fundingconfig.ObfuscatorConfig,
	pnlReader PnLReportReader,
	generator *OrderGenerator,
	runner *ObfuscatorRunner,
	clock shared.Clock,
	logger *slog.Logger,
) (*ObfuscatorJob, error) {
	if pnlReader == nil || generator == nil || runner == nil || clock == nil {
		return nil, fmt.Errorf("missing required dependencies for ObfuscatorJob")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &ObfuscatorJob{
		cfg:       cfg,
		pnlReader: pnlReader,
		generator: generator,
		runner:    runner,
		clock:     clock,
		logger:    logger.With("component", "ObfuscatorJob"),
	}, nil
}

// AccountID returns the account ID bound to this ObfuscatorJob.
func (j *ObfuscatorJob) AccountID() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.accountID
}

// NewObfuscatorJobForAccount initializes an ObfuscatorJob bound to a specific account.
func NewObfuscatorJobForAccount(
	accountID string,
	accountExchange string,
	cfg fundingconfig.ObfuscatorConfig,
	pnlReader PnLReportReader,
	generator *OrderGenerator,
	runner *ObfuscatorRunner,
	clock shared.Clock,
	logger *slog.Logger,
) (*ObfuscatorJob, error) {
	if accountID == "" {
		return nil, fmt.Errorf("missing required accountID for ObfuscatorJob")
	}
	if accountExchange == "" {
		return nil, fmt.Errorf("missing required accountExchange for ObfuscatorJob (account %q)", accountID)
	}
	job, err := NewObfuscatorJob(cfg, pnlReader, generator, runner, clock, logger)
	if err != nil {
		return nil, err
	}
	job.accountID = accountID
	job.accountExchange = accountExchange
	return job, nil
}

// NewObfuscatorJobs creates ObfuscatorJob instances for all accounts configured in cfg.
func NewObfuscatorJobs(
	cfg *fundingconfig.Config,
	repo futures.TradeRepository,
	gen *OrderGenerator,
	runner *ObfuscatorRunner,
	clock shared.Clock,
	log *slog.Logger,
) ([]*ObfuscatorJob, error) {
	if cfg == nil {
		return nil, fmt.Errorf("missing required root config for ObfuscatorJobs")
	}
	if len(cfg.Accounts) == 0 {
		return nil, nil
	}
	var pnlReader PnLReportReader
	if reader, ok := repo.(PnLReportReader); ok {
		pnlReader = reader
	} else {
		pnlReader = noopPnLReader{}
	}

	accountIDs := make([]string, 0, len(cfg.Accounts))
	for accID := range cfg.Accounts {
		accountIDs = append(accountIDs, accID)
	}
	sort.Strings(accountIDs)

	var jobs []*ObfuscatorJob
	for _, accID := range accountIDs {
		accCfg := cfg.Accounts[accID]
		if accCfg == nil || accCfg.Obfuscator == nil {
			continue
		}
		job, err := NewObfuscatorJobForAccount(accID, accCfg.Account.Exchange, *accCfg.Obfuscator, pnlReader, gen, runner, clock, log)
		if err != nil {
			return nil, fmt.Errorf("account %q obfuscator job: %w", accID, err)
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

// Enabled returns true if obfuscation is enabled in config.
func (j *ObfuscatorJob) Enabled() bool {
	return j.cfg.Enabled
}

// Start begins the background scheduler implementing strategy.BackgroundStrategy.
func (j *ObfuscatorJob) Start(ctx context.Context, stores map[string]strategy.FundingStoreSet) error {
	if !j.cfg.Enabled {
		j.logger.InfoContext(ctx, "Order Obfuscator disabled in config; skipping background job start")
		return nil
	}

	pollInterval := time.Duration(j.cfg.PollInterval)
	jitter := time.Duration(j.cfg.Jitter)
	j.logger.InfoContext(ctx, "🚀 Starting Order Obfuscator background job",
		slog.Duration("poll_interval", pollInterval),
		slog.Duration("jitter", jitter),
	)

	j.mu.Lock()
	cronCtx, cancel := context.WithCancel(ctx)
	j.cancel = cancel

	go ticker.RunWithJitter(cronCtx, pollInterval, jitter, func() bool {
		if err := j.Tick(cronCtx); err != nil {
			j.logger.ErrorContext(cronCtx, "Obfuscator job tick failed", slog.Any("error", err))
		}
		return true
	})

	j.mu.Unlock()
	return nil
}

// Stop gracefully shuts down the background scheduler.
func (j *ObfuscatorJob) Stop(ctx context.Context) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	j.logger.InfoContext(ctx, "🛑 Order Obfuscator background job stopped")
	if j.cancel != nil {
		j.cancel()
	}
	return nil
}

// Tick executes a single scan cycle for the account.
func (j *ObfuscatorJob) Tick(ctx context.Context) error {
	if !j.cfg.Enabled {
		return nil
	}
	now := j.clock.Now()

	lookback := time.Duration(j.cfg.LookbackWindow)
	since := now.Add(-lookback)

	j.logger.InfoContext(ctx, "ObfuscatorJob Tick",
		slog.String("account", j.accountID),
		slog.String("exchange", j.accountExchange),
		slog.Bool("enabled", j.cfg.Enabled),
		slog.Float64("sacrifice_loss_pct", j.cfg.SacrificeLossPct),
	)

	j.processLossBudget(ctx, j.accountExchange, j.cfg, since, now)

	return nil
}

func (j *ObfuscatorJob) evaluateSymbolLossBudget(
	ctx context.Context,
	exchange string,
	exchCfg fundingconfig.ExchangeObfuscationCfg,
	summary *ordermanagerpersistence.SymbolPnLSummary,
) (float64, bool) {
	if summary.FundingNetProfit <= 0 {
		return 0, false
	}
	threshold := exchCfg.NetPnLThresholdUSDT
	if threshold > 0 && summary.FundingNetProfit < threshold {
		return 0, false
	}

	targetLoss := summary.FundingNetProfit * (exchCfg.SacrificeLossPct / 100.0)
	if exchCfg.MaxDailyLossUSD > 0 && targetLoss > exchCfg.MaxDailyLossUSD {
		targetLoss = exchCfg.MaxDailyLossUSD
	}

	currentLoss := 0.0
	if summary.ObfuscatorNetPnL < 0 {
		currentLoss = -summary.ObfuscatorNetPnL
	}

	if currentLoss >= targetLoss {
		j.logger.InfoContext(ctx, "🎯 Symbol obfuscator loss budget satisfied; skipping further orders",
			slog.String("exchange", exchange),
			slog.String("symbol", summary.Symbol),
			slog.Float64("funding_profit", summary.FundingNetProfit),
			slog.Float64("target_loss", targetLoss),
			slog.Float64("current_loss", currentLoss),
		)
		return 0, false
	}

	return targetLoss - currentLoss, true
}

func (j *ObfuscatorJob) processLossBudget(
	ctx context.Context,
	exchange string,
	exchCfg fundingconfig.ExchangeObfuscationCfg,
	since, now time.Time,
) {
	summaries, err := j.pnlReader.GetAccountSymbolPnLSummaries(ctx, j.accountID, exchange, since)
	if err != nil {
		j.logger.ErrorContext(ctx, "Failed to query symbol pnl summaries", slog.String("exchange", exchange), slog.String("account", j.accountID), slog.Any("error", err))
		return
	}

	j.logger.InfoContext(ctx, "ObfuscatorJob GetAccountSymbolPnLSummaries", slog.String("account", j.accountID), slog.Int("symbol_count", len(summaries)))

	activeCount := 0
	for i := range summaries {
		summary := &summaries[i]
		remainingLoss, ok := j.evaluateSymbolLossBudget(ctx, exchange, exchCfg, summary)
		if !ok {
			continue
		}

		if exchCfg.MaxActiveOrders > 0 && activeCount >= exchCfg.MaxActiveOrders {
			j.logger.InfoContext(ctx, "Max active obfuscation orders reached for exchange; skipping remaining symbols",
				slog.String("exchange", exchange),
				slog.Int("max_active_orders", exchCfg.MaxActiveOrders),
			)
			break
		}

		originReqID := fmt.Sprintf("loss-budget-%s-%d", summary.Symbol, now.Unix())
		j.logger.InfoContext(ctx, "🛡️ Loss budget active for symbol; triggering obfuscation order",
			slog.String("exchange", exchange),
			slog.String("symbol", summary.Symbol),
			slog.Float64("funding_profit", summary.FundingNetProfit),
			slog.Float64("remaining_loss", remainingLoss),
		)

		spec, err := j.generator.GenerateSpecForSymbol(ctx, j.accountID, exchCfg, exchange, summary.Symbol, remainingLoss, originReqID)
		if err != nil {
			j.logger.ErrorContext(ctx, "Failed to generate obfuscation spec for symbol",
				slog.String("symbol", summary.Symbol),
				slog.Any("error", err),
			)
			continue
		}

		if err := j.runner.Execute(ctx, spec); err != nil {
			j.logger.ErrorContext(ctx, "Failed to execute obfuscation runner",
				slog.String("symbol", summary.Symbol),
				slog.Any("error", err),
			)
			continue
		}

		activeCount++
	}
}
