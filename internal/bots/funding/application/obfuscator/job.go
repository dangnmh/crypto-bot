package obfuscator

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"crypto-bot/internal/bots/funding/application/strategy"
	fundingconfig "crypto-bot/internal/bots/funding/config"
	shared "crypto-bot/internal/domain"

	"github.com/patrickmn/go-cache"
	"github.com/robfig/cron/v3"
)

var _ strategy.BackgroundStrategy = (*ObfuscatorJob)(nil)

// ObfuscatorJob runs background scheduled queries to identify profitable trades and execute obfuscation orders.
type ObfuscatorJob struct {
	cfg       fundingconfig.ObfuscatorConfig
	pnlReader PnLReportReader
	generator *OrderGenerator
	runner    *ObfuscatorRunner
	clock     shared.Clock
	logger    *slog.Logger
	processed *cache.Cache
	cron      *cron.Cron
	cancel    context.CancelFunc
	mu        sync.Mutex
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
		processed: cache.New(24*time.Hour, 1*time.Hour),
	}, nil
}

// Start begins the background cron scheduler implementing strategy.BackgroundStrategy.
func (j *ObfuscatorJob) Start(ctx context.Context, stores map[string]strategy.FundingStoreSet) error {
	if !j.cfg.Enabled {
		j.logger.InfoContext(ctx, "Order Obfuscator disabled in config; skipping background job start")
		return nil
	}

	pollInterval := time.Duration(j.cfg.PollInterval)
	j.logger.InfoContext(ctx, "🚀 Starting Order Obfuscator background job", slog.Duration("poll_interval", pollInterval))

	j.mu.Lock()
	cronCtx, cancel := context.WithCancel(ctx)
	j.cancel = cancel

	c := cron.New(cron.WithLocation(time.Local))
	spec := fmt.Sprintf("@every %s", pollInterval)
	_, err := c.AddFunc(spec, func() {
		if err := j.Tick(cronCtx); err != nil {
			j.logger.ErrorContext(cronCtx, "Obfuscator job tick failed", slog.Any("error", err))
		}
	})
	if err != nil {
		cancel()
		j.mu.Unlock()
		return fmt.Errorf("schedule obfuscator cron: %w", err)
	}

	j.cron = c
	c.Start()
	j.mu.Unlock()
	return nil
}

// Stop gracefully shuts down the background cron scheduler.
func (j *ObfuscatorJob) Stop(ctx context.Context) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	j.logger.InfoContext(ctx, "🛑 Order Obfuscator background job stopped")
	if j.cron != nil {
		j.cron.Stop()
	}
	if j.cancel != nil {
		j.cancel()
	}
	return nil
}

// IsSettlementBlackout returns true if the current time falls within the funding settlement window (-5m to +5m around every hour mark).
func IsSettlementBlackout(t time.Time) bool {
	minute := t.Minute()
	return minute >= 55 || minute <= 5
}

// Tick executes a single scan cycle over configured exchanges.
func (j *ObfuscatorJob) Tick(ctx context.Context) error {
	now := j.clock.Now()
	if IsSettlementBlackout(now) {
		j.logger.InfoContext(ctx, "⏳ Skipping obfuscator scan during funding settlement window (-5m to +5m)",
			slog.Time("now", now),
			slog.Int("minute", now.Minute()),
		)
		return nil
	}

	lookback := time.Duration(j.cfg.LookbackWindow)
	since := now.Add(-lookback)

	for exchange, exchCfg := range j.cfg.Exchanges {
		j.logger.InfoContext(ctx, "ObfuscatorJob Tick",
			slog.String("exchange", exchange),
			slog.Bool("enabled", exchCfg.Enabled))
		if !exchCfg.Enabled {
			continue
		}

		threshold := exchCfg.NetPnLThresholdUSDT
		records, err := j.pnlReader.GetProfitableTradeRecords(ctx, exchange, threshold, since)
		j.logger.InfoContext(ctx, "ObfuscatorJob GetProfitableTradeRecords", slog.Any("candidates", len(records)))
		if err != nil {
			j.logger.ErrorContext(ctx, "Failed to query profitable trade records", slog.String("exchange", exchange), slog.Any("error", err))
			continue
		}

		activeCount := 0
		for i := range records {
			record := &records[i]
			if exchCfg.MaxActiveOrders > 0 && activeCount >= exchCfg.MaxActiveOrders {
				j.logger.InfoContext(ctx, "Max active obfuscation orders reached for exchange; skipping remaining trades",
					slog.String("exchange", exchange),
					slog.Int("max_active_orders", exchCfg.MaxActiveOrders),
				)
				break
			}

			if err := j.processed.Add(record.ReqID, true, cache.DefaultExpiration); err != nil {
				continue // already obfuscated within 24h
			}

			j.logger.InfoContext(ctx, "🛡️ Profitable trade record detected; triggering obfuscation",
				slog.String("req_id", record.ReqID),
				slog.String("exchange", exchange),
				slog.String("symbol", record.Symbol),
				slog.Float64("net_profit", record.NetProfit),
				slog.Float64("threshold", threshold),
			)

			spec, err := j.generator.GenerateSpec(ctx, exchCfg, record)
			if err != nil {
				j.logger.ErrorContext(ctx, "Failed to generate obfuscation spec", slog.String("req_id", record.ReqID), slog.Any("error", err))
				j.processed.Delete(record.ReqID)
				continue
			}

			if err := j.runner.Execute(ctx, spec); err != nil {
				j.logger.ErrorContext(ctx, "Failed to execute obfuscation runner", slog.String("req_id", record.ReqID), slog.Any("error", err))
				j.processed.Delete(record.ReqID)
				continue
			}

			activeCount++
		}
	}

	return nil
}
