package bootstrap

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"crypto-bot/internal/bots/funding/application"
	"crypto-bot/internal/bots/funding/application/reversion"
	"crypto-bot/internal/bots/funding/application/strategy"
	fundingconfig "crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/bots/funding/domain"
	persistence "crypto-bot/internal/bots/funding/infrastructure/persistence"
	infraapp "crypto-bot/internal/infrastructure/app"
	sysconfig "crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/notifier"
	"crypto-bot/internal/infrastructure/observability"
	"crypto-bot/internal/infrastructure/server"
	"crypto-bot/internal/trading/ordermanager"
	ordermanagerpersistence "crypto-bot/internal/trading/ordermanager/persistence"
	applogger "crypto-bot/pkg/logger"

	"github.com/patrickmn/go-cache"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"gorm.io/gorm"
)

// ConfigPaths contains the startup configuration file paths supplied by the CLI.
type ConfigPaths struct {
	System    string
	Exchange  string
	Bot       string
	Blacklist string
	Reversion string
}

// Module wires the funding bot dependency graph and lifecycle.
func Module(paths ConfigPaths) fx.Option {
	return fx.Options(
		fx.Supply(paths),
		exchange.Module,
		fx.Provide(
			provideSystemConfig,
			provideBaseSystemConfig,
			provideLogger,
			provideFundingConfig,
			provideNotifier,
			provideEngine,
			persistence.InitDatabase,
			provideTradeReportRepository,
			provideTradeRepository,
			provideOrderManager,
			provideSymbolFundingReportRepository,
			providePriceTickRepository,
			providePriceTrackJob,
			provideStatsReportJob,
			provideGoCache,
			provideReversionStrategy,
			provideBot,
			infraapp.NewBotRunner,
			server.NewAPIServer,
			observability.InitMetrics,
		),
		fx.Invoke(
			infraapp.RegisterBotRunner,
			server.Register,
		),
		fx.WithLogger(func(log *slog.Logger) fxevent.Logger {
			return &fxevent.SlogLogger{Logger: log.With("component", "fx")}
		}),
	)
}

func provideSystemConfig(paths ConfigPaths) (*fundingconfig.SystemConfig, error) {
	return fundingconfig.LoadSystemConfig(paths.System, paths.Exchange)
}

func provideBaseSystemConfig(cfg *fundingconfig.SystemConfig) *sysconfig.SystemConfig {
	return &cfg.SystemConfig
}

func provideLogger(lc fx.Lifecycle, cfg *fundingconfig.SystemConfig) *slog.Logger {
	cleanup := applogger.InitLogger(cfg.Logging.Level, cfg.Env)
	lc.Append(fx.Hook{
		OnStop: func(context.Context) error {
			cleanup()
			return nil
		},
	})
	return slog.Default()
}

func provideFundingConfig(paths ConfigPaths, cfg *fundingconfig.SystemConfig) (*fundingconfig.Config, error) {
	return fundingconfig.Load(cfg, paths.Bot, paths.Blacklist, paths.Reversion)
}

func provideNotifier(lc fx.Lifecycle, cfg *fundingconfig.SystemConfig, fundingCfg *fundingconfig.Config, log *slog.Logger) (notifier.Notifier, error) {
	enabled := false
	if fundingCfg != nil && fundingCfg.Reversion != nil {
		enabled = fundingCfg.Reversion.Notifier.Enabled
	}

	n, err := notifier.NewFromConfig(notifier.Config{
		Enabled:          enabled,
		TelegramBotToken: cfg.NotiConfig.TelegramBotToken,
		TelegramChatID:   cfg.NotiConfig.TelegramChatID,
	}, log)
	if err != nil {
		return nil, err
	}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			return n.Start(ctx)
		},
		OnStop: func(ctx context.Context) error {
			return n.Stop(ctx)
		},
	})

	return n, nil
}

func collectActiveExchanges(fundingCfg *fundingconfig.Config) []string {
	if fundingCfg == nil {
		return nil
	}
	var activeExchanges []string
	seen := make(map[string]bool)

	// Case 1: Configured scanner (collect from Symbols if configured scanner is enabled)
	isConfigured := fundingCfg.Reversion == nil || fundingCfg.Reversion.Scanners.Configured
	if isConfigured {
		for i := range fundingCfg.Symbols {
			sym := &fundingCfg.Symbols[i]
			if fundingCfg.Blacklist != nil && fundingCfg.Blacklist.IsBlacklisted(sym.Exchange, sym.Symbol) {
				continue
			}
			exch := strings.ToLower(strings.TrimSpace(sym.Exchange))
			if exch != "" && !seen[exch] {
				seen[exch] = true
				activeExchanges = append(activeExchanges, exch)
			}
		}
	}

	// Case 2: Schedule scanner (collect from Reversion Schedule)
	if fundingCfg.Reversion != nil {
		for exch, enabled := range fundingCfg.Reversion.Scanners.Schedule {
			if enabled {
				exch = strings.ToLower(strings.TrimSpace(exch))
				if exch != "" && !seen[exch] {
					seen[exch] = true
					activeExchanges = append(activeExchanges, exch)
				}
			}
		}
	}
	return activeExchanges
}

