package reversion_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	"crypto-bot/internal/bots/funding/application/orders"
	"crypto-bot/internal/bots/funding/application/reversion"
	"crypto-bot/internal/bots/funding/application/strategy"
	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/bots/funding/domain"
	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	infraws "crypto-bot/internal/infrastructure/ws"
	"crypto-bot/internal/testutil/mocks"
	ordermanager "crypto-bot/internal/trading/ordermanager"
	"crypto-bot/pkg/eventbus"
	"crypto-bot/pkg/types"
	pkgws "crypto-bot/pkg/ws"

	"github.com/ThreeDotsLabs/watermill"
	cache "github.com/patrickmn/go-cache"
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
func (f fakeFundingStoreSet) WireWS(*pkgws.Pool, infraws.ExchangeAdapterParser) {}
func (f fakeFundingStoreSet) Ticker() store.TickerReader                        { return f.ticker }
func (f fakeFundingStoreSet) Contract() store.ContractReader                    { return f.contract }
func (f fakeFundingStoreSet) Price() store.PriceReader                          { return f.price }
func (f fakeFundingStoreSet) Funding() store.FundingReader                      { return f.funding }
func (f fakeFundingStoreSet) Depth() store.DepthReader                          { return f.depth }
func (f fakeFundingStoreSet) Kline() store.KlineReadWriter                      { return f.kline }

type fakeExchangeAdapter struct {
	*mocks.MockSubscriber
}

func (f *fakeExchangeAdapter) SetPool(pool *pkgws.Pool)                                 {}
func (f *fakeExchangeAdapter) GetPingConfig() (payload any, interval time.Duration)     { return nil, 0 }
func (f *fakeExchangeAdapter) GetAuthHook(apiKey, apiSecret string) func(*pkgws.Client) { return nil }
func (f *fakeExchangeAdapter) GetChannelExtractor() func([]byte) string                 { return nil }
func (f *fakeExchangeAdapter) ParseTicker(data []byte) (symbol string, pd *store.PriceData, err error) {
	return "", nil, nil
}
func (f *fakeExchangeAdapter) ParsePosition(data []byte) (*exchange.PersonalPositionUpdate, error) {
	return nil, nil
}
func (f *fakeExchangeAdapter) ParseDepth(data []byte) (string, *shared.OrderBook, error) {
	return "", nil, nil
}
func (f *fakeExchangeAdapter) Subscribe(ctx context.Context, topic, flowID string, subMsg any) error {
	return nil
}
func (f *fakeExchangeAdapter) Unsubscribe(ctx context.Context, topic, flowID string, unsubMsg any) error {
	return nil
}
func (f *fakeExchangeAdapter) UnsubscribePersonal(ctx context.Context) error {
	return nil
}

func executeReversionHelper(t *testing.T, bus *eventbus.Bus, reqID string, candidate domain.Candidate, settleTime time.Time) error {
	candidate.ExternalID = orders.ExternalOrderID(candidate.Symbol, settleTime, candidate.Config.Exchange)

	startEvt := reversion.CandidateFoundEvent{
		Flow:       reversion.FlowIDFundingReversion,
		ReqID:      reqID,
		Symbol:     candidate.Symbol,
		Exchange:   candidate.Config.Exchange,
		Timestamp:  time.Now(),
		EventID:    watermill.NewUUID(),
		Seq:        1,
		Topic:      reversion.TopicReversionCandidate,
		ExternalID: candidate.ExternalID,
		SettleTime: settleTime,
		Candidate:  candidate,
	}

	return bus.Publish(reversion.TopicReversionCandidate, startEvt)
}

