package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"crypto-bot/internal/bots/funding/application"
	botconfig "crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/config"
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

	// 2. Resolve Telegram Token (Bitwarden -> Env fallback)
	token := os.Getenv("TELEGRAM_BOT_TOKEN")

	// Try Bitwarden if no env var
	if token == "" {
		bw, bwErr := config.NewBitwardenLoader()
		if bwErr == nil {
			if secret, secretErr := bw.GetSecret("TELEGRAM_BOT_TOKEN"); secretErr == nil {
				token = secret
				log.Info("Loaded TELEGRAM_BOT_TOKEN from Bitwarden")
			}
		}
	}

	// 3. Initialize Notifier
	n, err := notifier.NewFromConfig(sysCfg, token, slog.Default())
	if err != nil {
		log.Error("Failed to initialize notifier", "error", err)
		os.Exit(1)
	}
	if err := n.Start(); err != nil {
		log.Error("Failed to start notifier", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := n.Stop(); err != nil {
			log.Warn("Failed to stop notifier", "error", err)
		}
	}()

	// 4. Create exchange client (MEXC-specific wiring)
	httpPool := httpclient.NewPool(httpclient.DefaultPoolConfig())
	var client exchange.Client = mexc.NewClient(httpPool, sysCfg.API.Future.BaseURL, sysCfg.APIKey, sysCfg.APISecret, sysCfg.Logging)

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