func provideEngine(cfg *fundingconfig.SystemConfig, fundingCfg *fundingconfig.Config, httpClient *http.Client, log *slog.Logger) (*infraapp.Engine, error) {
	activeExchanges := collectActiveExchanges(fundingCfg)

	var timeSyncInterval time.Duration
	if fundingCfg != nil && fundingCfg.Reversion != nil {
		timeSyncInterval = time.Duration(fundingCfg.Reversion.Sync.Time)
	}

	return infraapp.NewEngine(context.Background(), infraapp.EngineConfig{
		SystemConfig:     &cfg.SystemConfig,
		HTTPClient:       httpClient,
		Logger:           log,
		ActiveExchanges:  activeExchanges,
		TimeSyncInterval: timeSyncInterval,
	})
}

func provideGoCache() *cache.Cache {
	return cache.New(time.Hour*24, time.Hour)
}

func provideReversionStrategy(
	engine *infraapp.Engine,
	cfg *fundingconfig.Config,
	n notifier.Notifier,
	repo domain.TradeReportRepository,
	c *cache.Cache,
	log *slog.Logger,
) *reversion.Strategy {
	return reversion.NewStrategy(engine, cfg, n, repo, c, log)
}

func provideTradeReportRepository(db *gorm.DB) domain.TradeReportRepository {
	return persistence.NewGormTradeReportRepository(db)
}

func provideTradeRepository(db *gorm.DB) ordermanager.TradeRepository {
	return ordermanagerpersistence.NewGormTradeRepository(db)
}

func provideOrderManager(
	lc fx.Lifecycle,
	engine *infraapp.Engine,
	repo ordermanager.TradeRepository,
	n notifier.Notifier,
	log *slog.Logger,
) (*ordermanager.OrderManager, error) {
	mgr, err := ordermanager.NewOrderManager(context.Background(), engine, engine.Bus, repo, n, log)
	if err != nil {
		return nil, err
	}
	if lc != nil {
		lc.Append(fx.Hook{
			OnStop: func(ctx context.Context) error {
				return mgr.Shutdown(ctx)
			},
		})
	}
	return mgr, nil
}

func provideSymbolFundingReportRepository(db *gorm.DB) domain.SymbolFundingReportRepository {
	return persistence.NewGormSymbolFundingReportRepository(db)
}

func providePriceTickRepository(db *gorm.DB) domain.FundingPriceTickRepository {
	return persistence.NewGormFundingPriceTickRepository(db)
}

func providePriceTrackJob(
	reportRepo domain.SymbolFundingReportRepository,
	cfg *fundingconfig.Config,
	sysCfg *fundingconfig.SystemConfig,
	engine *infraapp.Engine,
	tickRepo domain.FundingPriceTickRepository,
	httpClient *http.Client,
	log *slog.Logger,
) *application.PriceTrackJob {
	return application.NewPriceTrackJob(reportRepo, cfg, sysCfg, engine, tickRepo, httpClient, log)
}

func provideStatsReportJob(
	cfg *fundingconfig.Config,
	sysCfg *fundingconfig.SystemConfig,
	httpClient *http.Client,
	repo domain.SymbolFundingReportRepository,
	n notifier.Notifier,
	log *slog.Logger,
) *application.StatsReportJob {
	return application.NewStatsReportJob(cfg, sysCfg, httpClient, repo, n, log)
}

func provideBot(
	cfg *fundingconfig.Config,
	sysCfg *fundingconfig.SystemConfig,
	engine *infraapp.Engine,
	n notifier.Notifier,
	reversionStrategy *reversion.Strategy,
	orderMgr *ordermanager.OrderManager,
	statsReporter *application.StatsReportJob,
	priceTracker *application.PriceTrackJob,
	log *slog.Logger,
) infraapp.Bot {
	return application.NewFundingBot(
		cfg, sysCfg, engine, n,
		[]strategy.BackgroundStrategy{reversionStrategy, statsReporter, priceTracker},
		log.With("bot", "funding"),
	)
}
