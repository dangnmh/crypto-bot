package application

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"crypto-bot/internal/bots/funding/application/strategy"
	fundingconfig "crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/bots/funding/domain"
	infraapp "crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/exchange"

	"github.com/robfig/cron/v3"
)

// PriceTrackJob is a background worker that fetches post-settlement historical K-lines.
type PriceTrackJob struct {
	reportRepo domain.SymbolFundingReportRepository
	tickRepo   domain.FundingPriceTickRepository
	engine     *infraapp.Engine
	log        *slog.Logger
	cron       *cron.Cron
	cancel     context.CancelFunc
	sysCfg     *fundingconfig.SystemConfig
	httpClient *http.Client
}

// NewPriceTrackJob creates a new PriceTrackJob.
func NewPriceTrackJob(
	reportRepo domain.SymbolFundingReportRepository,
	sysCfg *fundingconfig.SystemConfig,
	engine *infraapp.Engine,
	tickRepo domain.FundingPriceTickRepository,
	httpClient *http.Client,
	log *slog.Logger,
) *PriceTrackJob {
	return &PriceTrackJob{
		reportRepo: reportRepo,
		tickRepo:   tickRepo,
		engine:     engine,
		sysCfg:     sysCfg,
		httpClient: httpClient,
		log:        log.With("subsystem", "price_tracker"),
	}
}

// Start registers the background cron scheduler.
func (j *PriceTrackJob) Start(ctx context.Context, _ map[string]strategy.FundingStoreSet) error {
	j.log.InfoContext(ctx, "🚀 Starting background price tracker (K-line fetcher) cron loop")

	cronCtx, cancel := context.WithCancel(ctx)
	j.cancel = cancel

	c := cron.New(cron.WithLocation(time.Local))
	// Run every 7 minutes to sweep completed settlements
	_, err := c.AddFunc("*/7 * * * *", func() {
		j.TrackPrePrices(cronCtx)
		j.TrackAfterPrices(cronCtx)
	})
	if err != nil {
		cancel()
		return fmt.Errorf("failed to schedule price tracker cron: %w", err)
	}

	j.cron = c
	c.Start()
	return nil
}

// Stop stops the scheduler and cancels the running context.
func (j *PriceTrackJob) Stop(ctx context.Context) error {
	j.log.InfoContext(ctx, "🛑 Stopping price tracker cron loop")
	if j.cron != nil {
		j.cron.Stop()
	}
	if j.cancel != nil {
		j.cancel()
	}
	return nil
}

// TrackPrePrices queries completed settlements and fetches their pre-settle price history (T-20m => T).
func (j *PriceTrackJob) TrackPrePrices(ctx context.Context) {
	now := time.Now()
	settleTimeThreshold := now.Add(-5 * time.Minute)

	reports, err := j.reportRepo.GetPendingPreFunding(ctx, settleTimeThreshold)
	if err != nil {
		j.log.ErrorContext(ctx, "Failed to query pending pre-funding reports", slog.Any("error", err))
		return
	}

	if len(reports) == 0 {
		return
	}

	for i := range reports {
		rep := &reports[i]
		j.log.InfoContext(ctx, "Fetching pre-funding price history for completed settlement",
			slog.String("exchange", rep.Exchange),
			slog.String("symbol", rep.Symbol),
			slog.Time("settle_time", rep.SettleTime),
		)

		startTime := rep.SettleTime.Add(-20 * time.Minute)
		endTime := rep.SettleTime

		ticks, err := j.FetchHistoryTicksRange(ctx, rep, startTime, endTime)
		if err != nil {
			j.log.ErrorContext(ctx, "Failed to fetch pre-funding historical K-lines",
				slog.String("exchange", rep.Exchange),
				slog.String("symbol", rep.Symbol),
				slog.Any("error", err),
			)
			// If it's a permanent error (unsupported/not implemented), mark as fetched to prevent infinite retries
			errStr := err.Error()
			if strings.Contains(errStr, "does not implement") || strings.Contains(errStr, "unsupported exchange") {
				_ = j.reportRepo.UpdatePreFunding(ctx, rep.ID, true)
			}
			continue
		}

		if len(ticks) > 0 {
			err = j.tickRepo.SaveBatch(ctx, ticks)
			if err != nil {
				j.log.ErrorContext(ctx, "Failed to save pre-funding ticks to database", slog.Any("error", err))
				continue
			}
		}

		// Mark completion
		err = j.reportRepo.UpdatePreFunding(ctx, rep.ID, true)
		if err != nil {
			j.log.ErrorContext(ctx, "Failed to update pre-funding status on report", slog.Uint64("id", uint64(rep.ID)), slog.Any("error", err))
		} else {
			j.log.InfoContext(ctx, "Successfully captured and saved pre-funding price history",
				slog.String("exchange", rep.Exchange),
				slog.String("symbol", rep.Symbol),
				slog.Int("ticks_saved", len(ticks)),
			)
		}
	}
}

