package application

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"sync"
	"time"

	"crypto-bot/internal/bots/funding/application/strategy"
	fundingconfig "crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/bots/funding/domain"
	infraapp "crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/notifier"
	"crypto-bot/pkg/decmath"
	"crypto-bot/pkg/formatutil"

	"github.com/robfig/cron/v3"
)

// ScannerClient defines the interface for scanning funding rates.
type ScannerClient interface {
	GetPotentialFundingSymbols(
		ctx context.Context,
		minVol24h, maxVol24h float64,
		whitelist []string,
		blacklist []string,
	) ([]exchange.PotentialFundingResult, error)
}

// StatsReportJob is a background strategy that aggregates and registers high-conviction funding rate statistics.
type StatsReportJob struct {
	cfg        *fundingconfig.Config
	sysCfg     *fundingconfig.SystemConfig
	httpClient *http.Client
	repo       domain.SymbolFundingReportRepository
	notifier   notifier.Notifier
	log        *slog.Logger
	Clients    map[string]ScannerClient
	cron       *cron.Cron
	cancel     context.CancelFunc
}

// NewStatsReportJob creates a new StatsReportJob.
//
//nolint:goconst // Public base URLs are raw strings, making them constants is unnecessary code churn
func NewStatsReportJob(
	cfg *fundingconfig.Config,
	sysCfg *fundingconfig.SystemConfig,
	httpClient *http.Client,
	repo domain.SymbolFundingReportRepository,
	n notifier.Notifier,
	log *slog.Logger,
) *StatsReportJob {
	logCfg := sysCfg.Logging

	exchanges := []string{
		"mexc", "gate", "bybit", "okx", "kucoin", "binance", "hyperliquid", "bitget",
		"bingx", "zoomex", "deepcoin", "gemini", "toobit", "weex", "batonex", "bitmart",
		"coinw", "krakenfutures", "bitunix", "xt", "htx", "lbank", "mandala", "orangex",
		"pionex", "poloniex", "deribit", "delta", "coinex", "bitfinex", "whitebit", "dydx",
		"aster", "backpack", "aevo", "apex", "lighter", "tradexyz", "grvt", "pacifica",
		"extended", "jupiter", "avantis", "btse", "bitmex", "hashkey", "hibt", "hitbtc",
		"hotcoin", "cryptocom", "woox", "phemex", "blofin", "digifinex", "bydfi", "ju",
		"echobit", "sunx", "fameex", "fmfw", "coinbase", "koinbay", "trubit",
	}

	clients := make(map[string]ScannerClient)
	for _, name := range exchanges {
		clientExchangeName := name
		if name == "echobit" {
			clientExchangeName = "ju"
		}
		c, err := infraapp.BuildPublicClient(context.Background(), clientExchangeName, httpClient, log, logCfg)
		if err != nil {
			log.Warn("Failed to build public client for stats reporter", slog.String("exchange", name), slog.Any("error", err))
			continue
		}
		scanner, ok := c.(ScannerClient)
		if !ok {
			log.Warn("Client does not implement ScannerClient", slog.String("exchange", name))
			continue
		}
		clients[name] = scanner
	}

	return &StatsReportJob{
		cfg:        cfg,
		sysCfg:     sysCfg,
		httpClient: httpClient,
		repo:       repo,
		notifier:   n,
		log:        log.With("subsystem", "stats_reporter"),
		Clients:    clients,
	}
}

// Start registers the background cron scheduler.
func (j *StatsReportJob) Start(ctx context.Context, _ map[string]strategy.FundingStoreSet) error {
	j.log.InfoContext(ctx, "🚀 Starting background stats reporter cron loop")

	cronCtx, cancel := context.WithCancel(ctx)
	j.cancel = cancel

	c := cron.New(cron.WithLocation(time.Local))
	_, err := c.AddFunc("50 * * * *", func() {
		j.CollectStats(cronCtx)
	})
	if err != nil {
		cancel()
		return fmt.Errorf("failed to schedule stats reporter cron: %w", err)
	}

	j.cron = c
	c.Start()
	return nil
}

