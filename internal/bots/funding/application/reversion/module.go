package reversion

import (
	"log/slog"

	fundingconfig "crypto-bot/internal/bots/funding/config"
	infraapp "crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/notifier"

	"github.com/patrickmn/go-cache"
	"go.uber.org/fx"
)

// Module wires reversion strategy dependencies.
var Module = fx.Options(
	fx.Provide(
		ProvideReversionStrategy,
	),
)

// ProvideReversionStrategy provides a reversion Strategy instance.
func ProvideReversionStrategy(
	engine *infraapp.Engine,
	cfg *fundingconfig.Config,
	n notifier.Notifier,
	c *cache.Cache,
	log *slog.Logger,
) *Strategy {
	return NewStrategy(engine, cfg, n, c, log)
}
