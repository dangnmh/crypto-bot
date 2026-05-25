package application_test

import (
	"context"
	"log/slog"
	"testing"

	"crypto-bot/internal/bots/funding/application"
	"crypto-bot/internal/bots/funding/application/reversion"
	"crypto-bot/internal/bots/funding/application/strategy"
	"crypto-bot/internal/bots/funding/application/trailing"
	"crypto-bot/internal/bots/funding/application/trap"
	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/testutil/mocks"
	"crypto-bot/pkg/eventbus"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestNewSniper(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Symbols: []config.SymbolConfig{
			{Symbol: "BTC_USDT"},
		},
	}
	sysCfg := &config.SystemConfig{}
	engine := &app.Engine{
		Bus: eventbus.New(slog.Default()),
	}

	ctrl := gomock.NewController(t)
	var reversionFactory application.ReversionStrategyFactory = func(cfg config.SymbolConfig, global *config.Config, deps application.Deps) strategy.Strategy {
		return reversion.NewStrategy(cfg, global, deps)
	}
	var trapFactory application.TrapStrategyFactory = func(cfg config.SymbolConfig, global *config.Config, deps application.Deps) strategy.Strategy {
		return trap.NewStrategy(cfg, global, deps)
	}
	var trailingFactory application.TrailingStrategyFactory = func(cfg config.SymbolConfig, global *config.Config, deps application.Deps) strategy.Strategy {
		return trailing.NewStrategy(cfg, global, deps)
	}

	sniper := application.NewSniper(
		cfg, sysCfg, engine,
		mocks.NewMockNotifier(ctrl),
		reversionFactory, trapFactory, trailingFactory,
		slog.Default(),
	)
	require.NotNil(t, sniper)
}

func TestSniper_Stop(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	sysCfg := &config.SystemConfig{}
	engine := &app.Engine{
		Bus: eventbus.New(slog.Default()),
	}

	ctrl := gomock.NewController(t)
	m := mocks.NewMockNotifier(ctrl)

	var reversionFactory application.ReversionStrategyFactory = func(cfg config.SymbolConfig, global *config.Config, deps application.Deps) strategy.Strategy {
		return reversion.NewStrategy(cfg, global, deps)
	}
	var trapFactory application.TrapStrategyFactory = func(cfg config.SymbolConfig, global *config.Config, deps application.Deps) strategy.Strategy {
		return trap.NewStrategy(cfg, global, deps)
	}
	var trailingFactory application.TrailingStrategyFactory = func(cfg config.SymbolConfig, global *config.Config, deps application.Deps) strategy.Strategy {
		return trailing.NewStrategy(cfg, global, deps)
	}

	sniper := application.NewSniper(
		cfg, sysCfg, engine, m,
		reversionFactory, trapFactory, trailingFactory,
		slog.Default(),
	)
	err := sniper.Stop(context.Background())
	assert.NoError(t, err)
}

func TestSniper_Run_CancelledContext(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Symbols: []config.SymbolConfig{
			{Symbol: "BTC_USDT"},
		},
	}
	sysCfg := &config.SystemConfig{}
	engine := &app.Engine{
		Bus: eventbus.New(slog.Default()),
	}

	ctrl := gomock.NewController(t)
	var reversionFactory application.ReversionStrategyFactory = func(cfg config.SymbolConfig, global *config.Config, deps application.Deps) strategy.Strategy {
		return reversion.NewStrategy(cfg, global, deps)
	}
	var trapFactory application.TrapStrategyFactory = func(cfg config.SymbolConfig, global *config.Config, deps application.Deps) strategy.Strategy {
		return trap.NewStrategy(cfg, global, deps)
	}
	var trailingFactory application.TrailingStrategyFactory = func(cfg config.SymbolConfig, global *config.Config, deps application.Deps) strategy.Strategy {
		return trailing.NewStrategy(cfg, global, deps)
	}

	sniper := application.NewSniper(
		cfg, sysCfg, engine,
		mocks.NewMockNotifier(ctrl),
		reversionFactory, trapFactory, trailingFactory,
		slog.Default(),
	)

	// Run it with a cancelled context so it exits immediately after spawning workers.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := sniper.Run(ctx)
	assert.NoError(t, err)
}
