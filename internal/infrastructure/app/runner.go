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

const shutdownTimeout = 5 * time.Second

// RunBot takes an initialized Bot and its parent Engine, and manages the full
// application lifecycle including background services, main execution, and graceful shutdown.
func RunBot(engine *Engine, bot Bot) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := slog.Default()

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

	log.WarnContext(ctx, "🛑 Shutdown signal received — cleaning up...")

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
	log := slog.Default()
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
		defer shutdownCancel()
		if err := engine.Shutdown(shutdownCtx); err != nil {
			log.ErrorContext(shutdownCtx, "🔴 Engine shutdown error", slog.Any("error", err))
		}
	}()

	log.InfoContext(ctx, "🚀 Starting background services...")
	if err := bot.RunAsBackground(ctx); err != nil {
		if started != nil {
			started <- err
		}
		log.ErrorContext(ctx, "Failed to start background services", slog.Any("error", err))
		return err
	}
	if started != nil {
		started <- nil
	}

	var wg sync.WaitGroup
	wg.Go(func() {
		if err := bot.Run(ctx); err != nil {
			log.ErrorContext(ctx, "🔴 Bot error", slog.Any("error", err))
			if onRunError != nil {
				onRunError(err)
			}
		}
	})

	log.InfoContext(ctx, "🟢 All systems running — ready for operations")

	<-ctx.Done()

	// Execute explicit bot stop.
	shutdownCtx, stopCancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer stopCancel()

	if err := bot.Stop(shutdownCtx); err != nil {
		log.ErrorContext(shutdownCtx, "Error during bot shutdown", slog.Any("error", err))
	}

	// Wait for bot.Run goroutine to finish, bounded by shutdown timeout.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		log.InfoContext(shutdownCtx, "✅ All goroutines stopped cleanly")
	case <-shutdownCtx.Done():
		log.WarnContext(shutdownCtx, "⚠️ Shutdown timeout — some goroutines may still be running")
	}

	log.InfoContext(shutdownCtx, "👋 Goodbye!")
	return nil
}
