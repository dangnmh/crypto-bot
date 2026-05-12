package app

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// RunBot takes an initialized Bot and its parent Engine, and manages the full
// application lifecycle including background services, main execution, and graceful shutdown.
func RunBot(engine *Engine, bot Bot) error {
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := engine.Shutdown(shutdownCtx); err != nil {
			slog.Error("🔴 Engine shutdown error", "error", err)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	slog.Info("🚀 Starting background services...")
	if err := bot.RunAsBackground(ctx); err != nil {
		slog.Error("Failed to start background services", "error", err)
		return err
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := bot.Run(ctx); err != nil {
			slog.Error("🔴 Bot error", "error", err)
		}
	}()

	slog.Info("🟢 All systems running — ready for operations")

	// Wait for interrupt signal to gracefully shutdown.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Warn("🛑 Shutdown signal received — cleaning up...")

	// Cancel global context to signal all goroutines to stop.
	cancel()

	// Execute explicit bot stop.
	shutdownCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()

	if err := bot.Stop(shutdownCtx); err != nil {
		slog.Error("Error during bot shutdown", "error", err)
	}

	// Wait for bot.Run goroutine to finish, bounded by shutdown timeout.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		slog.Info("✅ All goroutines stopped cleanly")
	case <-shutdownCtx.Done():
		slog.Warn("⚠️ Shutdown timeout — some goroutines may still be running")
	}

	slog.Info("👋 Goodbye!")
	return nil
}
