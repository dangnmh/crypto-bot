package app

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// RunBot takes an initialized Bot and its parent Engine, and manages the full
// application lifecycle including background services, main execution, and graceful shutdown.
func RunBot(engine *Engine, bot Bot) {
	defer engine.Shutdown()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	slog.Info("🚀 Starting background services...")
	if err := bot.RunAsBackground(ctx); err != nil {
		slog.Error("Failed to start background services", "error", err)
		os.Exit(1)
	}

	go func() {
		if err := bot.Run(ctx); err != nil {
			slog.Error("🔴 Bot error", "error", err)
		}
	}()

	slog.Info("🟢 All systems running — ready for operations")

	// Wait for interrupt signal to gracefully shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Warn("🛑 Shutdown signal received — cleaning up...")

	// Execute explicit bot stop
	shutdownCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()

	if err := bot.Stop(shutdownCtx); err != nil {
		slog.Error("Error during bot shutdown", "error", err)
	}

	// Cancel global context to shutdown background routines
	cancel()

	// Give goroutines time to finish in-flight operations
	time.Sleep(2 * time.Second)

	slog.Info("👋 Goodbye!")
}