func TestStrategy_Execute_Success(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockClient(ctrl)
	mockClient.EXPECT().SupportLeverageOnOrder().Return(false).AnyTimes()
	mockWs := mocks.NewMockSubscriber(ctrl)
	mockTickerStore := mocks.NewMockTickerReader(ctrl)
	mockContractStore := mocks.NewMockContractReader(ctrl)
	mockPriceStore := mocks.NewMockPriceReader(ctrl)
	mockNotifier := mocks.NewMockNotifier(ctrl)
	mockFundingStore := mocks.NewMockFundingReader(ctrl)

	// Set up Clock
	mockClock := mocks.NewMockClock(ctrl)
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	currentNow := now
	mockClock.EXPECT().Now().DoAndReturn(func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return currentNow
	}).AnyTimes()
	mockClock.EXPECT().GetServerTime().DoAndReturn(func() int64 {
		mu.Lock()
		defer mu.Unlock()
		return currentNow.UnixMilli()
	}).AnyTimes()
	mockClock.EXPECT().LatencyMs().Return(int64(20)).AnyTimes()
	mockClock.EXPECT().Offset().Return(int64(0)).AnyTimes()
	mockClock.EXPECT().Until(gomock.Any()).DoAndReturn(func(target time.Time) time.Duration {
		mu.Lock()
		defer mu.Unlock()
		return target.Sub(currentNow)
	}).AnyTimes()
	mockClock.EXPECT().Sleep(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, d time.Duration) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if d >= 50*time.Millisecond {
			time.Sleep(30 * time.Millisecond)
		}
		mu.Lock()
		currentNow = currentNow.Add(d)
		mu.Unlock()
		return nil
	}).AnyTimes()

	bus := eventbus.New(slog.Default())
	defer func() { _ = bus.Close() }()

	engine := &app.Engine{
		Bus: bus,
		Providers: map[string]*app.ExchangeProvider{
			"mexc": {
				Name:    "mexc",
				Client:  mockClient,
				Adapter: infraws.NewExchangeManagerAdapter(&fakeExchangeAdapter{MockSubscriber: mockWs}),
			},
		},
	}

	cfg := config.SymbolConfig{
		Symbol:   "BTC_USDT",
		Exchange: "mexc",
		FundingReversion: domain.FundingReversionConfig{
			Enabled:           true,
			PostSettleTimeout: types.Duration(10 * time.Second),
			MaxLatency:        types.Duration(100 * time.Millisecond),
			BufferTime:        0,
		},
	}

	globalCfg := &config.Config{
		System: &config.SystemConfig{},
		Reversion: &config.ReversionConfig{
			Default: config.ExchangeReversionConfig{
				MinVol24USD: 10000,
			},
			Safety: config.SafetyConfig{
				MaxImpactRatio: 1.0,
			},
		},
		Symbols: []config.SymbolConfig{cfg},
	}

	candidate := domain.Candidate{
		Config: domain.TradeConfig{
			Symbol:      "BTC_USDT",
			Exchange:    "mexc",
			MinVol24USD: 10000,
		},
		Symbol:       "BTC_USDT",
		FundingRate:  0.001,
		Side:         shared.SideOpenLong,
		CloseSide:    shared.SideCloseLong,
		PriceUnit:    0.01,
		VolUnit:      1,
		MinVol:       1,
		PriceScale:   2,
		VolScale:     4,
		ContractSize: 0.001,
		TakerFeeRate: 0.0006,
		MakerFeeRate: 0.0002,
		LastPrice:    60000.0,
		BestBid:      59990.0,
		BestAsk:      60000.0,
		Volume24:     1000,
		Vol24USDT:    60000000,
	}

	// 1. Arm expectations
	mockPriceStore.EXPECT().SubscribePrice(gomock.Any(), "BTC_USDT").Return(nil)
	mockPriceStore.EXPECT().GetPrice(gomock.Any(), "BTC_USDT", gomock.Any()).Return(&store.PriceData{
		BestBid:   59990.0,
		BestAsk:   60000.0,
		LastPrice: 60000.0,
	}, nil).AnyTimes()
	mockContractStore.EXPECT().GetContract(gomock.Any(), "BTC_USDT").Return(&store.ContractData{
		Symbol:       "BTC_USDT",
		ContractSize: 0.001,
	}, nil).AnyTimes()

	// 2. Recheck expectations
	mockClient.EXPECT().GetFundingRates(gomock.Any(), []string{"BTC_USDT"}).Return([]exchange.FundingRateResult{
		{Symbol: "BTC_USDT", Rate: 0.001},
	}, nil)

	// 3. FireIOC expectations
	mockClient.EXPECT().SwitchMarginMode(gomock.Any(), "BTC_USDT", shared.MarginModeIsolated, 0, gomock.Any()).Return(nil)

	// 4. Notifier expectations
	mockNotifier.EXPECT().Send(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	// Subscribe to OrderIntent to verify OrderManager dispatch
	intentSub, err := bus.Subscribe(context.Background(), ordermanager.TopicOrderIntent)
	require.NoError(t, err)

	c := cache.New(5*time.Minute, 10*time.Minute)
	strategyInst := reversion.NewStrategy(engine, globalCfg, mockNotifier, c, slog.Default())
	strategyInst.SetTestFallbacks(mockClock, nil, infraws.NewExchangeManagerAdapter(nil))

	stores := map[string]strategy.FundingStoreSet{
		"mexc": fakeFundingStoreSet{
			ticker:   mockTickerStore,
			contract: mockContractStore,
			price:    mockPriceStore,
			funding:  mockFundingStore,
		},
	}
	err = strategyInst.Start(context.Background(), stores)
	require.NoError(t, err)

	err = executeReversionHelper(t, bus, "req_success_1", candidate, now.Add(10*time.Second))
	assert.NoError(t, err)

	// Wait for OrderIntent to be dispatched
	select {
	case msg := <-intentSub:
		var intent ordermanager.OrderIntentEvent
		require.NoError(t, json.Unmarshal(msg.Payload, &intent))
		assert.Equal(t, "req_success_1", intent.ReqID)
		assert.Equal(t, "BTC_USDT", intent.Symbol)
		assert.Equal(t, ordermanager.StrategyFundingReversion, intent.StrategyType)
		assert.Greater(t, intent.Price, 0.0)
		msg.Ack()
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for TopicOrderIntent")
	}
}

