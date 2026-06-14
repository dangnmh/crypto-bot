package application

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"crypto-bot/internal/bots/funding/application/reversion"
	"crypto-bot/internal/bots/funding/application/strategy"
	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/bots/funding/domain"
	"crypto-bot/internal/infrastructure/app"
	sysconfig "crypto-bot/internal/infrastructure/config"
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

func sniperTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNewFundingBotBuildsExchangeScopedResources(t *testing.T) {
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
				TimeSync: timesync.New(client, slog.Default(), time.Second),
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

	s := NewFundingBot(
		cfg,
		sysCfg,
		engine,
		mocks.NewMockNotifier(ctrl),
		[]strategy.BackgroundStrategy{},
		sniperTestLogger(),
	)

	require.NotNil(t, s)
	assert.Contains(t, s.orderNotifiers, "mexc")
	assert.Contains(t, s.stores, "mexc")
	assert.NotContains(t, s.stores, "gate")
}

func TestConfiguredScanner_Scan(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	tickers := mocks.NewMockTickerReader(ctrl)
	contracts := mocks.NewMockContractReader(ctrl)
	fundings := mocks.NewMockFundingReader(ctrl)

	now := time.Now()

	tickers.EXPECT().GetTicker(gomock.Any(), "BTC_USDT").Return(&store.TickerData{
		Symbol:    "BTC_USDT",
		LastPrice: 100,
		BestBid:   99,
		BestAsk:   101,
		Amount24:  100000,
	}, nil).AnyTimes()

	contracts.EXPECT().GetContract(gomock.Any(), "BTC_USDT").Return(&store.ContractData{
		Symbol: "BTC_USDT",
	}, nil).AnyTimes()

	fundings.EXPECT().GetSettleTime(gomock.Any(), "BTC_USDT").Return(now.Add(30*time.Second), nil).AnyTimes()
	fundings.EXPECT().GetFunding(gomock.Any(), "BTC_USDT").Return(&store.FundingData{
		Symbol:      "BTC_USDT",
		FundingRate: 0.01,
	}, nil).AnyTimes()

	client := mocks.NewMockClient(ctrl)
	client.EXPECT().GetServerTime(gomock.Any()).Return(now.UnixMilli(), nil).AnyTimes()

	ts := timesync.New(client, slog.Default(), time.Second)
	ctxSync, cancelSync := context.WithCancel(context.Background())
	cancelSync()
	ts.Start(ctxSync)

	bus := eventbus.New(sniperTestLogger())
	t.Cleanup(func() { _ = bus.Close() })

	engine := &app.Engine{
		Bus: bus,
		Providers: map[string]*app.ExchangeProvider{
			"mexc": {
				Name:     "mexc",
				Client:   client,
				TimeSync: ts,
			},
		},
	}

	cfg := &config.Config{
		Symbols: []config.SymbolConfig{
			{Symbol: "BTC_USDT", Exchange: "mexc"},
		},
	}

	scanner := NewConfiguredScanner(
		cfg,
		engine,
		map[string]strategy.FundingStoreSet{
			"mexc": fakeFundingStoreSet{
				ticker:   tickers,
				contract: contracts,
				funding:  fundings,
			},
		},
		sniperTestLogger(),
		func(string) (string, bool) { return "", false },
	)

	opportunities, err := scanner.Scan(context.Background())
	require.NoError(t, err)
	require.Len(t, opportunities, 1)
	assert.Equal(t, "BTC_USDT", opportunities[0].Candidate.Symbol)
	assert.Equal(t, 0.01, opportunities[0].Candidate.FundingRate)
}

func TestConfiguredScanner_disabledSymbol(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Symbols: []config.SymbolConfig{
			{Symbol: "BTC_USDT", Exchange: "mexc"},
		},
	}

	scanner := NewConfiguredScanner(
		cfg,
		nil,
		nil,
		sniperTestLogger(),
		func(symbol string) (string, bool) {
			if symbol == "BTC_USDT" {
				return "paused", true
			}
			return "", false
		},
	)

	opportunities, err := scanner.Scan(context.Background())
	require.NoError(t, err)
	assert.Empty(t, opportunities)
}

type mockScanner struct {
	opportunities []ScanOpportunity
	err           error
	scanCalled    int
	mu            sync.Mutex
}

func (m *mockScanner) Scan(ctx context.Context) ([]ScanOpportunity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.scanCalled++
	return m.opportunities, m.err
}

func TestScannerJob_Run(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	bus := eventbus.New(sniperTestLogger())
	t.Cleanup(func() { _ = bus.Close() })

	engine := &app.Engine{
		Bus:       bus,
		Providers: map[string]*app.ExchangeProvider{},
	}

	now := time.Now()
	opp := ScanOpportunity{
		Candidate: domain.Candidate{
			Config: domain.TradeConfig{
				Symbol:   "BTC_USDT",
				Exchange: "mexc",
			},
			TradeIntent: domain.TradeIntent{
				Symbol:      "BTC_USDT",
				FundingRate: 0.01,
			},
		},
		SettleTime: now.Add(30 * time.Second),
	}

	mScanner := &mockScanner{
		opportunities: []ScanOpportunity{opp},
	}

	client := mocks.NewMockClient(ctrl)
	client.EXPECT().GetServerTime(gomock.Any()).Return(now.UnixMilli(), nil).AnyTimes()
	client.EXPECT().SupportLeverageOnOrder().Return(false).AnyTimes()
	ts := timesync.New(client, slog.Default(), time.Second)
	ctxSync, cancelSync := context.WithCancel(context.Background())
	cancelSync()
	ts.Start(ctxSync)

	engine.Providers["mexc"] = &app.ExchangeProvider{
		Name:     "mexc",
		Client:   client,
		TimeSync: ts,
	}

	job := NewScannerJob(
		[]Scanner{mScanner},
		engine,
		nil,
		sniperTestLogger(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	subCtx := t.Context()
	ch, err := bus.Subscribe(subCtx, reversion.TopicReversionCandidate)
	require.NoError(t, err)

	go func() {
		_ = job.Run(ctx)
	}()

	select {
	case msg := <-ch:
		require.NotNil(t, msg)
		msg.Ack()
	case <-time.After(3 * time.Second):
		t.Fatal("Timeout waiting for reversion candidate event")
	}

	mScanner.mu.Lock()
	assert.GreaterOrEqual(t, mScanner.scanCalled, 1)
	mScanner.mu.Unlock()
}

func TestFundingBot_disabledReason(t *testing.T) {
	t.Parallel()

	s := &FundingBot{
		disabled: map[string]string{"BTC_USDT": "paused"},
	}

	reason, disabled := s.disabledReason("BTC_USDT")
	assert.True(t, disabled)
	assert.Equal(t, "paused", reason)

	_, disabled = s.disabledReason("ETH_USDT")
	assert.False(t, disabled)
}
