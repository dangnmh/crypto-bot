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

const shutdownTimeout = 5 * time.Second

// RunBot takes an initialized Bot and its parent Engine, and manages the full
// application lifecycle including background services, main execution, and graceful shutdown.
func RunBot(engine *Engine, bot Bot) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := applogger.WithCtx(ctx, slog.Default())

	started := make(chan error, 1)
	done := make(chan error, 1)
	go func() {
		done <- RunBotWithContext(ctx, engine, bot, started)
	}()

	if err := <-started; err != nil {
		return err
	}

	// Wait for interrupt signal to gracefully shutdown.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)
	<-quit

	log.Warn("🛑 Shutdown signal received — cleaning up...")

	// Cancel global context to signal all goroutines to stop.
	cancel()
	return <-done
}

// RunBotWithContext manages the bot lifecycle until ctx is cancelled. If
// started is non-nil, exactly one value is sent after background startup
// succeeds or fails.
func RunBotWithContext(ctx context.Context, engine *Engine, bot Bot, started chan<- error) error {
	return runBotWithContext(ctx, engine, bot, started, nil)
}

func runBotWithContext(
	ctx context.Context,
	engine *Engine,
	bot Bot,
	started chan<- error,
	onRunError func(error),
) error {
	log := applogger.WithCtx(ctx, slog.Default())
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
		defer shutdownCancel()
		if err := engine.Shutdown(shutdownCtx); err != nil {
			applogger.WithCtx(shutdownCtx, slog.Default()).Error("🔴 Engine shutdown error", "error", err)
		}
	}()

	log.Info("🚀 Starting background services...")
	if err := bot.RunAsBackground(ctx); err != nil {
		if started != nil {
			started <- err
		}
		log.Error("Failed to start background services", "error", err)
		return err
	}
	if started != nil {
		started <- nil
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := bot.Run(ctx); err != nil {
			log.Error("🔴 Bot error", "error", err)
			if onRunError != nil {
				onRunError(err)
			}
		}
	}()

	log.Info("🟢 All systems running — ready for operations")

	<-ctx.Done()

	// Execute explicit bot stop.
	shutdownCtx, stopCancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
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