func TestStrategy_Execute_ExternalID_Propagation(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockClient(ctrl)
	mockClient.EXPECT().SupportLeverageOnOrder().Return(false).AnyTimes()
	mockWs := mocks.NewMockSubscriber(ctrl)
	mockTickerStore := mocks.NewMockTickerReader(ctrl)
	mockContractStore := mocks.NewMockContractReader(ctrl)
	mockPriceStore := mocks.NewMockPriceReader(ctrl)
	mockNotifier := mocks.NewMockNotifier(ctrl)
	mockClock := mocks.NewMockClock(ctrl)
	mockFundingStore := mocks.NewMockFundingReader(ctrl)

	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	currentNow := now
	mockClock.EXPECT().Now().DoAndReturn(func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return currentNow
	}).AnyTimes()
	mockClock.EXPECT().GetServerTime().DoAndReturn(func() int64 {
		mu.Lock()
		defer mu.Unlock()
		return currentNow.UnixMilli()
	}).AnyTimes()
	mockClock.EXPECT().LatencyMs().Return(int64(20)).AnyTimes()
	mockClock.EXPECT().Offset().Return(int64(0)).AnyTimes()
	mockClock.EXPECT().Until(gomock.Any()).DoAndReturn(func(target time.Time) time.Duration {
		mu.Lock()
		defer mu.Unlock()
		return target.Sub(currentNow)
	}).AnyTimes()
	mockClock.EXPECT().Sleep(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, d time.Duration) error {
		mu.Lock()
		currentNow = currentNow.Add(d)
		mu.Unlock()
		return nil
	}).AnyTimes()

	bus := eventbus.New(slog.Default())
	defer func() { _ = bus.Close() }()

	engine := &app.Engine{
		Bus: bus,
		Providers: map[string]*app.ExchangeProvider{
			"mexc": {
				Name:    "mexc",
				Client:  mockClient,
				Adapter: infraws.NewExchangeManagerAdapter(&fakeExchangeAdapter{MockSubscriber: mockWs}),
			},
		},
	}

	cfg := config.SymbolConfig{
		Symbol:   "BTC_USDT",
		Exchange: "mexc",
		FundingReversion: domain.FundingReversionConfig{
			Enabled:           true,
			PostSettleTimeout: types.Duration(10 * time.Second),
			MaxLatency:        types.Duration(100 * time.Millisecond),
			BufferTime:        0,
		},
	}

	globalCfg := &config.Config{
		System: &config.SystemConfig{},
		Reversion: &config.ReversionConfig{
			Default: config.ExchangeReversionConfig{
				MinVol24USD: 10000,
			},
			Safety: config.SafetyConfig{
				MaxImpactRatio: 1.0,
			},
		},
		Symbols: []config.SymbolConfig{cfg},
	}

	candidate := domain.Candidate{
		Config: domain.TradeConfig{
			Symbol:      "BTC_USDT",
			Exchange:    "mexc",
			MinVol24USD: 10000,
		},
		Symbol:       "BTC_USDT",
		FundingRate:  0.001,
		Side:         shared.SideOpenLong,
		CloseSide:    shared.SideCloseLong,
		PriceUnit:    0.01,
		VolUnit:      1,
		MinVol:       1,
		PriceScale:   2,
		VolScale:     4,
		ContractSize: 0.001,
		TakerFeeRate: 0.0006,
		MakerFeeRate: 0.0002,
		LastPrice:    60000.0,
		BestBid:      59990.0,
		BestAsk:      60000.0,
		Volume24:     1000,
		Vol24USDT:    60000000,
	}

	mockPriceStore.EXPECT().SubscribePrice(gomock.Any(), "BTC_USDT").Return(nil)
	mockPriceStore.EXPECT().GetPrice(gomock.Any(), "BTC_USDT", gomock.Any()).Return(&store.PriceData{
		BestBid:   59990.0,
		BestAsk:   60000.0,
		LastPrice: 60000.0,
	}, nil).AnyTimes()
	mockContractStore.EXPECT().GetContract(gomock.Any(), "BTC_USDT").Return(&store.ContractData{
		Symbol:       "BTC_USDT",
		ContractSize: 0.001,
	}, nil).AnyTimes()

	mockClient.EXPECT().GetFundingRates(gomock.Any(), []string{"BTC_USDT"}).Return([]exchange.FundingRateResult{
		{Symbol: "BTC_USDT", Rate: 0.001},
	}, nil)

	mockClient.EXPECT().SwitchMarginMode(gomock.Any(), "BTC_USDT", shared.MarginModeIsolated, 0, gomock.Any()).Return(nil)
	mockNotifier.EXPECT().Send(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	// Capture and verify ExternalID on event bus for the first event (CandidateFoundEvent)
	candidateChan, err := bus.Subscribe(context.Background(), reversion.TopicReversionCandidate)
	require.NoError(t, err)

	intentSub, err := bus.Subscribe(context.Background(), ordermanager.TopicOrderIntent)
	require.NoError(t, err)

	var candidateEvt reversion.CandidateFoundEvent
	var wg sync.WaitGroup
	wg.Go(func() {
		msg := <-candidateChan
		_ = json.Unmarshal(msg.Payload, &candidateEvt)
		msg.Ack()
	})

	c := cache.New(5*time.Minute, 10*time.Minute)
	strategyInst := reversion.NewStrategy(engine, globalCfg, mockNotifier, c, slog.Default())
	strategyInst.SetTestFallbacks(mockClock, nil, infraws.NewExchangeManagerAdapter(nil))

	stores := map[string]strategy.FundingStoreSet{
		"mexc": fakeFundingStoreSet{
			ticker:   mockTickerStore,
			contract: mockContractStore,
			price:    mockPriceStore,
			funding:  mockFundingStore,
		},
	}
	err = strategyInst.Start(context.Background(), stores)
	require.NoError(t, err)

	err = executeReversionHelper(t, bus, "req_prop_1", candidate, now.Add(10*time.Second))
	assert.NoError(t, err)

	wg.Wait()

	select {
	case msg := <-intentSub:
		var intent ordermanager.OrderIntentEvent
		require.NoError(t, json.Unmarshal(msg.Payload, &intent))
		msg.Ack()
		assert.Equal(t, candidateEvt.ExternalID, intent.ClientOrderID)
		assert.Equal(t, "req_prop_1", intent.ReqID)
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for TopicOrderIntent")
	}

	assert.NotEmpty(t, candidateEvt.ExternalID)
	expectedID := orders.ExternalOrderID(candidate.Symbol, now.Add(10*time.Second), candidate.Config.Exchange)
	assert.Equal(t, expectedID, candidateEvt.ExternalID)
	assert.LessOrEqual(t, len(candidateEvt.ExternalID), 32)
}

