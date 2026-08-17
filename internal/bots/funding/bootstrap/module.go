package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"crypto-bot/internal/bots/funding/application"
	fundingconfig "crypto-bot/internal/bots/funding/config"
	persistence "crypto-bot/internal/bots/funding/infrastructure/persistence"
	shared "crypto-bot/internal/domain"
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
	System     string
	Exchange   string
	Bot        string
	Blacklist  string
	Reversion  string
	Obfuscator string
	Dilution   string
}

// Module wires the funding bot dependency graph and lifecycle.
func Module(paths ConfigPaths) fx.Option {
	return fx.Options(
		fx.Supply(paths),
		exchange.Module,
		notifier.Module,
		observability.Module,
		server.Module,
		infraapp.Module,
		ordermanager.Module,
		ordermanagerpersistence.Module,
		persistence.Module,
		application.Module,
		fx.Provide(
			provideSystemConfig,
			provideBaseSystemConfig,
			provideLogger,
			provideFundingConfig,
			provideNotifierConfig,
			provideEngine,
			provideClock,
			provideDatabase,
			provideGoCache,
		),
		fx.WithLogger(func(log *slog.Logger) fxevent.Logger {
			return &fxevent.SlogLogger{Logger: log.With("component", "fx")}
		}),
	)
}

func provideSystemConfig(paths ConfigPaths) (*fundingconfig.SystemConfig, error) {
	sysCfg, err := fundingconfig.LoadSystemConfig(paths.System, paths.Exchange)
	if err != nil {
		return nil, fmt.Errorf("load system config: %w", err)
	}
	return sysCfg, nil
}

func provideBaseSystemConfig(sysCfg *fundingconfig.SystemConfig) *sysconfig.SystemConfig {
	return &sysCfg.SystemConfig
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
	return fundingconfig.Load(cfg, paths.Bot, paths.Blacklist, paths.Reversion, paths.Obfuscator, paths.Dilution)
}

func provideNotifierConfig(cfg *fundingconfig.SystemConfig, fundingCfg *fundingconfig.Config) notifier.Config {
	enabled := false
	if fundingCfg != nil && fundingCfg.Reversion != nil {
		enabled = fundingCfg.Reversion.Notifier.Enabled
	}

	return notifier.Config{
		Enabled:          enabled,
		TelegramBotToken: cfg.NotiConfig.TelegramBotToken,
		TelegramChatID:   cfg.NotiConfig.TelegramChatID,
	}
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

func provideDatabase(lc fx.Lifecycle) (*gorm.DB, error) {
	return infraapp.InitDatabase(
		lc,
		&persistence.ReversionTradeReport{},
		&persistence.GormSymbolFundingReport{},
		&persistence.GormFundingPriceTick{},
		&ordermanagerpersistence.TradeRecord{},
	)
}

func provideClock() shared.Clock {
	return SystemClock{}
}
