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
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/internal/infrastructure/timesync"
	"crypto-bot/internal/infrastructure/watcher"
	infraws "crypto-bot/internal/infrastructure/ws"
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

type disabledStrategy struct{}

func (disabledStrategy) Flow() string { return "disabled" }
func (disabledStrategy) Enabled(config.SymbolConfig) bool {
	return false
}
func (disabledStrategy) Execute(context.Context, time.Time, fundingdomain.Candidate) error {
	return nil
}
func (disabledStrategy) CleanupOpenExposure(context.Context) error {
	return nil
}

type signalStrategy struct {
	ch chan<- struct{}
}

func (s signalStrategy) Flow() string { return "signal" }
func (s signalStrategy) Enabled(config.SymbolConfig) bool {
	return true
}
func (s signalStrategy) Execute(context.Context, time.Time, fundingdomain.Candidate) error {
	select {
	case s.ch <- struct{}{}:
	default:
	}
	return nil
}
func (s signalStrategy) CleanupOpenExposure(context.Context) error {
	return nil
}

type fakeFundingStoreSet struct {
	ticker   store.TickerReader
	contract store.ContractReader
	price    store.PriceReader
	funding  store.FundingReader
	depth    store.DepthReader
	kline    store.KlineReadWriter
}

func (f fakeFundingStoreSet) Start(context.Context) {}
func (f fakeFundingStoreSet) WaitReady(context.Context) error {
	return nil
}
func (f fakeFundingStoreSet) WireWS(*pkgws.Pool, infraws.ExchangeAdapter) {}
func (f fakeFundingStoreSet) Ticker() store.TickerReader                  { return f.ticker }
func (f fakeFundingStoreSet) Contract() store.ContractReader              { return f.contract }
func (f fakeFundingStoreSet) Price() store.PriceReader                    { return f.price }
func (f fakeFundingStoreSet) Funding() store.FundingReader                { return f.funding }
func (f fakeFundingStoreSet) Depth() store.DepthReader                    { return f.depth }
func (f fakeFundingStoreSet) Kline() store.KlineReadWriter                { return f.kline }

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

func TestSniperPublishCandidateSkipsMissingResourcesAndDisabledSymbols(t *testing.T) {
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
		stores:           map[string]fundingStoreSet{},
		disabled:         map[string]string{"BTC_USDT": "paused"},
		reversionFactory: sniperStrategyFactory,
		trapFactory:      sniperStrategyFactory,
		trailingFactory:  sniperStrategyFactory,
		notifier:         noOpNotifier{},
		log:              sniperTestLogger(),
	}

	baseLog := sniperTestLogger()
	published := s.publishCandidate(context.Background(), baseLog, config.SymbolConfig{
		Symbol:   "BTC_USDT",
		Exchange: "missing",
	})
	assert.False(t, published)

	published = s.publishCandidate(context.Background(), baseLog, config.SymbolConfig{
		Symbol:   "BTC_USDT",
		Exchange: "mexc",
	})
	assert.False(t, published)

	s.stores["mexc"] = app.NewCentralStore()
	reason, disabled := s.disabledReason("BTC_USDT")
	assert.True(t, disabled)
	assert.Equal(t, "paused", reason)
	published = s.publishCandidate(context.Background(), baseLog, config.SymbolConfig{
		Symbol:   "BTC_USDT",
		Exchange: "mexc",
	})
	assert.False(t, published)
}

func TestSniperRunPublishesOnceAndKeepsScannerJobAlive(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	tickers := mocks.NewMockTickerReader(ctrl)
	contracts := mocks.NewMockContractReader(ctrl)
	funding := mocks.NewMockFundingReader(ctrl)

	settle := time.Now().Add(time.Minute)
	tickers.EXPECT().GetTicker(gomock.Any(), "BTC_USDT").Return(&store.TickerData{
		Symbol:      "BTC_USDT",
		FundingRate: 0.01,
		LastPrice:   100,
		BestBid:     99,
		BestAsk:     101,
		Amount24:    100000,
	}, nil)
	contracts.EXPECT().GetContract(gomock.Any(), "BTC_USDT").Return(&store.ContractData{
		Symbol: "BTC_USDT",
	}, nil)
	funding.EXPECT().GetSettleTime(gomock.Any(), "BTC_USDT").Return(settle, nil)

	bus := eventbus.New(sniperTestLogger())
	t.Cleanup(func() { _ = bus.Close() })

	published := make(chan struct{}, 1)
	s := &Sniper{
		cfg: &config.Config{
			System: &config.SystemConfig{},
			Symbols: []config.SymbolConfig{
				{Symbol: "BTC_USDT", Exchange: "mexc"},
			},
		},
		engine: &app.Engine{
			Bus: bus,
			Providers: map[string]*app.ExchangeProvider{
				"mexc": {
					Name:     "mexc",
					Client:   client,
					TimeSync: timesync.New(client, time.Second),
				},
			},
		},
		stores: map[string]fundingStoreSet{
			"mexc": fakeFundingStoreSet{
				ticker:   tickers,
				contract: contracts,
				funding:  funding,
			},
		},
		disabled: make(map[string]string),
		reversionFactory: func(config.SymbolConfig, *config.Config, Deps) strategy.Strategy {
			return signalStrategy{ch: published}
		},
		trapFactory: func(config.SymbolConfig, *config.Config, Deps) strategy.Strategy {
			return disabledStrategy{}
		},
		trailingFactory: func(config.SymbolConfig, *config.Config, Deps) strategy.Strategy {
			return disabledStrategy{}
		},
		notifier: noOpNotifier{},
		log:      sniperTestLogger(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- s.Run(ctx)
	}()

	select {
	case <-published:
	case <-time.After(time.Second):
		t.Fatal("scan did not publish event")
	}

	select {
	case err := <-done:
		t.Fatalf("Run returned before shutdown: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	cancel()
	require.Eventually(t, func() bool {
		select {
		case err := <-done:
			require.NoError(t, err)
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)
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
		stores: map[string]fundingStoreSet{},
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
		stores: map[string]fundingStoreSet{"mexc": app.NewCentralStore()},
		log:    sniperTestLogger(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, s.RunAsBackground(ctx), context.Canceled)
}
