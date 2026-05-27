package bootstrap

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"crypto-bot/internal/bots/funding/application"
	"crypto-bot/internal/bots/funding/application/reversion"
	"crypto-bot/internal/bots/funding/application/strategy"
	"crypto-bot/internal/bots/funding/application/trailing"
	"crypto-bot/internal/bots/funding/application/trap"
	fundingconfig "crypto-bot/internal/bots/funding/config"
	infraapp "crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/notifier"
	"crypto-bot/pkg/httpclient"
	applogger "crypto-bot/pkg/logger"

	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
)

// ConfigPaths contains the startup configuration file paths supplied by the CLI.
type ConfigPaths struct {
	System string
	Bot    string
}

// Module wires the funding bot dependency graph and lifecycle.
func Module(paths ConfigPaths) fx.Option {
	return fx.Options(
		fx.Supply(paths),
		fx.Provide(
			provideSystemConfig,
			provideLogger,
			provideFundingConfig,
			provideNotifier,
			provideHTTPClient,
			provideEngine,
			provideReversionStrategyFactory,
			provideTrapStrategyFactory,
			provideTrailingStrategyFactory,
			provideBot,
			infraapp.NewBotRunner,
		),
		fx.Invoke(infraapp.RegisterBotRunner),
		fx.WithLogger(func(log *slog.Logger) fxevent.Logger {
			return &fxevent.SlogLogger{Logger: log.With("component", "fx")}
		}),
	)
}

func provideSystemConfig(paths ConfigPaths) (*fundingconfig.SystemConfig, error) {
	return fundingconfig.LoadSystemConfig(paths.System)
}

func provideLogger(lc fx.Lifecycle, cfg *fundingconfig.SystemConfig) *slog.Logger {
	cleanup := applogger.InitLogger(cfg.Logging.Level)
	lc.Append(fx.Hook{
		OnStop: func(context.Context) error {
			cleanup()
			return nil
		},
	})
	return slog.Default()
}

func provideFundingConfig(paths ConfigPaths, cfg *fundingconfig.SystemConfig) (*fundingconfig.Config, error) {
	return fundingconfig.Load(cfg, paths.Bot)
}

func provideNotifier(lc fx.Lifecycle, cfg *fundingconfig.SystemConfig, log *slog.Logger) (notifier.Notifier, error) {
	n, err := notifier.NewFromConfig(notifier.Config{
		Enabled:          cfg.NotiConfig.Enabled,
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

func provideHTTPClient() *http.Client {
	return httpclient.NewPool(httpclient.DefaultPoolConfig())
}

func provideEngine(cfg *fundingconfig.SystemConfig, fundingCfg *fundingconfig.Config, httpClient *http.Client, log *slog.Logger) (*infraapp.Engine, error) {
	var activeExchanges []string
	if fundingCfg != nil {
		seen := make(map[string]bool)
		for i := range fundingCfg.Symbols {
			exch := strings.ToLower(strings.TrimSpace(fundingCfg.Symbols[i].Exchange))
			if exch != "" && !seen[exch] {
				seen[exch] = true
				activeExchanges = append(activeExchanges, exch)
			}
		}
	}

	return infraapp.NewEngine(context.Background(), infraapp.EngineConfig{
		SystemConfig:    &cfg.SystemConfig,
		HTTPClient:      httpClient,
		Logger:          log,
		ActiveExchanges: activeExchanges,
	})
}

func provideReversionStrategyFactory() application.ReversionStrategyFactory {
	return func(cfg fundingconfig.SymbolConfig, global *fundingconfig.Config, deps application.Deps) strategy.Strategy {
		return reversion.NewStrategy(cfg, global, deps)
	}
}

func provideTrapStrategyFactory() application.TrapStrategyFactory {
	return func(cfg fundingconfig.SymbolConfig, global *fundingconfig.Config, deps application.Deps) strategy.Strategy {
		return trap.NewStrategy(cfg, global, deps)
	}
}

func provideTrailingStrategyFactory() application.TrailingStrategyFactory {
	return func(cfg fundingconfig.SymbolConfig, global *fundingconfig.Config, deps application.Deps) strategy.Strategy {
		return trailing.NewStrategy(cfg, global, deps)
	}
}

func provideBot(
	cfg *fundingconfig.Config,
	sysCfg *fundingconfig.SystemConfig,
	engine *infraapp.Engine,
	n notifier.Notifier,
	reversionFactory application.ReversionStrategyFactory,
	trapFactory application.TrapStrategyFactory,
	trailingFactory application.TrailingStrategyFactory,
	log *slog.Logger,
) infraapp.Bot {
	return application.NewSniper(
		cfg, sysCfg, engine, n,
		reversionFactory, trapFactory, trailingFactory,
		log.With("bot", "funding"),
		reversion.InitGlobalSubscriptions,
	)
}
