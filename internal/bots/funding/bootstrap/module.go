package bootstrap

import (
	"context"
	"log/slog"
	"net/http"

	"crypto-bot/internal/bots/funding/application"
	fundingconfig "crypto-bot/internal/bots/funding/config"
	infraapp "crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/mexc"
	"crypto-bot/internal/infrastructure/notifier"
	infraws "crypto-bot/internal/infrastructure/ws"
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
			provideExchangeClient,
			provideWSAdapter,
			provideEngine,
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
	n, err := notifier.NewFromConfig(cfg, log)
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

func provideExchangeClient(httpClient *http.Client, cfg *fundingconfig.SystemConfig, log *slog.Logger) exchange.Client {
	var client exchange.Client = mexc.NewClient(
		httpClient,
		cfg.ExchangeConfig.Mexc.Future.BaseURL,
		cfg.ExchangeConfig.Mexc.APIKey,
		cfg.ExchangeConfig.Mexc.APISecret,
		cfg.Logging,
	)

	if cfg.DryRun {
		log.Warn("DRY-RUN MODE ENABLED: no real orders will be placed")
		client = exchange.NewDryRunClient(client)
	}

	return client
}

func provideWSAdapter() infraws.ExchangeAdapter {
	return mexc.NewWsAdapter()
}

func provideEngine(cfg *fundingconfig.SystemConfig, client exchange.Client, adapter infraws.ExchangeAdapter) *infraapp.Engine {
	return infraapp.NewEngine(infraapp.EngineConfig{
		SystemConfig: &cfg.SystemConfig,
		Client:       client,
		Adapter:      adapter,
	})
}

func provideBot(
	cfg *fundingconfig.Config,
	sysCfg *fundingconfig.SystemConfig,
	engine *infraapp.Engine,
	n notifier.Notifier,
	log *slog.Logger,
) infraapp.Bot {
	return application.NewSniper(cfg, sysCfg, engine, n, log.With("bot", "funding"))
}
