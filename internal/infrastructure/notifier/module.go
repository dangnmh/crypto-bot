package notifier

import (
	"context"
	"log/slog"

	"go.uber.org/fx"
)

// Module wires Notifier dependencies and lifecycle hooks.
var Module = fx.Options(
	fx.Provide(
		ProvideNotifier,
	),
)

// ProvideNotifier instantiates a Notifier from Config and registers lifecycle hooks.
func ProvideNotifier(lc fx.Lifecycle, cfg Config, log *slog.Logger) (Notifier, error) {
	n, err := NewFromConfig(cfg, log)
	if err != nil {
		return nil, err
	}

	if lc != nil {
		lc.Append(fx.Hook{
			OnStart: func(ctx context.Context) error {
				return n.Start(ctx)
			},
			OnStop: func(ctx context.Context) error {
				return n.Stop(ctx)
			},
		})
	}

	return n, nil
}
