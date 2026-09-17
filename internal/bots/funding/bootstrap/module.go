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
	"crypto-bot/internal/trading/ordermanager/futures"
	ordermanagerpersistence "crypto-bot/internal/trading/ordermanager/persistence"
	applogger "crypto-bot/pkg/logger"

	"github.com/patrickmn/go-cache"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"gorm.io/gorm"
)

// ConfigPaths contains the startup configuration file paths supplied by the CLI.
type ConfigPaths struct {
	Accounts  string
	System    string
	Exchange  string
	Blacklist string
	Reversion string
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
		futures.Module,
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
	return slog.Default().With("service", "funding")
}

func provideFundingConfig(paths ConfigPaths, cfg *fundingconfig.SystemConfig) (*fundingconfig.Config, error) {
	if paths.Accounts == "" {
		return nil, fmt.Errorf("accounts manifest path (-accounts) is required")
	}
	return fundingconfig.Load(cfg, paths.Accounts, paths.Blacklist, paths.Reversion)
}

func provideNotifierConfig(cfg *fundingconfig.SystemConfig, fundingCfg *fundingconfig.Config) notifier.Config {
	enabled := false
	if fundingCfg != nil {
		for _, acc := range fundingCfg.Accounts {
			if acc.Reversion != nil && acc.Reversion.Notifier.Enabled {
				enabled = true
				break
			}
		}
	}

	return notifier.Config{
		Enabled:                enabled,
		TelegramBotToken:       cfg.NotiConfig.TelegramBotToken,
		TelegramChatID:         cfg.NotiConfig.TelegramChatID,
		TelegramCriticalChatID: cfg.NotiConfig.TelegramCriticalChatID,
	}
}

func appendUniqueExchange(list []string, seen map[string]bool, rawExch string) []string {
	exch := strings.ToLower(strings.TrimSpace(rawExch))
	if exch != "" && !seen[exch] {
		seen[exch] = true
		return append(list, exch)
	}
	return list
}

func collectActiveExchanges(fundingCfg *fundingconfig.Config) []string {
	if fundingCfg == nil {
		return nil
	}
	var activeExchanges []string
	seen := make(map[string]bool)

	for _, acc := range fundingCfg.Accounts {
		if acc == nil || !acc.Account.Enabled {
			continue
		}
		activeExchanges = appendUniqueExchange(activeExchanges, seen, acc.Account.Exchange)
		for i := range acc.Symbols {
			sym := &acc.Symbols[i]
			if fundingCfg.Blacklist != nil && fundingCfg.Blacklist.IsBlacklisted(sym.Exchange, sym.Symbol) {
				continue
			}
			activeExchanges = appendUniqueExchange(activeExchanges, seen, sym.Exchange)
		}
	}

	return activeExchanges
}

func provideEngine(cfg *fundingconfig.SystemConfig, fundingCfg *fundingconfig.Config, httpClient *http.Client, log *slog.Logger) (*infraapp.Engine, error) {
	activeExchanges := collectActiveExchanges(fundingCfg)

	var timeSyncInterval time.Duration
	if fundingCfg != nil {
		for _, acc := range fundingCfg.Accounts {
			if acc.Reversion != nil && acc.Reversion.Sync.Time > 0 {
				dur := time.Duration(acc.Reversion.Sync.Time)
				if timeSyncInterval == 0 || dur < timeSyncInterval {
					timeSyncInterval = dur
				}
			}
		}
	}

	engine, err := infraapp.NewEngine(context.Background(), infraapp.EngineConfig{
		SystemConfig:     &cfg.SystemConfig,
		HTTPClient:       httpClient,
		Logger:           log,
		ActiveExchanges:  activeExchanges,
		TimeSyncInterval: timeSyncInterval,
	})
	if err != nil {
		return nil, err
	}

	if fundingCfg != nil && len(fundingCfg.Accounts) > 0 {
		factoryCfg := infraapp.ProviderFactoryConfig{
			SystemConfig: &cfg.SystemConfig,
			HTTPClient:   httpClient,
			Logger:       log,
			Bus:          engine.Bus,
		}
		for accID, accCfg := range fundingCfg.Accounts {
			accProv, err := infraapp.BuildAccountProvider(context.Background(), accCfg.Account, factoryCfg)
			if err != nil {
				return nil, fmt.Errorf("build account provider for %q: %w", accID, err)
			}
			engine.AccountProviders[accID] = accProv
		}
	}

	return engine, nil
}

func provideGoCache() *cache.Cache {
	return cache.New(time.Hour*24, time.Hour)
}

func provideDatabase(lc fx.Lifecycle) (*gorm.DB, error) {
	return infraapp.InitDatabase(
		lc,
		&persistence.GormSymbolFundingReport{},
		&persistence.GormFundingPriceTick{},
		&ordermanagerpersistence.TradeRecord{},
	)
}

func provideClock() shared.Clock {
	return shared.SystemClock{}
}
