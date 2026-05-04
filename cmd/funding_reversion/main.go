package main

import (
	"flag"
	"log/slog"
	"os"

	"crypto-bot/internal/bots/funding_reversion/application"
	botconfig "crypto-bot/internal/bots/funding_reversion/config"
	"crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/exchange/mexc"
	"crypto-bot/pkg/httpclient"
)

func main() {
	sysCfgPath := flag.String("sys", "./configs/funding_reversion/system.jsonc", "path to system config")
	botCfgPath := flag.String("bot", "./configs/funding_reversion/funding.jsonc", "path to bot config")
	flag.Parse()

	// 1. Load Configurations
	sysCfg, err := botconfig.LoadSystemConfig(*sysCfgPath)
	if err != nil {
		slog.Error("Failed to load system config", "error", err)
		os.Exit(1)
	}

	botCfg, err := botconfig.Load(sysCfg, *botCfgPath)
	if err != nil {
		panic("Failed to load bot config: " + err.Error())
	}

	// 2. Create exchange client (MEXC-specific wiring)
	httpPool := httpclient.NewPool(httpclient.DefaultPoolConfig())
	client := mexc.NewClient(httpPool, sysCfg.API.Future.BaseURL, sysCfg.APIKey, sysCfg.APISecret, sysCfg.Logging)

	// 3. Initialize Engine with injected client and WS adapter
	wsAdapter := mexc.NewWsAdapter()
	engine := app.NewEngine(app.EngineConfig{
		SystemConfig: &sysCfg.SystemConfig,
		Client:       client,
		Adapter:      wsAdapter,
	})

	// 4. Initialize the sniper bot
	sniper := application.NewSniper(botCfg, sysCfg, engine)

	// 5. Run the bot lifecycle
	app.RunBot(engine, sniper)
}
