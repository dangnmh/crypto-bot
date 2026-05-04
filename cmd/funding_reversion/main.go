package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"crypto-bot/internal/bots/funding_reversion/application"
	botconfig "crypto-bot/internal/bots/funding_reversion/config"
	"crypto-bot/internal/infrastructure/app"
	sysconfig "crypto-bot/internal/infrastructure/config"
)

func main() {
	// 1. Load System config
	sysCfg, err := sysconfig.Load("./configs/funding_reversion/system.jsonc")
	if err != nil {
		panic("Failed to load system config: " + err.Error())
	}

	// 2. Initialize Engine
	engine := app.NewEngine(sysCfg)
	defer engine.Shutdown()

	slog.Info("🚀 Funding Rate Sniper Bot starting...")

	// 3. Create global context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 4. Load Bot config
	botCfg, err := botconfig.Load(sysCfg, "./configs/funding_reversion/funding.jsonc")
	if err != nil {
		panic("Failed to load bot config: " + err.Error())
	}

	slog.Info("📋 Config loaded",
		"symbol_count", len(botCfg.Symbols),
		"base_url", sysCfg.API.BaseURL,
	)

	var symbolsToSync []string
	for _, s := range botCfg.Symbols {
		symbolsToSync = append(symbolsToSync, s.Symbol)
	}

	// 5. Start shared background services
	engine.StartBackgroundServices(ctx, symbolsToSync)

	// 6. Initialize the sniper bot
	sniper := application.NewSniper(botCfg, engine.Client, engine.WS, engine.Store, engine.TimeSync)

	// 7. Run the bot
	go func() {
		if err := sniper.Run(ctx); err != nil {
			slog.Error("🔴 Sniper error", "error", err)
		}
	}()

	slog.Info("🟢 All systems running — waiting for funding cycle")

	// 8. Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Warn("🛑 Shutdown signal received — cleaning up...")

	// Execute explicit bot stop
	shutdownCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	_ = sniper.Stop(shutdownCtx)

	// Cancel global context to shutdown background routines
	cancel()

	// Give goroutines time to clean up
	time.Sleep(2 * time.Second)

	slog.Info("👋 Goodbye!")
}
