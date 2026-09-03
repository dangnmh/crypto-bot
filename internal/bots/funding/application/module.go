package application

import (
	"log/slog"
	"net/http"

	"crypto-bot/internal/bots/funding/application/dilution"
	"crypto-bot/internal/bots/funding/application/obfuscator"
	"crypto-bot/internal/bots/funding/application/reversion"
	"crypto-bot/internal/bots/funding/application/strategy"
	fundingconfig "crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/bots/funding/domain"
	infraapp "crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/notifier"
	"crypto-bot/internal/trading/ordermanager/futures"

	"go.uber.org/fx"
)

// Module wires funding application services, background jobs, strategies, and bot entrypoint.
var Module = fx.Options(
	reversion.Module,
	obfuscator.Module,
	dilution.Module,
	fx.Provide(
		ProvidePriceTrackJob,
		ProvideStatsReportJob,
		ProvideFundingBot,
	),
)

// ProvidePriceTrackJob provides a PriceTrackJob instance.
func ProvidePriceTrackJob(
	reportRepo domain.SymbolFundingReportRepository,
	cfg *fundingconfig.Config,
	sysCfg *fundingconfig.SystemConfig,
	engine *infraapp.Engine,
	tickRepo domain.FundingPriceTickRepository,
	httpClient *http.Client,
	log *slog.Logger,
) *PriceTrackJob {
	return NewPriceTrackJob(reportRepo, cfg, sysCfg, engine, tickRepo, httpClient, log)
}

// ProvideStatsReportJob provides a StatsReportJob instance.
func ProvideStatsReportJob(
	cfg *fundingconfig.Config,
	sysCfg *fundingconfig.SystemConfig,
	httpClient *http.Client,
	repo domain.SymbolFundingReportRepository,
	n notifier.Notifier,
	log *slog.Logger,
) *StatsReportJob {
	return NewStatsReportJob(cfg, sysCfg, httpClient, repo, n, log)
}

// ProvideFundingBot provides the main FundingBot instance implementing infraapp.Bot.
func ProvideFundingBot(
	cfg *fundingconfig.Config,
	sysCfg *fundingconfig.SystemConfig,
	engine *infraapp.Engine,
	n notifier.Notifier,
	reversionStrategy *reversion.Strategy,
	orderMgr *futures.OrderManager,
	statsReporter *StatsReportJob,
	priceTracker *PriceTrackJob,
	obfuscatorJob *obfuscator.ObfuscatorJob,
	dilutionJob *dilution.DilutionJob,
	log *slog.Logger,
) infraapp.Bot {
	bgStrats := []strategy.BackgroundStrategy{reversionStrategy, statsReporter, priceTracker, obfuscatorJob, dilutionJob}
	return NewFundingBot(
		cfg, sysCfg, engine, n,
		bgStrats,
		log.With("bot", "funding"),
	)
}
