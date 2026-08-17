package dilution

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"crypto-bot/internal/bots/funding/application/strategy"
	fundingconfig "crypto-bot/internal/bots/funding/config"
	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"

	"github.com/robfig/cron/v3"
)

var _ strategy.BackgroundStrategy = (*DilutionJob)(nil)

// DilutionJob manages background scheduled PostOnly maker quotes to safely dilute funding trade volume.
type DilutionJob struct {
	cfg    *fundingconfig.DilutionConfig
	engine EngineProviderGetter
	maker  *DilutionMaker
	runner *DilutionRunner
	clock  shared.Clock
	logger *slog.Logger
	cron   *cron.Cron
	cancel context.CancelFunc
	mu     sync.Mutex
}

// NewDilutionJob creates a new DilutionJob instance.
func NewDilutionJob(
	rootCfg *fundingconfig.Config,
	engine EngineProviderGetter,
	maker *DilutionMaker,
	runner *DilutionRunner,
	clock shared.Clock,
	logger *slog.Logger,
) (*DilutionJob, error) {
	var dilutionCfg *fundingconfig.DilutionConfig
	if rootCfg != nil {
		dilutionCfg = rootCfg.Dilution
	}
	if maker == nil || runner == nil || clock == nil {
		return nil, fmt.Errorf("missing required dependencies for DilutionJob")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &DilutionJob{
		cfg:    dilutionCfg,
		engine: engine,
		maker:  maker,
		runner: runner,
		clock:  clock,
		logger: logger.With("component", "DilutionJob"),
	}, nil
}

// Start starts the background dilution cron scheduler.
func (j *DilutionJob) Start(ctx context.Context, stores map[string]strategy.FundingStoreSet) error {
	if j.cfg == nil || !j.cfg.Enabled {
		j.logger.InfoContext(ctx, "Volume Dilution disabled in config; skipping background job start")
		return nil
	}

	pollInterval := time.Duration(j.cfg.PollInterval)
	if pollInterval <= 0 {
		pollInterval = 5 * time.Second
	}
	j.logger.InfoContext(ctx, "🚀 Starting Background Volume Dilution job", slog.Duration("poll_interval", pollInterval))

	j.mu.Lock()
	cronCtx, cancel := context.WithCancel(ctx)
	j.cancel = cancel

	c := cron.New(cron.WithLocation(time.Local))
	spec := fmt.Sprintf("@every %s", pollInterval)
	_, err := c.AddFunc(spec, func() {
		if err := j.Tick(cronCtx); err != nil {
			j.logger.ErrorContext(cronCtx, "Dilution job tick failed", slog.Any("error", err))
		}
	})
	if err != nil {
		cancel()
		j.mu.Unlock()
		return fmt.Errorf("schedule dilution cron: %w", err)
	}

	j.cron = c
	c.Start()
	j.mu.Unlock()
	return nil
}

// Stop gracefully stops the background cron scheduler.
func (j *DilutionJob) Stop(ctx context.Context) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	j.logger.InfoContext(ctx, "🛑 Background Volume Dilution job stopped")
	if j.cron != nil {
		j.cron.Stop()
	}
	if j.cancel != nil {
		j.cancel()
	}
	return nil
}

// IsSettlementBlackout returns true if the current time falls within the funding settlement blackout window (-5m to +5m).
func IsSettlementBlackout(t time.Time) bool {
	minute := t.Minute()
	return minute >= 59 || minute <= 1
}

// Tick executes a single dilution quoting cycle across enabled exchanges.
func (j *DilutionJob) Tick(ctx context.Context) error {
	if j.cfg == nil || !j.cfg.Enabled {
		return nil
	}

	now := j.clock.Now()
	if IsSettlementBlackout(now) {
		j.logger.DebugContext(ctx, "⏳ Skipping dilution quotes during funding settlement window (-5m to +5m)",
			slog.Time("now", now),
			slog.Int("minute", now.Minute()),
		)
		return nil
	}

	for exchangeName, exchCfg := range j.cfg.Exchanges {
		if !exchCfg.Enabled {
			continue
		}
		j.processExchange(ctx, exchangeName, exchCfg)
	}

	return nil
}

