package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"crypto-bot/internal/bots/funding/application"
	botconfig "crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/mexc"
	"crypto-bot/internal/infrastructure/notifier"
	"crypto-bot/pkg/httpclient"
	"crypto-bot/pkg/logger"
)

func main() {
	ctx := context.Background()
	log := logger.WithCtx(ctx, slog.Default())
	sysCfgPath := flag.String("sys", "./configs/funding/system.jsonc", "path to system config")
	botCfgPath := flag.String("bot", "./configs/funding/funding.jsonc", "path to bot config")
	flag.Parse()

	// 1. Load Configurations
	sysCfg, err := botconfig.LoadSystemConfig(*sysCfgPath)
	if err != nil {
		log.Error("Failed to load system config", "error", err)
		os.Exit(1)
	}

	// 2. Initialize Global Logger (from pkg/logger)
	cleanupLogger := logger.InitLogger(sysCfg.Logging.Level)
	defer cleanupLogger()
	log = logger.WithCtx(ctx, slog.Default())

	botCfg, err := botconfig.Load(sysCfg, *botCfgPath)
	if err != nil {
		log.Error("Failed to load bot config", "error", err)
		os.Exit(1)
	}

	// 3. Initialize Notifier
	n, err := notifier.NewFromConfig(sysCfg, slog.Default())
	if err != nil {
		log.Error("Failed to initialize notifier", "error", err)
		os.Exit(1)
	}
	if err := n.Start(ctx); err != nil {
		log.Error("Failed to start notifier", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := n.Stop(ctx); err != nil {
			log.Warn("Failed to stop notifier", "error", err)
		}
	}()

	// 4. Create exchange client (MEXC-specific wiring)
	httpPool := httpclient.NewPool(httpclient.DefaultPoolConfig())
	var client exchange.Client = mexc.NewClient(httpPool, sysCfg.ExchangeConfig.Mexc.Future.BaseURL, sysCfg.ExchangeConfig.Mexc.APIKey, sysCfg.ExchangeConfig.Mexc.APISecret, sysCfg.Logging)

	// 5. Wrap with DryRunClient if configured
	if sysCfg.DryRun {
		log.Warn("🧪 DRY-RUN MODE ENABLED — no real orders will be placed")
		client = exchange.NewDryRunClient(client)
	}

	// 6. Initialize Engine with injected client and WS adapter
	wsAdapter := mexc.NewWsAdapter()
	engine := app.NewEngine(app.EngineConfig{
		SystemConfig: &sysCfg.SystemConfig,
		Client:       client,
		Adapter:      wsAdapter,
	})

	// 7. Initialize the sniper bot
	sniper := application.NewSniper(botCfg, sysCfg, engine, n, slog.Default().With("bot", "funding"))

	// 8. Run the bot lifecycle
	if err := app.RunBot(engine, sniper); err != nil {
		os.Exit(1)
	}
}
