package application

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"crypto-bot/internal/bots/funding/application/strategy"
	"crypto-bot/internal/bots/funding/config"
	fundingdomain "crypto-bot/internal/bots/funding/domain"
	"crypto-bot/internal/infrastructure/app"
	sysconfig "crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/notifier"
	"crypto-bot/internal/infrastructure/timesync"
	"crypto-bot/internal/infrastructure/watcher"
	"crypto-bot/internal/testutil/mocks"
	"crypto-bot/pkg/eventbus"
	"crypto-bot/pkg/types"
	pkgws "crypto-bot/pkg/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type noOpStrategy struct{}

func (noOpStrategy) Flow() string { return "noop" }
func (noOpStrategy) Enabled(config.SymbolConfig) bool {
	return true
}
func (noOpStrategy) Execute(context.Context, time.Time, fundingdomain.Candidate) error {
	return nil
}
func (noOpStrategy) CleanupOpenExposure(context.Context) error {
	return nil
}

type noOpNotifier struct{}

func (noOpNotifier) Send(context.Context, notifier.Event) error { return nil }
func (noOpNotifier) Start(context.Context) error                { return nil }
func (noOpNotifier) Stop(context.Context) error                 { return nil }

func sniperTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func sniperStrategyFactory(config.SymbolConfig, *config.Config, Deps) strategy.Strategy {
	return noOpStrategy{}
}

func TestNewSniperBuildsExchangeScopedResources(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	bus := eventbus.New(sniperTestLogger())
	t.Cleanup(func() { _ = bus.Close() })
	engine := &app.Engine{
		Bus: bus,
		Providers: map[string]*app.ExchangeProvider{
			"mexc": {
				Name:     "mexc",
				Client:   client,
				Watcher:  watcher.NewOrderWatcher(bus, "mexc", sniperTestLogger()),
				TimeSync: timesync.New(client, time.Second),
			},
		},
	}
	cfg := &config.Config{Symbols: []config.SymbolConfig{
		{Symbol: "BTC_USDT", Exchange: "mexc"},
		{Symbol: "ETH_USDT", Exchange: "gate"},
	}}
	sysCfg := &config.SystemConfig{Sync: config.SyncConfig{
		SyncConfig:  sysconfig.SyncConfig{Ticker: types.Duration(time.Second), Contract: types.Duration(time.Second)},
		FundingSync: types.Duration(time.Second),
	}}

	s := NewSniper(
		cfg,
		sysCfg,
		engine,
		noOpNotifier{},
		sniperStrategyFactory,
		sniperStrategyFactory,
		sniperStrategyFactory,
		sniperTestLogger(),
	)

	require.NotNil(t, s)
	assert.Contains(t, s.orderNotifiers, "mexc")
	assert.Contains(t, s.stores, "mexc")
	assert.NotContains(t, s.stores, "gate")
}

func TestSniperWorkerSkipsMissingResourcesAndDisabledSymbols(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	bus := eventbus.New(sniperTestLogger())
	t.Cleanup(func() { _ = bus.Close() })

	engine := &app.Engine{
		Bus: bus,
		Providers: map[string]*app.ExchangeProvider{
			"mexc": {
				Name:     "mexc",
				Client:   client,
				TimeSync: timesync.New(client, time.Second),
			},
		},
	}
	s := &Sniper{
		cfg:              &config.Config{},
		engine:           engine,
		stores:           map[string]*app.CentralStore{},
		disabled:         map[string]string{"BTC_USDT": "paused"},
		reversionFactory: sniperStrategyFactory,
		trapFactory:      sniperStrategyFactory,
		trailingFactory:  sniperStrategyFactory,
		notifier:         noOpNotifier{},
		log:              sniperTestLogger(),
	}

	require.NoError(t, s.spawnWorker(context.Background(), config.SymbolConfig{
		Symbol:   "BTC_USDT",
		Exchange: "missing",
	})())

	require.NoError(t, s.spawnWorker(context.Background(), config.SymbolConfig{
		Symbol:   "BTC_USDT",
		Exchange: "mexc",
	})())

	s.stores["mexc"] = app.NewCentralStore()
	reason, disabled := s.disabledReason("BTC_USDT")
	assert.True(t, disabled)
	assert.Equal(t, "paused", reason)
	require.NoError(t, s.spawnWorker(context.Background(), config.SymbolConfig{
		Symbol:   "BTC_USDT",
		Exchange: "mexc",
	})())
}

func TestSniperRunAsBackgroundSkipsProvidersWithoutStores(t *testing.T) {
	t.Parallel()

	bus := eventbus.New(sniperTestLogger())
	t.Cleanup(func() { _ = bus.Close() })
	s := &Sniper{
		engine: &app.Engine{
			Bus: bus,
			Providers: map[string]*app.ExchangeProvider{
				"mexc": {Name: "mexc"},
			},
		},
		stores: map[string]*app.CentralStore{},
		log:    sniperTestLogger(),
	}

	require.NoError(t, s.RunAsBackground(context.Background()))
}

func TestSniperRunAsBackgroundReturnsTimeSyncReadinessError(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	client.EXPECT().WarmUp(gomock.Any(), 4*time.Second).AnyTimes()
	client.EXPECT().GetServerTime(gomock.Any()).Return(int64(0), context.Canceled).AnyTimes()

	bus := eventbus.New(sniperTestLogger())
	t.Cleanup(func() { _ = bus.Close() })
	s := &Sniper{
		engine: &app.Engine{
			Bus: bus,
			Providers: map[string]*app.ExchangeProvider{
				"mexc": {
					Name:     "mexc",
					Client:   client,
					TimeSync: timesync.New(client, time.Millisecond),
					WS:       pkgws.NewPool("ws://127.0.0.1:1", 1, sniperTestLogger()),
				},
			},
		},
		stores: map[string]*app.CentralStore{"mexc": app.NewCentralStore()},
		log:    sniperTestLogger(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, s.RunAsBackground(ctx), context.Canceled)
}
