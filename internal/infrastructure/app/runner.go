package app

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	applogger "crypto-bot/pkg/logger"
)

// RunBot takes an initialized Bot and its parent Engine, and manages the full
// application lifecycle including background services, main execution, and graceful shutdown.
func RunBot(engine *Engine, bot Bot) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := applogger.WithCtx(ctx, slog.Default())

	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer shutdownCancel()
		if err := engine.Shutdown(shutdownCtx); err != nil {
			applogger.WithCtx(shutdownCtx, slog.Default()).Error("🔴 Engine shutdown error", "error", err)
		}
	}()

	log.Info("🚀 Starting background services...")
	if err := bot.RunAsBackground(ctx); err != nil {
		log.Error("Failed to start background services", "error", err)
		return err
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := bot.Run(ctx); err != nil {
			log.Error("🔴 Bot error", "error", err)
		}
	}()

	log.Info("🟢 All systems running — ready for operations")

	// Wait for interrupt signal to gracefully shutdown.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Warn("🛑 Shutdown signal received — cleaning up...")

	// Cancel global context to signal all goroutines to stop.
	cancel()

	// Execute explicit bot stop.
	shutdownCtx, stopCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stopCancel()

	if err := bot.Stop(shutdownCtx); err != nil {
		applogger.WithCtx(shutdownCtx, slog.Default()).Error("Error during bot shutdown", "error", err)
	}

	// Wait for bot.Run goroutine to finish, bounded by shutdown timeout.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		applogger.WithCtx(shutdownCtx, slog.Default()).Info("✅ All goroutines stopped cleanly")
	case <-shutdownCtx.Done():
		applogger.WithCtx(shutdownCtx, slog.Default()).Warn("⚠️ Shutdown timeout — some goroutines may still be running")
	}

	applogger.WithCtx(shutdownCtx, slog.Default()).Info("👋 Goodbye!")
	return nil
}
