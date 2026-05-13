package application_test

import (
	"context"
	"log/slog"
	"testing"

	"crypto-bot/internal/bots/funding/application"
	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/infrastructure/app"
	"crypto-bot/pkg/eventbus"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	sniper := application.NewSniper(cfg, sysCfg, engine, slog.Default())
	require.NotNil(t, sniper)
}

func TestSniper_Stop(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	sysCfg := &config.SystemConfig{}
	engine := &app.Engine{
		Bus: eventbus.New(slog.Default()),
	}

	sniper := application.NewSniper(cfg, sysCfg, engine, slog.Default())
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

	sniper := application.NewSniper(cfg, sysCfg, engine, slog.Default())

	// Run it with a cancelled context so it exits immediately after spawning workers.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := sniper.Run(ctx)
	assert.NoError(t, err)
}