func (j *DilutionJob) processExchange(ctx context.Context, exchangeName string, exchCfg fundingconfig.ExchangeDilutionCfg) {
	posSummary := j.resolvePositionSummary(ctx, exchangeName, exchCfg.Symbol)

	if exchCfg.MaxPositionUSD > 0 && (posSummary.GrossUSD >= exchCfg.MaxPositionUSD || posSummary.NetUSD >= exchCfg.MaxPositionUSD || posSummary.NetUSD <= -exchCfg.MaxPositionUSD) {
		j.logger.InfoContext(ctx, "Max position ceiling reached for dilution symbol; quoting exit only",
			slog.String("exchange", exchangeName),
			slog.String("symbol", exchCfg.Symbol),
			slog.Float64("net_pos_usd", posSummary.NetUSD),
			slog.Float64("gross_pos_usd", posSummary.GrossUSD),
			slog.Float64("max_pos_usd", exchCfg.MaxPositionUSD),
		)
	}

	j.cancelStaleOrders(ctx, exchangeName, exchCfg.Symbol)

	specs, err := j.maker.GenerateQuotes(ctx, exchangeName, exchCfg, posSummary)
	if err != nil {
		j.logger.WarnContext(ctx, "Failed to generate dilution quotes",
			slog.String("exchange", exchangeName),
			slog.String("symbol", exchCfg.Symbol),
			slog.Any("error", err),
		)
		return
	}

	for _, spec := range specs {
		if err := j.runner.Execute(ctx, spec); err != nil {
			j.logger.ErrorContext(ctx, "Failed to execute dilution runner",
				slog.String("exchange", exchangeName),
				slog.String("symbol", exchCfg.Symbol),
				slog.Any("error", err),
			)
		}
	}
}

func (j *DilutionJob) cancelStaleOrders(ctx context.Context, exchangeName, symbol string) {
	if j.runner == nil {
		return
	}
	if err := j.runner.CancelOpenOrders(ctx, exchangeName, symbol); err != nil {
		j.logger.WarnContext(ctx, "Failed to cancel stale dilution orders via OrderManager",
			slog.String("exchange", exchangeName),
			slog.String("symbol", symbol),
			slog.Any("error", err),
		)
	}
}

func (j *DilutionJob) resolvePositionSummary(ctx context.Context, exchangeName, symbol string) PositionSummary {
	var summary PositionSummary
	executor := j.getExecutor(exchangeName)
	if executor == nil {
		return summary
	}

	positions, err := executor.GetOpenPositions(ctx, symbol)
	if err != nil || len(positions) == 0 {
		return summary
	}

	contractSize := j.getContractSize(ctx, exchangeName, symbol)

	for _, pos := range positions {
		if pos.Symbol != symbol {
			continue
		}
		rawVol := extractPositionVolume(pos)
		if rawVol == 0 {
			continue
		}

		vol := math.Abs(rawVol)
		posVal := calcPositionUSD(pos, vol, contractSize)
		if pos.PositionType == exchange.PositionTypeShort || rawVol < 0 {
			summary.ShortVol += vol
			summary.ShortUSD += posVal
		} else {
			summary.LongVol += vol
			summary.LongUSD += posVal
		}
	}

	summary.NetUSD = summary.LongUSD - summary.ShortUSD
	summary.GrossUSD = summary.LongUSD + summary.ShortUSD
	return summary
}

func (j *DilutionJob) getExecutor(exchangeName string) exchange.OrderExecutor {
	if j.engine == nil {
		return nil
	}
	prov, err := j.engine.GetProvider(exchangeName)
	if err != nil || prov == nil || prov.Client == nil {
		return nil
	}
	executor, ok := prov.Client.(exchange.OrderExecutor)
	if !ok {
		return nil
	}
	return executor
}

func (j *DilutionJob) getContractSize(ctx context.Context, exchangeName, symbol string) float64 {
	if j.maker != nil {
		mInfo := j.maker.resolveMarketInfo(ctx, exchangeName, symbol)
		if mInfo.ContractSize > 0 {
			return mInfo.ContractSize
		}
	}
	return 1.0
}

func extractPositionVolume(pos exchange.Position) float64 {
	if pos.HoldVolContract != 0 {
		return pos.HoldVolContract
	}
	if pos.HoldVolCoin != 0 {
		return pos.HoldVolCoin
	}
	return pos.RawHoldVol
}

func calcPositionUSD(pos exchange.Position, vol, contractSize float64) float64 {
	if pos.HoldVolCoin > 0 {
		return math.Abs(pos.HoldVolCoin) * pos.HoldAvgPrice
	}
	return vol * contractSize * pos.HoldAvgPrice
}