// TrackAfterPrices queries completed settlements and fetches their post-settle price history (T => T+20m).
func (j *PriceTrackJob) TrackAfterPrices(ctx context.Context) {
	now := time.Now()
	settleTimeThreshold := now.Add(-25 * time.Minute)

	reports, err := j.reportRepo.GetPendingAfterFunding(ctx, settleTimeThreshold)
	if err != nil {
		j.log.ErrorContext(ctx, "Failed to query pending after-funding reports", slog.Any("error", err))
		return
	}

	if len(reports) == 0 {
		return
	}

	for i := range reports {
		rep := &reports[i]
		j.log.InfoContext(ctx, "Fetching after-funding price history for completed settlement",
			slog.String("exchange", rep.Exchange),
			slog.String("symbol", rep.Symbol),
			slog.Time("settle_time", rep.SettleTime),
		)

		startTime := rep.SettleTime
		endTime := rep.SettleTime.Add(20 * time.Minute)

		ticks, err := j.FetchHistoryTicksRange(ctx, rep, startTime, endTime)
		if err != nil {
			j.log.ErrorContext(ctx, "Failed to fetch after-funding historical K-lines",
				slog.String("exchange", rep.Exchange),
				slog.String("symbol", rep.Symbol),
				slog.Any("error", err),
			)
			// If it's a permanent error (unsupported/not implemented), mark as fetched to prevent infinite retries
			errStr := err.Error()
			if strings.Contains(errStr, "does not implement") || strings.Contains(errStr, "unsupported exchange") {
				_ = j.reportRepo.UpdateAfterFunding(ctx, rep.ID, true)
			}
			continue
		}

		if len(ticks) > 0 {
			err = j.tickRepo.SaveBatch(ctx, ticks)
			if err != nil {
				j.log.ErrorContext(ctx, "Failed to save after-funding ticks to database", slog.Any("error", err))
				continue
			}
		}

		// Mark completion
		err = j.reportRepo.UpdateAfterFunding(ctx, rep.ID, true)
		if err != nil {
			j.log.ErrorContext(ctx, "Failed to update after-funding status on report", slog.Uint64("id", uint64(rep.ID)), slog.Any("error", err))
		} else {
			j.log.InfoContext(ctx, "Successfully captured and saved after-funding price history",
				slog.String("exchange", rep.Exchange),
				slog.String("symbol", rep.Symbol),
				slog.Int("ticks_saved", len(ticks)),
			)
		}
	}
}

// FetchHistoryTicksRange fetches 1-minute historical candles using the client provider's FetchKlines method.
func (j *PriceTrackJob) FetchHistoryTicksRange(ctx context.Context, rep *domain.SymbolFundingReport, startTime, endTime time.Time) ([]domain.FundingPriceTick, error) {
	// 1. Get the provider from Engine or build client dynamically
	var klineProv exchange.KlineProvider
	prov, err := j.engine.GetProvider(rep.Exchange)
	if err == nil {
		var ok bool
		klineProv, ok = prov.Client.(exchange.KlineProvider)
		if !ok {
			return nil, fmt.Errorf("exchange client %s does not implement exchange.KlineProvider", rep.Exchange)
		}
	} else {
		klineProv, err = j.buildClient(ctx, rep.Exchange)
		if err != nil {
			return nil, fmt.Errorf("build client for %s: %w", rep.Exchange, err)
		}
	}

	// 2. Fetch klines
	klines, err := klineProv.FetchKlines(ctx, rep.Symbol, exchange.Interval1m, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("fetch klines: %w", err)
	}

	// 3. Map exchange.Kline to domain.FundingPriceTick
	var ticks []domain.FundingPriceTick
	for _, k := range klines {
		ts := time.UnixMilli(k.Timestamp).Truncate(time.Minute)
		// Filter out ticks outside the T-30m to T settle window
		if (ts.After(startTime) || ts.Equal(startTime)) && (ts.Before(endTime) || ts.Equal(endTime)) {
			ticks = append(ticks, domain.FundingPriceTick{
				Exchange:     rep.Exchange,
				Symbol:       rep.Symbol,
				SettleTime:   rep.SettleTime,
				Timestamp:    ts,
				IntervalType: "1m",
				Price:        k.Close,
				BidPrice:     k.Close,
				AskPrice:     k.Close,
			})
		}
	}
	return ticks, nil
}

// buildClient dynamically constructs a public REST client for scanner-only exchanges.
//
//nolint:contextcheck // Caller context is correctly propagated
func (j *PriceTrackJob) buildClient(ctx context.Context, exchangeName string) (exchange.KlineProvider, error) {
	c, err := infraapp.BuildPublicClient(ctx, exchangeName, j.httpClient, j.log, j.sysCfg.Logging)
	if err != nil {
		return nil, err
	}
	prov, ok := c.(exchange.KlineProvider)
	if !ok {
		return nil, fmt.Errorf("client %s does not implement exchange.KlineProvider", exchangeName)
	}
	return prov, nil
}
