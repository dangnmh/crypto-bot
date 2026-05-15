package main

import (
	"flag"
	"log/slog"
	"os"

	"crypto-bot/internal/bots/funding/application"
	botconfig "crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/mexc"
	"crypto-bot/pkg/httpclient"
)

func main() {
	sysCfgPath := flag.String("sys", "./configs/funding/system.jsonc", "path to system config")
	botCfgPath := flag.String("bot", "./configs/funding/funding.jsonc", "path to bot config")
	flag.Parse()

	// 1. Load Configurations
	sysCfg, err := botconfig.LoadSystemConfig(*sysCfgPath)
	if err != nil {
		slog.Error("Failed to load system config", "error", err)
		os.Exit(1)
	}

	botCfg, err := botconfig.Load(sysCfg, *botCfgPath)
	if err != nil {
		slog.Error("Failed to load bot config", "error", err)
		os.Exit(1)
	}

	// 2. Initialize Telemetry (Removed)

	// 3. Create exchange client (MEXC-specific wiring)
	httpPool := httpclient.NewPool(httpclient.DefaultPoolConfig())
	var client exchange.Client = mexc.NewClient(httpPool, sysCfg.API.Future.BaseURL, sysCfg.APIKey, sysCfg.APISecret, sysCfg.Logging)

	// 4. Wrap with DryRunClient if configured
	if sysCfg.DryRun {
		slog.Warn("🧪 DRY-RUN MODE ENABLED — no real orders will be placed")
		client = exchange.NewDryRunClient(client)
	}

	// 5. Initialize Engine with injected client and WS adapter
	wsAdapter := mexc.NewWsAdapter()
	engine := app.NewEngine(app.EngineConfig{
		SystemConfig: &sysCfg.SystemConfig,
		Client:       client,
		Adapter:      wsAdapter,
	})

	// 6. Initialize the sniper bot
	sniper := application.NewSniper(botCfg, sysCfg, engine, slog.Default().With("bot", "funding"))

	// 7. Run the bot lifecycle
	if err := app.RunBot(engine, sniper); err != nil {
		os.Exit(1)
	}
}
