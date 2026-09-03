package futures

import (
	"context"
	"log/slog"

	infraapp "crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/notifier"

	"go.uber.org/fx"
)

// Module wires Futures OrderManager lifecycle and dependencies.
var Module = fx.Options(
	fx.Provide(
		ProvideOrderManager,
	),
)

// ProvideOrderManager instantiates and manages the lifecycle of Futures OrderManager.
func ProvideOrderManager(
	lc fx.Lifecycle,
	engine *infraapp.Engine,
	repo TradeRepository,
	n notifier.Notifier,
	log *slog.Logger,
) (*OrderManager, error) {
	mgr, err := NewOrderManager(context.Background(), engine, engine.Bus, repo, n, log)
	if err != nil {
		return nil, err
	}
	if lc != nil {
		lc.Append(fx.Hook{
			OnStop: func(ctx context.Context) error {
				return mgr.Shutdown(ctx)
			},
		})
	}
	return mgr, nil
}