func TestStrategy_Execute_SkipLeverageChange(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockClient(ctrl)
	mockClient.EXPECT().SupportLeverageOnOrder().Return(true).AnyTimes()
	mockWs := mocks.NewMockSubscriber(ctrl)
	mockTickerStore := mocks.NewMockTickerReader(ctrl)
	mockContractStore := mocks.NewMockContractReader(ctrl)
	mockPriceStore := mocks.NewMockPriceReader(ctrl)
	mockNotifier := mocks.NewMockNotifier(ctrl)
	mockFundingStore := mocks.NewMockFundingReader(ctrl)

	// Set up Clock
	mockClock := mocks.NewMockClock(ctrl)
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	currentNow := now
	mockClock.EXPECT().Now().DoAndReturn(func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return currentNow
	}).AnyTimes()
	mockClock.EXPECT().GetServerTime().DoAndReturn(func() int64 {
		mu.Lock()
		defer mu.Unlock()
		return currentNow.UnixMilli()
	}).AnyTimes()
	mockClock.EXPECT().LatencyMs().Return(int64(20)).AnyTimes()
	mockClock.EXPECT().Offset().Return(int64(0)).AnyTimes()
	mockClock.EXPECT().Until(gomock.Any()).DoAndReturn(func(target time.Time) time.Duration {
		mu.Lock()
		defer mu.Unlock()
		return target.Sub(currentNow)
	}).AnyTimes()
	mockClock.EXPECT().Sleep(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, d time.Duration) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		mu.Lock()
		currentNow = currentNow.Add(d)
		mu.Unlock()
		return nil
	}).AnyTimes()

	bus := eventbus.New(slog.Default())
	defer func() { _ = bus.Close() }()

	engine := &app.Engine{
		Bus: bus,
		Providers: map[string]*app.ExchangeProvider{
			"bybit": {
				Name:    "bybit",
				Client:  mockClient,
				Adapter: infraws.NewExchangeManagerAdapter(&fakeExchangeAdapter{MockSubscriber: mockWs}),
			},
		},
	}

	cfg := config.SymbolConfig{
		Symbol:   "BTC_USDT",
		Exchange: "bybit",
		Leverage: 10,
		FundingReversion: domain.FundingReversionConfig{
			Enabled:           true,
			PostSettleTimeout: types.Duration(10 * time.Second),
			MaxLatency:        types.Duration(100 * time.Millisecond),
			BufferTime:        0,
		},
	}

	globalCfg := &config.Config{
		System: &config.SystemConfig{},
		Reversion: &config.ReversionConfig{
			Default: config.ExchangeReversionConfig{
				MinVol24USD: 10000,
			},
			Safety: config.SafetyConfig{
				MaxImpactRatio: 1.0,
			},
		},
		Symbols: []config.SymbolConfig{cfg},
	}

	candidate := domain.Candidate{
		Config: domain.TradeConfig{
			Symbol:      "BTC_USDT",
			Exchange:    "bybit",
			MinVol24USD: 10000,
			Leverage:    10,
		},
		Symbol:       "BTC_USDT",
		FundingRate:  0.001,
		Side:         shared.SideOpenLong,
		CloseSide:    shared.SideCloseLong,
		PriceUnit:    0.01,
		VolUnit:      1,
		MinVol:       1,
		PriceScale:   2,
		VolScale:     4,
		ContractSize: 0.001,
		TakerFeeRate: 0.0006,
		MakerFeeRate: 0.0002,
		LastPrice:    60000.0,
		BestBid:      59990.0,
		BestAsk:      60000.0,
		Volume24:     1000,
		Vol24USDT:    60000000,
	}

	mockPriceStore.EXPECT().SubscribePrice(gomock.Any(), "BTC_USDT").Return(nil)
	mockPriceStore.EXPECT().GetPrice(gomock.Any(), "BTC_USDT", gomock.Any()).Return(&store.PriceData{
		BestBid:   59990.0,
		BestAsk:   60000.0,
		LastPrice: 60000.0,
	}, nil).AnyTimes()
	mockContractStore.EXPECT().GetContract(gomock.Any(), "BTC_USDT").Return(&store.ContractData{
		Symbol:       "BTC_USDT",
		ContractSize: 0.001,
	}, nil).AnyTimes()

	mockClient.EXPECT().GetFundingRates(gomock.Any(), []string{"BTC_USDT"}).Return([]exchange.FundingRateResult{
		{Symbol: "BTC_USDT", Rate: 0.001},
	}, nil)

	mockClient.EXPECT().SwitchMarginMode(gomock.Any(), "BTC_USDT", shared.MarginModeIsolated, 10, gomock.Any()).Return(nil)
	mockNotifier.EXPECT().Send(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	intentSub, err := bus.Subscribe(context.Background(), ordermanager.TopicOrderIntent)
	require.NoError(t, err)

	c := cache.New(5*time.Minute, 10*time.Minute)
	strategyInst := reversion.NewStrategy(engine, globalCfg, mockNotifier, c, slog.Default())
	strategyInst.SetTestFallbacks(mockClock, nil, infraws.NewExchangeManagerAdapter(nil))

	stores := map[string]strategy.FundingStoreSet{
		"bybit": fakeFundingStoreSet{
			ticker:   mockTickerStore,
			contract: mockContractStore,
			price:    mockPriceStore,
			funding:  mockFundingStore,
		},
	}
	err = strategyInst.Start(context.Background(), stores)
	require.NoError(t, err)

	startEvt := reversion.CandidateFoundEvent{
		Flow:       reversion.FlowIDFundingReversion,
		ReqID:      "req_skip_leverage",
		Symbol:     candidate.Symbol,
		Exchange:   candidate.Config.Exchange,
		Timestamp:  time.Now(),
		EventID:    watermill.NewUUID(),
		Seq:        1,
		Topic:      reversion.TopicReversionCandidate,
		ExternalID: orders.ExternalOrderID(candidate.Symbol, now.Add(10*time.Second), candidate.Config.Exchange),
		SettleTime: now.Add(10 * time.Second),
		Candidate:  candidate,
	}

	err = bus.Publish(reversion.TopicReversionCandidate, startEvt)
	require.NoError(t, err)

	select {
	case msg := <-intentSub:
		var intent ordermanager.OrderIntentEvent
		require.NoError(t, json.Unmarshal(msg.Payload, &intent))
		msg.Ack()
		assert.Equal(t, 10, intent.Leverage)
		assert.Equal(t, "req_skip_leverage", intent.ReqID)
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for TopicOrderIntent")
	}
}