// Stop stops the scheduler and cancels the running context.
func (j *StatsReportJob) Stop(ctx context.Context) error {
	j.log.InfoContext(ctx, "🛑 Stopping stats reporter cron loop")
	if j.cron != nil {
		j.cron.Stop()
	}
	if j.cancel != nil {
		j.cancel()
	}
	return nil
}

// CollectStats aggregates funding rate statistics and saves them.
func (j *StatsReportJob) CollectStats(ctx context.Context) {
	now := time.Now()
	j.log.InfoContext(ctx, "📊 Running hourly funding stats collection...")

	var wg sync.WaitGroup
	var mu sync.Mutex
	var matchedReports []domain.SymbolFundingReport

	// Set aggregate timeout context for 5 minutes
	scanCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	for name, cl := range j.Clients {
		wg.Add(1)
		go func(exchName string, client ScannerClient) {
			defer wg.Done()
			exchMatches := j.scanExchange(scanCtx, exchName, client, now)
			if len(exchMatches) > 0 {
				mu.Lock()
				matchedReports = append(matchedReports, exchMatches...)
				mu.Unlock()
			}
		}(name, cl)
	}

	wg.Wait()

	// Sort opportunities by absolute funding rate descending
	sort.Slice(matchedReports, func(i, j int) bool {
		return math.Abs(matchedReports[i].FundingRate) > math.Abs(matchedReports[j].FundingRate)
	})

	// Save opportunities to the database
	if len(matchedReports) > 0 {
		err := j.repo.SaveBatch(ctx, matchedReports)
		if err != nil {
			j.log.ErrorContext(ctx, "Failed to save symbol funding stats to database", slog.Any("error", err))
		} else {
			j.log.InfoContext(ctx, "Successfully saved funding reports to database", slog.Int("count", len(matchedReports)))
		}
	} else {
		j.log.InfoContext(ctx, "No funding opportunities matched the filters (Rate >= 0.8%, Vol >= 10M USDT).")
	}
}

func (j *StatsReportJob) scanExchange(ctx context.Context, exchName string, client ScannerClient, now time.Time) []domain.SymbolFundingReport {
	var blacklist []string
	if j.cfg.Blacklist != nil {
		blacklist = append(blacklist, j.cfg.Blacklist.GetCommonBlacklist()...)
		blacklist = append(blacklist, j.cfg.Blacklist.GetExchangeBlacklist(exchName)...)
	}

	// Condition 4: 24h volume at least 10M USDT
	results, err := client.GetPotentialFundingSymbols(ctx, 10000000, 0, nil, blacklist)
	if err != nil {
		j.log.DebugContext(ctx, "Failed to query public symbols from exchange",
			slog.String("exchange", exchName),
			slog.Any("error", err),
		)
		return nil
	}

	var exchMatches []domain.SymbolFundingReport
	for _, r := range results {
		settleTime := time.UnixMilli(r.SettleTime)

		// Condition 1: Skip if settle time is in the past
		if !settleTime.After(now) {
			continue
		}

		// Condition 2: Settle time must be within 15 minutes of current time
		if settleTime.After(now.Add(15 * time.Minute)) {
			continue
		}

		// Condition 3: Funding rate at least 0.8% absolute (0.008 ratio)
		if math.Abs(r.Rate) < 0.008 {
			continue
		}

		exchMatches = append(exchMatches, domain.SymbolFundingReport{
			Timestamp:        now,
			Exchange:         exchName,
			Symbol:           r.Symbol,
			NormalizedSymbol: formatutil.GetNormalizedSymbol(r.Symbol),
			FundingRate:      decmath.RoundToScale(r.Rate, 3),
			Volume24h:        r.Volume24h,
			SettleTime:       settleTime,
		})
	}
	return exchMatches
}
