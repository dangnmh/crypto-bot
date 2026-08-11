package application_test

import (
	"context"
	"log/slog"
	"testing"

	"crypto-bot/internal/bots/funding/application"
	"crypto-bot/internal/bots/funding/application/reversion"
	"crypto-bot/internal/bots/funding/application/strategy"
	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/testutil/mocks"
	"crypto-bot/pkg/eventbus"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestNewFundingBot(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Reversion: &config.ReversionConfig{},
		Symbols:   []config.SymbolConfig{},
	}
	sysCfg := &config.SystemConfig{}
	engine := &app.Engine{
		Bus: eventbus.New(slog.Default()),
	}

	ctrl := gomock.NewController(t)
	m := mocks.NewMockNotifier(ctrl)

	revStrat := reversion.NewStrategy(engine, cfg, m, nil, nil, slog.Default())

	bot := application.NewFundingBot(
		cfg, sysCfg, engine, m,
		[]strategy.BackgroundStrategy{revStrat},
		slog.Default(),
	)
	require.NotNil(t, bot)
}

func TestFundingBot_Stop(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Reversion: &config.ReversionConfig{},
	}
	sysCfg := &config.SystemConfig{}
	engine := &app.Engine{
		Bus: eventbus.New(slog.Default()),
	}

	ctrl := gomock.NewController(t)
	m := mocks.NewMockNotifier(ctrl)

	revStrat := reversion.NewStrategy(engine, cfg, m, nil, nil, slog.Default())

	bot := application.NewFundingBot(
		cfg, sysCfg, engine, m,
		[]strategy.BackgroundStrategy{revStrat},
		slog.Default(),
	)
	err := bot.Stop(context.Background())
	assert.NoError(t, err)
}

func TestFundingBot_Run_CancelledContext(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Reversion: &config.ReversionConfig{},
		Symbols:   []config.SymbolConfig{},
	}
	sysCfg := &config.SystemConfig{}
	engine := &app.Engine{
		Bus: eventbus.New(slog.Default()),
	}

	ctrl := gomock.NewController(t)
	m := mocks.NewMockNotifier(ctrl)

	revStrat := reversion.NewStrategy(engine, cfg, m, nil, nil, slog.Default())

	bot := application.NewFundingBot(
		cfg, sysCfg, engine, m,
		[]strategy.BackgroundStrategy{revStrat},
		slog.Default(),
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := bot.Run(ctx)
	assert.NoError(t, err)
}

func TestNewFundingBot_WithBlacklist(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Reversion: &config.ReversionConfig{},
		Symbols: []config.SymbolConfig{
			{Symbol: "BTC_USDT", Exchange: "mexc", MarginUSDT: 100, Leverage: 5},
			{Symbol: "ETH_USDT", Exchange: "mexc", MarginUSDT: 100, Leverage: 5},
		},
		Blacklist: &config.BlacklistConfig{
			"common": []string{"ETH_USDT"},
		},
	}
	sysCfg := &config.SystemConfig{}
	engine := &app.Engine{
		Bus: eventbus.New(slog.Default()),
		Providers: map[string]*app.ExchangeProvider{
			"mexc": {
				Name: "mexc",
			},
		},
	}

	ctrl := gomock.NewController(t)
	m := mocks.NewMockNotifier(ctrl)

	revStrat := reversion.NewStrategy(engine, cfg, m, nil, nil, slog.Default())

	bot := application.NewFundingBot(
		cfg, sysCfg, engine, m,
		[]strategy.BackgroundStrategy{revStrat},
		slog.Default(),
	)
	require.NotNil(t, bot)
}