type mockClientWithMaxLeverage struct {
	exchange.Client
	maxLeverage    int
	maxLeverageErr error
}

func (m *mockClientWithMaxLeverage) GetMaxLeverage(ctx context.Context, symbol string) (int, error) {
	return m.maxLeverage, m.maxLeverageErr
}

func TestStrategy_Execute_LeverageCapping(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockClient(ctrl)
	mockClient.EXPECT().SupportLeverageOnOrder().Return(false).AnyTimes()
	mockWs := mocks.NewMockSubscriber(ctrl)
	mockTickerStore := mocks.NewMockTickerReader(ctrl)
	mockContractStore := mocks.NewMockContractReader(ctrl)
	mockPriceStore := mocks.NewMockPriceReader(ctrl)
	mockNotifier := mocks.NewMockNotifier(ctrl)
	mockFundingStore := mocks.NewMockFundingReader(ctrl)

	// Set up Clock
	mockClock := mocks.NewMockClock(ctrl)
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	currentNow := now
	mockClock.EXPECT().Now().DoAndReturn(func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return currentNow
	}).AnyTimes()
	mockClock.EXPECT().GetServerTime().DoAndReturn(func() int64 {
		mu.Lock()
		defer mu.Unlock()
		return currentNow.UnixMilli()
	}).AnyTimes()
	mockClock.EXPECT().LatencyMs().Return(int64(20)).AnyTimes()
	mockClock.EXPECT().Offset().Return(int64(0)).AnyTimes()
	mockClock.EXPECT().Until(gomock.Any()).DoAndReturn(func(target time.Time) time.Duration {
		mu.Lock()
		defer mu.Unlock()
		return target.Sub(currentNow)
	}).AnyTimes()
	mockClock.EXPECT().Sleep(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, d time.Duration) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		mu.Lock()
		currentNow = currentNow.Add(d)
		mu.Unlock()
		return nil
	}).AnyTimes()

	bus := eventbus.New(slog.Default())
	defer func() { _ = bus.Close() }()

	wrappedClient := &mockClientWithMaxLeverage{
		Client:      mockClient,
		maxLeverage: 5,
	}

	engine := &app.Engine{
		Bus: bus,
		Providers: map[string]*app.ExchangeProvider{
			"bybit": {
				Name:    "bybit",
				Client:  wrappedClient,
				Adapter: infraws.NewExchangeManagerAdapter(&fakeExchangeAdapter{MockSubscriber: mockWs}),
			},
		},
	}

	cfg := config.SymbolConfig{
		Symbol:   "BTC_USDT",
		Exchange: "bybit",
		Leverage: 10,
		FundingReversion: domain.FundingReversionConfig{
			Enabled:           true,
			PostSettleTimeout: types.Duration(10 * time.Second),
			MaxLatency:        types.Duration(100 * time.Millisecond),
			BufferTime:        0,
		},
	}

	globalCfg := &config.Config{
		System: &config.SystemConfig{},
		Reversion: &config.ReversionConfig{
			Default: config.ExchangeReversionConfig{
				MinVol24USD: 10000,
			},
			Safety: config.SafetyConfig{
				MaxImpactRatio: 1.0,
			},
		},
		Symbols: []config.SymbolConfig{cfg},
	}

	candidate := domain.Candidate{
		Config: domain.TradeConfig{
			Symbol:      "BTC_USDT",
			Exchange:    "bybit",
			MinVol24USD: 10000,
			Leverage:    5,
		},
		Symbol:       "BTC_USDT",
		FundingRate:  0.001,
		Side:         shared.SideOpenLong,
		CloseSide:    shared.SideCloseLong,
		PriceUnit:    0.01,
		VolUnit:      1,
		MinVol:       1,
		PriceScale:   2,
		VolScale:     4,
		ContractSize: 0.001,
		TakerFeeRate: 0.0006,
		MakerFeeRate: 0.0002,
		LastPrice:    60000.0,
		BestBid:      59990.0,
		BestAsk:      60000.0,
		Volume24:     1000,
		Vol24USDT:    60000000,
	}

	mockPriceStore.EXPECT().SubscribePrice(gomock.Any(), "BTC_USDT").Return(nil)
	mockPriceStore.EXPECT().GetPrice(gomock.Any(), "BTC_USDT", gomock.Any()).Return(&store.PriceData{
		BestBid:   59990.0,
		BestAsk:   60000.0,
		LastPrice: 60000.0,
	}, nil).AnyTimes()
	mockContractStore.EXPECT().GetContract(gomock.Any(), "BTC_USDT").Return(&store.ContractData{
		Symbol:       "BTC_USDT",
		ContractSize: 0.001,
	}, nil).AnyTimes()

	mockClient.EXPECT().GetFundingRates(gomock.Any(), []string{"BTC_USDT"}).Return([]exchange.FundingRateResult{
		{Symbol: "BTC_USDT", Rate: 0.001},
	}, nil)

	mockClient.EXPECT().SwitchMarginMode(gomock.Any(), "BTC_USDT", shared.MarginModeIsolated, 5, gomock.Any()).Return(nil)

	changeLeverageCalled := make(chan struct{})
	mockClient.EXPECT().ChangeLeverage(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, req exchange.ChangeLeverageRequest) error {
		assert.Equal(t, 5, req.Leverage)
		close(changeLeverageCalled)
		return nil
	})

	mockNotifier.EXPECT().Send(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	intentSub, err := bus.Subscribe(context.Background(), ordermanager.TopicOrderIntent)
	require.NoError(t, err)

	c := cache.New(5*time.Minute, 10*time.Minute)
	strategyInst := reversion.NewStrategy(engine, globalCfg, mockNotifier, c, slog.Default())
	strategyInst.SetTestFallbacks(mockClock, nil, infraws.NewExchangeManagerAdapter(nil))

	stores := map[string]strategy.FundingStoreSet{
		"bybit": fakeFundingStoreSet{
			ticker:   mockTickerStore,
			contract: mockContractStore,
			price:    mockPriceStore,
			funding:  mockFundingStore,
		},
	}
	err = strategyInst.Start(context.Background(), stores)
	require.NoError(t, err)

	startEvt := reversion.CandidateFoundEvent{
		Flow:       reversion.FlowIDFundingReversion,
		ReqID:      "req_capped_leverage",
		Symbol:     candidate.Symbol,
		Exchange:   candidate.Config.Exchange,
		Timestamp:  time.Now(),
		EventID:    watermill.NewUUID(),
		Seq:        1,
		Topic:      reversion.TopicReversionCandidate,
		ExternalID: orders.ExternalOrderID(candidate.Symbol, now.Add(10*time.Second), candidate.Config.Exchange),
		SettleTime: now.Add(10 * time.Second),
		Candidate:  candidate,
	}

	err = bus.Publish(reversion.TopicReversionCandidate, startEvt)
	require.NoError(t, err)

	select {
	case <-changeLeverageCalled:
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for ChangeLeverage to be called")
	}

	select {
	case msg := <-intentSub:
		var intent ordermanager.OrderIntentEvent
		require.NoError(t, json.Unmarshal(msg.Payload, &intent))
		msg.Ack()
		assert.Equal(t, 5, intent.Leverage)
		assert.Equal(t, "req_capped_leverage", intent.ReqID)
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for TopicOrderIntent")
	}
}

