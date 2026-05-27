package app

import (
	"context"
	"errors"
	"log/slog"

	"go.uber.org/fx"
)

// BotRunner adapts the bot lifecycle to fx.Lifecycle.
type BotRunner struct {
	engine     *Engine
	bot        Bot
	shutdowner fx.Shutdowner
	cancel     context.CancelFunc
	done       chan error
}

// NewBotRunner creates an Fx-managed lifecycle runner for one bot.
func NewBotRunner(engine *Engine, bot Bot, shutdowner fx.Shutdowner) *BotRunner {
	return &BotRunner{
		engine:     engine,
		bot:        bot,
		shutdowner: shutdowner,
		done:       make(chan error, 1),
	}
}

// RegisterBotRunner starts the bot after Fx starts and stops it when Fx stops.
func RegisterBotRunner(lc fx.Lifecycle, runner *BotRunner) {
	lc.Append(fx.Hook{
		OnStart: runner.Start,
		OnStop:  runner.Stop,
	})
}

// Start launches the bot lifecycle in the background after startup checks pass.
func (r *BotRunner) Start(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	r.cancel = cancel

	started := make(chan error, 1)
	go func() {
		r.done <- runBotWithContext(runCtx, r.engine, r.bot, started, r.shutdown)
	}()

	if err := <-started; err != nil {
		cancel()
		return err
	}
	return nil
}

func (r *BotRunner) shutdown(err error) {
	if errors.Is(err, context.Canceled) || r.shutdowner == nil {
		return
	}
	if shutdownErr := r.shutdowner.Shutdown(); shutdownErr != nil {
		slog.Default().Error("failed to trigger Fx shutdown", slog.Any("error", shutdownErr))
	}
}

// Stop cancels the bot lifecycle and waits for shutdown to complete.
func (r *BotRunner) Stop(ctx context.Context) error {
	if r.cancel == nil {
		return nil
	}
	r.cancel()

	select {
	case err := <-r.done:
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	case <-ctx.Done():
		slog.Default().Warn("bot runner stop timed out", slog.Any("error", ctx.Err()))
		return ctx.Err()
	}
}
