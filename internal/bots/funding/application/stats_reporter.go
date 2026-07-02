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

	"crypto-bot/internal/bots/funding/application/reversion"
	"crypto-bot/internal/bots/funding/application/strategy"
	fundingconfig "crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/bots/funding/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/aster"
	"crypto-bot/internal/infrastructure/exchange/batonex"
	"crypto-bot/internal/infrastructure/exchange/binance"
	"crypto-bot/internal/infrastructure/exchange/bingx"
	"crypto-bot/internal/infrastructure/exchange/bitfinex"
	"crypto-bot/internal/infrastructure/exchange/bitget"
	"crypto-bot/internal/infrastructure/exchange/bitmart"
	"crypto-bot/internal/infrastructure/exchange/bitmex"
	"crypto-bot/internal/infrastructure/exchange/bitunix"
	"crypto-bot/internal/infrastructure/exchange/blofin"
	"crypto-bot/internal/infrastructure/exchange/bybit"
	"crypto-bot/internal/infrastructure/exchange/bydfi"
	"crypto-bot/internal/infrastructure/exchange/coinex"
	"crypto-bot/internal/infrastructure/exchange/coinw"
	"crypto-bot/internal/infrastructure/exchange/cryptocom"
	"crypto-bot/internal/infrastructure/exchange/deepcoin"
	"crypto-bot/internal/infrastructure/exchange/deribit"
	"crypto-bot/internal/infrastructure/exchange/digifinex"
	"crypto-bot/internal/infrastructure/exchange/dydx"
	"crypto-bot/internal/infrastructure/exchange/gate"
	"crypto-bot/internal/infrastructure/exchange/hashkey"
	"crypto-bot/internal/infrastructure/exchange/htx"
	"crypto-bot/internal/infrastructure/exchange/hyperliquid"
	"crypto-bot/internal/infrastructure/exchange/krakenfutures"
	"crypto-bot/internal/infrastructure/exchange/kucoin"
	"crypto-bot/internal/infrastructure/exchange/lbank"
	"crypto-bot/internal/infrastructure/exchange/mexc"
	"crypto-bot/internal/infrastructure/exchange/okx"
	"crypto-bot/internal/infrastructure/exchange/orangex"
	"crypto-bot/internal/infrastructure/exchange/phemex"
	"crypto-bot/internal/infrastructure/exchange/pionex"
	"crypto-bot/internal/infrastructure/exchange/toobit"
	"crypto-bot/internal/infrastructure/exchange/weex"
	"crypto-bot/internal/infrastructure/exchange/whitebit"
	"crypto-bot/internal/infrastructure/exchange/woo"
	"crypto-bot/internal/infrastructure/exchange/xt"
	"crypto-bot/internal/infrastructure/exchange/zoomex"
	"crypto-bot/internal/infrastructure/notifier"
	"crypto-bot/pkg/decmath"

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

	clients := map[string]ScannerClient{
		"mexc":          mexc.NewClient(httpClient, "https://contract.mexc.com", "", "", logCfg),
		"gate":          gate.NewClient(httpClient, "https://api.gateio.ws/api/v4", "", "", logCfg),
		"bybit":         bybit.NewClient(httpClient, "https://api.bybit.com", "", "", "standard", logCfg),
		"okx":           okx.NewClient(httpClient, "https://www.okx.com", "", "", "", logCfg),
		"kucoin":        kucoin.NewClient(httpClient, "https://api-futures.kucoin.com", "", "", "", logCfg),
		"binance":       binance.NewClient(httpClient, "https://fapi.binance.com", "", "", logCfg),
		"hyperliquid":   hyperliquid.NewClient(context.Background(), httpClient, "https://api.hyperliquid.xyz", "", "", logCfg),
		"bitget":        bitget.NewClient(httpClient, "https://api.bitget.com", "", "", "", logCfg),
		"bingx":         bingx.NewClient(httpClient, "https://open-api.bingx.com", "", "", logCfg),
		"zoomex":        zoomex.NewClient(httpClient, "https://openapi.zoomex.com", logCfg),
		"deepcoin":      deepcoin.NewClient(httpClient, "https://api.deepcoin.com", "", "", "", logCfg),
		"toobit":        toobit.NewClient(httpClient, "https://api.toobit.com", "", "", logCfg),
		"weex":          weex.NewClient(httpClient, "https://api-contract.weex.com", "", "", "", logCfg),
		"batonex":       batonex.NewClient(httpClient, "https://api.batonex.com", logCfg),
		"bitmart":       bitmart.NewClient(httpClient, "https://api-cloud-v2.bitmart.com", "", "", "", logCfg),
		"coinw":         coinw.NewClient(httpClient, "https://api.coinw.com", logCfg),
		"krakenfutures": krakenfutures.NewClient(httpClient, "https://futures.kraken.com", logCfg),
		"bitunix":       bitunix.NewClient(httpClient, "https://fapi.bitunix.com", "", "", logCfg),
		"xt":            xt.NewClient(httpClient, "https://fapi.xt.com", "", "", logCfg),
		"htx":           htx.NewClient(httpClient, "https://api.hbdm.com", logCfg),
		"lbank":         lbank.NewClient(httpClient, "https://lbkperp.lbank.com", logCfg),
		"orangex":       orangex.NewClient(httpClient, "https://api.orangex.com/api/v1", "", "", logCfg),
		"pionex":        pionex.NewClient(httpClient, "https://api.pionex.com", logCfg),
		"deribit":       deribit.NewClient(httpClient, "https://www.deribit.com", logCfg),
		"coinex":        coinex.NewClient(httpClient, "https://api.coinex.com/v2", logCfg),
		"bitfinex":      bitfinex.NewClient(httpClient, "https://api-pub.bitfinex.com", logCfg),
		"whitebit":      whitebit.NewClient(httpClient, "https://whitebit.com", logCfg),
		"dydx":          dydx.NewClient(httpClient, "https://indexer.dydx.trade", logCfg),
		"aster":         aster.NewClient(httpClient, "https://fapi.asterdex.com", logCfg),
		"bitmex":        bitmex.NewClient(httpClient, "https://www.bitmex.com", logCfg),
		"hashkey":       hashkey.NewClient(httpClient, "https://api-glb.hashkey.com", log),
		"cryptocom":     cryptocom.NewClient(httpClient, "https://deriv-api.crypto.com/v1", log),
		"woo":           woo.NewClient(httpClient, "https://api.woox.io", log),
		"phemex":        phemex.NewClient(httpClient, "https://api.phemex.com", log),
		"blofin":        blofin.NewClient(httpClient, "https://openapi.blofin.com", log),
		"digifinex":     digifinex.NewClient(httpClient, "https://openapi.digifinex.com", log),
		"bydfi":         bydfi.NewClient(httpClient, "https://api.bydfi.com/api", log),
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
	_, err := c.AddFunc("45 * * * *", func() {
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
	// Condition 4: 24h volume at least 10M USDT
	results, err := client.GetPotentialFundingSymbols(ctx, 10000000, 0, nil, nil)
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
			NormalizedSymbol: reversion.GetNormalizedSymbol(r.Symbol),
			FundingRate:      decmath.RoundToScale(r.Rate, 3),
			Volume24h:        r.Volume24h,
			SettleTime:       settleTime,
		})
	}
	return exchMatches
}