func TestReversion_OrderManager_DispatchesOrderIntent(t *testing.T) {
	t.Parallel()
	testCand := domain.Candidate{
		Config: domain.TradeConfig{
			Symbol:              "BTC_USDT",
			Exchange:            "mexc",
			MaxPriceDiffPercent: 0.01,
			MarginUSDT:          100,
			Leverage:            5,
			FundingReversion: domain.FundingReversionConfig{
				Enabled:           true,
				PostSettleTimeout: types.Duration(10 * time.Second),
				BufferTime:        types.Duration(150 * time.Millisecond),
				MaxLatency:        types.Duration(50 * time.Millisecond),
			},
		},
		Symbol:    "BTC_USDT",
		Side:      shared.SideOpenLong,
		CloseSide: shared.SideCloseLong,
		LastPrice: 60000,
		BestBid:   59990,
		BestAsk:   60000,
		Volume:    1,
	}

	ctrl := gomock.NewController(t)
	bus := eventbus.New(slog.Default())
	t.Cleanup(func() { _ = bus.Close() })

	cand := testCand
	cand.ContractSpec = domain.ContractSpec{
		PriceUnit:    0.01,
		VolUnit:      1,
		MinVol:       1,
		PriceScale:   2,
		VolScale:     4,
		ContractSize: 0.001,
	}

	client := mocks.NewMockClient(ctrl)
	client.EXPECT().SwitchMarginMode(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	client.EXPECT().SupportLeverageOnOrder().Return(true).AnyTimes()

	engine := &app.Engine{
		Bus: bus,
		Providers: map[string]*app.ExchangeProvider{
			"mexc": {
				Name:    "mexc",
				Client:  client,
				Adapter: infraws.NewExchangeManagerAdapter(nil),
			},
		},
	}
	cfg := &config.Config{
		Reversion: &config.ReversionConfig{
			Safety: config.SafetyConfig{
				MaxImpactRatio: 1.0,
			},
		},
	}
	strategyInst := reversion.NewStrategy(engine, cfg, nil, nil, slog.Default())

	intentSub, err := bus.Subscribe(context.Background(), ordermanager.TopicOrderIntent)
	require.NoError(t, err)

	now := time.Now()
	mockClock := mocks.NewMockClock(ctrl)
	mockClock.EXPECT().Now().Return(now).AnyTimes()
	mockClock.EXPECT().LatencyMs().Return(int64(20)).AnyTimes()
	mockClock.EXPECT().Until(gomock.Any()).Return(50 * time.Millisecond).AnyTimes()
	mockClock.EXPECT().Sleep(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	strategyInst.SetTestFallbacks(mockClock, nil, nil)

	confirmedEvt := reversion.ConfirmedEvent{
		ReqID:      "req-om-true",
		Symbol:     "BTC_USDT",
		Exchange:   "mexc",
		SettleTime: now.Add(1 * time.Second),
		Candidate:  cand,
	}

	mockPriceStore := mocks.NewMockPriceReader(ctrl)
	mockPriceStore.EXPECT().GetPrice(gomock.Any(), gomock.Any(), gomock.Any()).Return(&store.PriceData{BestBid: 59990, BestAsk: 60000, LastPrice: 60000}, nil).AnyTimes()

	stores := map[string]strategy.FundingStoreSet{
		"mexc": fakeFundingStoreSet{
			price: mockPriceStore,
		},
	}
	require.NoError(t, strategyInst.Start(context.Background(), stores))

	require.NoError(t, bus.Publish(reversion.TopicReversionConfirmed, confirmedEvt))

	select {
	case msg := <-intentSub:
		var receivedIntent ordermanager.OrderIntentEvent
		require.NoError(t, json.Unmarshal(msg.Payload, &receivedIntent))
		assert.Equal(t, "req-om-true", receivedIntent.ReqID)
		assert.Equal(t, "BTC_USDT", receivedIntent.Symbol)
		assert.Equal(t, ordermanager.StrategyFundingReversion, receivedIntent.StrategyType)
		assert.Greater(t, receivedIntent.Price, 0.0)
	case <-time.After(3 * time.Second):
		t.Fatal("Timeout waiting for OrderIntentEvent on ordermanager.TopicOrderIntent")
	}
}
