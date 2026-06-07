package reversion_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
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
	"crypto-bot/pkg/eventbus"
	"crypto-bot/pkg/types"
	pkgws "crypto-bot/pkg/ws"

	"github.com/ThreeDotsLabs/watermill"
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

func executeReversionHelper(t *testing.T, bus *eventbus.Bus, reqID string, candidate domain.Candidate, settleTime time.Time) error {
	candidate.ExternalID = orders.ExternalOrderID("ioc", candidate.Symbol)

	startEvt := reversion.CandidateFoundEvent{
		BaseReversionEvent: reversion.BaseReversionEvent{
			Flow:       reversion.FlowReversion,
			ReqID:      reqID,
			Symbol:     candidate.Symbol,
			Exchange:   candidate.Config.Exchange,
			SendNotify: false,
			Timestamp:  time.Now(),
			EventID:    watermill.NewUUID(),
			Seq:        1,
			Topic:      reversion.TopicReversionCandidate,
			ExternalID: candidate.ExternalID,
			SettleTime: settleTime,
		},
		Candidate: candidate,
	}

	return bus.Publish(reversion.TopicReversionCandidate, startEvt)
}

func TestStrategy_Execute_Success(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockClient(ctrl)
	mockWs := mocks.NewMockSubscriber(ctrl)
	mockOrderNotifier := mocks.NewMockOrderNotifier(ctrl)
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
				Adapter: &fakeExchangeAdapter{MockSubscriber: mockWs},
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
		System: &config.SystemConfig{
			Safety: config.SafetyConfig{
				MaxImpactRatio: 1.0,
				MinVol24USD:    10000,
			},
		},
		Symbols: []config.SymbolConfig{cfg},
	}

	candidate := domain.Candidate{
		Config: domain.TradeConfig{
			Symbol:   "BTC_USDT",
			Exchange: "mexc",
		},
		TradeIntent: domain.TradeIntent{
			Symbol:      "BTC_USDT",
			FundingRate: 0.001, // positive FR means open long
			Side:        shared.SideOpenLong,
			CloseSide:   shared.SideCloseLong,
		},
		ContractSpec: domain.ContractSpec{
			PriceUnit:    0.01,
			VolUnit:      1,
			MinVol:       1,
			PriceScale:   2,
			VolScale:     4,
			ContractSize: 0.001,
			TakerFeeRate: 0.0006,
			MakerFeeRate: 0.0002,
		},
		MarketData: domain.MarketData{
			LastPrice: 60000.0,
			BestBid:   59990.0,
			BestAsk:   60000.0,
			Volume24:  1000,
			Amount24:  60000000,
		},
	}

	// 1. Arm expectations
	mockWs.EXPECT().SubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil)
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
	mockFundingStore.EXPECT().GetFunding(gomock.Any(), "BTC_USDT").Return(&store.FundingData{
		Symbol:      "BTC_USDT",
		FundingRate: 0.001,
	}, nil)

	// 3. FireIOC expectations
	mockClient.EXPECT().SwitchMarginMode(gomock.Any(), "BTC_USDT", "ISOLATED", 0, gomock.Any()).Return(nil)
	createOrderCalled := make(chan struct{})
	mockClient.EXPECT().CreateOrder(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
			close(createOrderCalled)
			return exchange.CreateOrderResult{OrderID: "ord_123", TPSLSubmitted: false}, nil
		},
	)
	mockClient.EXPECT().GetOrder(gomock.Any(), gomock.Any(), "ord_123").Return(&exchange.OrderInfo{
		OrderID:      "ord_123",
		Symbol:       "BTC_USDT",
		State:        exchange.OrderStateFilled,
		DealVol:      1,
		DealAvgPrice: 60005.0,
	}, nil).AnyTimes()
	mockClient.EXPECT().GetOpenPositions(gomock.Any(), "BTC_USDT").Return([]exchange.Position{
		{Symbol: "BTC_USDT", HoldVol: 1},
	}, nil).AnyTimes()
	mockClient.EXPECT().CloseAllPositions(gomock.Any(), "BTC_USDT").Return(nil).AnyTimes()

	// 4. Watcher/notifier expectations
	mockOrderNotifier.EXPECT().OnPositionUpdate(gomock.Any(), "BTC_USDT", gomock.Any(), gomock.Any()).Do(
		func(ctx context.Context, symbol string, timeout time.Duration, cb func(exchange.PersonalPositionUpdate)) {
			// Trigger a fill update asynchronously
			go func() {
				time.Sleep(10 * time.Millisecond)
				cb(exchange.PersonalPositionUpdate{
					Symbol:       "BTC_USDT",
					HoldVol:      1.5,
					OpenAvgPrice: 60005.0,
				})
				time.Sleep(10 * time.Millisecond)
				cb(exchange.PersonalPositionUpdate{
					Symbol:       "BTC_USDT",
					HoldVol:      0.0,
					OpenAvgPrice: 60100.0,
				})
			}()
		},
	)

	// Unsubscribe ws expectation on cleanup (which might be called once or twice on error recovery)
	mockWs.EXPECT().UnsubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil).AnyTimes()

	// Notifier expectations for events with SendNotify = true
	mockNotifier.EXPECT().Send(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	// Subscribe to TopicReversionCompleted to wait for the asynchronous flow to finish in tests
	subCtx := t.Context()
	ch, err := bus.Subscribe(subCtx, reversion.TopicReversionCompleted)
	require.NoError(t, err)

	strategyInst := reversion.NewStrategy(engine, globalCfg, mockNotifier, slog.Default())
	strategyInst.SetTestFallbacks(mockClock, mockOrderNotifier, mockWs)

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

	// Wait for the completion event for "BTC_USDT" to ensure all mocks are met
	for {
		select {
		case msg, ok := <-ch:
			require.True(t, ok)
			var compEvt reversion.ReversionCompletedEvent
			err := json.Unmarshal(msg.Payload, &compEvt)
			if err == nil && compEvt.Symbol == "BTC_USDT" {
				msg.Ack()
				// Wait for CreateOrder to actually be called to avoid asynchronous race conditions in parallel tests
				select {
				case <-createOrderCalled:
				case <-time.After(5 * time.Second):
					t.Fatal("Timeout waiting for CreateOrder to be called")
				}
				return
			}
			msg.Ack()
		case <-time.After(15 * time.Second):
			t.Fatal("Timeout waiting for TopicReversionCompleted")
		}
	}
}

func TestStrategy_Execute_ExternalID_Propagation(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockClient(ctrl)
	mockWs := mocks.NewMockSubscriber(ctrl)
	mockOrderNotifier := mocks.NewMockOrderNotifier(ctrl)
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
				Adapter: &fakeExchangeAdapter{MockSubscriber: mockWs},
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
		System: &config.SystemConfig{
			Safety: config.SafetyConfig{
				MaxImpactRatio: 1.0,
				MinVol24USD:    10000,
			},
		},
		Symbols: []config.SymbolConfig{cfg},
	}

	candidate := domain.Candidate{
		Config: domain.TradeConfig{
			Symbol:   "BTC_USDT",
			Exchange: "mexc",
		},
		TradeIntent: domain.TradeIntent{
			Symbol:      "BTC_USDT",
			FundingRate: 0.001,
			Side:        shared.SideOpenLong,
			CloseSide:   shared.SideCloseLong,
		},
		ContractSpec: domain.ContractSpec{
			PriceUnit:    0.01,
			VolUnit:      1,
			MinVol:       1,
			PriceScale:   2,
			VolScale:     4,
			ContractSize: 0.001,
			TakerFeeRate: 0.0006,
			MakerFeeRate: 0.0002,
		},
		MarketData: domain.MarketData{
			LastPrice: 60000.0,
			BestBid:   59990.0,
			BestAsk:   60000.0,
			Volume24:  1000,
			Amount24:  60000000,
		},
	}

	mockWs.EXPECT().SubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil)
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

	mockFundingStore.EXPECT().GetFunding(gomock.Any(), "BTC_USDT").Return(&store.FundingData{
		Symbol:      "BTC_USDT",
		FundingRate: 0.001,
	}, nil)

	mockClient.EXPECT().SwitchMarginMode(gomock.Any(), "BTC_USDT", "ISOLATED", 0, gomock.Any()).Return(nil)
	createOrderCalled := make(chan struct{})
	// Capture the ExternalOID that is passed to CreateOrder
	var capturedExtOID string
	mockClient.EXPECT().CreateOrder(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
			capturedExtOID = req.ExternalOID
			close(createOrderCalled)
			return exchange.CreateOrderResult{OrderID: "ord_123", TPSLSubmitted: false}, nil
		},
	)

	mockClient.EXPECT().GetOrder(gomock.Any(), gomock.Any(), "ord_123").Return(&exchange.OrderInfo{
		OrderID:      "ord_123",
		Symbol:       "BTC_USDT",
		State:        exchange.OrderStateFilled,
		DealVol:      1,
		DealAvgPrice: 60005.0,
	}, nil).AnyTimes()
	mockClient.EXPECT().GetOpenPositions(gomock.Any(), "BTC_USDT").Return([]exchange.Position{
		{Symbol: "BTC_USDT", HoldVol: 1},
	}, nil).AnyTimes()
	mockClient.EXPECT().CloseAllPositions(gomock.Any(), "BTC_USDT").Return(nil).AnyTimes()

	mockOrderNotifier.EXPECT().OnPositionUpdate(gomock.Any(), "BTC_USDT", gomock.Any(), gomock.Any()).Do(
		func(ctx context.Context, symbol string, timeout time.Duration, cb func(exchange.PersonalPositionUpdate)) {
			go func() {
				cb(exchange.PersonalPositionUpdate{
					Symbol:       "BTC_USDT",
					HoldVol:      1.5,
					OpenAvgPrice: 60005.0,
				})
				cb(exchange.PersonalPositionUpdate{
					Symbol:       "BTC_USDT",
					HoldVol:      0.0,
					OpenAvgPrice: 60100.0,
				})
			}()
		},
	)

	mockWs.EXPECT().UnsubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil).AnyTimes()
	mockNotifier.EXPECT().Send(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	// Capture and verify ExternalID on event bus for the first event (CandidateFoundEvent)
	candidateChan, err := bus.Subscribe(context.Background(), reversion.TopicReversionCandidate)
	require.NoError(t, err)

	var candidateEvt reversion.CandidateFoundEvent
	var wg sync.WaitGroup
	wg.Go(func() {
		msg := <-candidateChan
		_ = json.Unmarshal(msg.Payload, &candidateEvt)
		msg.Ack()
	})

	compChan, err := bus.Subscribe(context.Background(), reversion.TopicReversionCompleted)
	require.NoError(t, err)

	strategyInst := reversion.NewStrategy(engine, globalCfg, mockNotifier, slog.Default())
	strategyInst.SetTestFallbacks(mockClock, mockOrderNotifier, mockWs)

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

	// Wait for CandidateFoundEvent to be captured
	wg.Wait()

	// Wait for completion event
	for {
		select {
		case msg, ok := <-compChan:
			require.True(t, ok)
			var compEvt reversion.ReversionCompletedEvent
			err := json.Unmarshal(msg.Payload, &compEvt)
			if err == nil && compEvt.Symbol == "BTC_USDT" {
				msg.Ack()
				goto Verified
			}
			msg.Ack()
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for TopicReversionCompleted")
		}
	}

Verified:
	// Wait for CreateOrder to actually be called to avoid asynchronous race conditions in parallel tests
	select {
	case <-createOrderCalled:
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for CreateOrder to be called")
	}

	// Verify that the generated ExternalID is <= 30 chars, not empty, starts with "ioc_"
	assert.NotEmpty(t, candidateEvt.ExternalID)
	assert.True(t, strings.HasPrefix(candidateEvt.ExternalID, "ioc_"))
	assert.LessOrEqual(t, len(candidateEvt.ExternalID), 30)

	// CRITICAL ASSERTION: The external ID sent to exchange exactly matches the one generated at the start of the flow!
	assert.Equal(t, candidateEvt.ExternalID, capturedExtOID)
}

func TestStrategy_Execute_SkipLeverageChange(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockClient(ctrl)
	mockWs := mocks.NewMockSubscriber(ctrl)
	mockOrderNotifier := mocks.NewMockOrderNotifier(ctrl)
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
				Adapter: &fakeExchangeAdapter{MockSubscriber: mockWs},
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
		System: &config.SystemConfig{
			Safety: config.SafetyConfig{
				MaxImpactRatio: 1.0,
				MinVol24USD:    10000,
			},
		},
		Symbols: []config.SymbolConfig{cfg},
	}

	candidate := domain.Candidate{
		Config: domain.TradeConfig{
			Symbol:   "BTC_USDT",
			Exchange: "bybit",
			Leverage: 10,
		},
		TradeIntent: domain.TradeIntent{
			Symbol:      "BTC_USDT",
			FundingRate: 0.001,
			Side:        shared.SideOpenLong,
			CloseSide:   shared.SideCloseLong,
		},
		ContractSpec: domain.ContractSpec{
			PriceUnit:    0.01,
			VolUnit:      1,
			MinVol:       1,
			PriceScale:   2,
			VolScale:     4,
			ContractSize: 0.001,
			TakerFeeRate: 0.0006,
			MakerFeeRate: 0.0002,
		},
		MarketData: domain.MarketData{
			LastPrice: 60000.0,
			BestBid:   59990.0,
			BestAsk:   60000.0,
			Volume24:  1000,
			Amount24:  60000000,
		},
	}

	// 1. Arm expectations
	mockWs.EXPECT().SubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil)
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
	mockFundingStore.EXPECT().GetFunding(gomock.Any(), "BTC_USDT").Return(&store.FundingData{
		Symbol:      "BTC_USDT",
		FundingRate: 0.001,
	}, nil)

	// 3. FireIOC expectations (we assert CreateOrder receives Leverage = 10, and ChangeLeverage is NOT called)
	mockClient.EXPECT().SwitchMarginMode(gomock.Any(), "BTC_USDT", "ISOLATED", 10, gomock.Any()).Return(nil)
	createOrderCalled := make(chan struct{})
	mockClient.EXPECT().CreateOrder(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
		assert.Equal(t, 10, req.Leverage)
		close(createOrderCalled)
		return exchange.CreateOrderResult{OrderID: "ord_123", TPSLSubmitted: false}, nil
	})
	mockClient.EXPECT().GetOrder(gomock.Any(), gomock.Any(), "ord_123").Return(&exchange.OrderInfo{
		OrderID:      "ord_123",
		Symbol:       "BTC_USDT",
		State:        exchange.OrderStateFilled,
		DealVol:      1,
		DealAvgPrice: 60005.0,
	}, nil).AnyTimes()
	mockClient.EXPECT().GetOpenPositions(gomock.Any(), "BTC_USDT").Return([]exchange.Position{
		{Symbol: "BTC_USDT", HoldVol: 1},
	}, nil).AnyTimes()

	// 4. Watcher/notifier expectations
	mockOrderNotifier.EXPECT().OnPositionUpdate(gomock.Any(), "BTC_USDT", gomock.Any(), gomock.Any()).Do(
		func(ctx context.Context, symbol string, timeout time.Duration, cb func(exchange.PersonalPositionUpdate)) {
			go func() {
				cb(exchange.PersonalPositionUpdate{
					Symbol:       "BTC_USDT",
					HoldVol:      1.5,
					OpenAvgPrice: 60005.0,
				})
				cb(exchange.PersonalPositionUpdate{
					Symbol:       "BTC_USDT",
					HoldVol:      0.0,
					OpenAvgPrice: 60100.0,
				})
			}()
		},
	)

	mockWs.EXPECT().UnsubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil).AnyTimes()
	mockNotifier.EXPECT().Send(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	compChan, err := bus.Subscribe(context.Background(), reversion.TopicReversionCompleted)
	require.NoError(t, err)

	strategyInst := reversion.NewStrategy(engine, globalCfg, mockNotifier, slog.Default())
	strategyInst.SetTestFallbacks(mockClock, mockOrderNotifier, mockWs)

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

	// Build candidate found event with SupportLeverageOnOrder = true
	startEvt := reversion.CandidateFoundEvent{
		BaseReversionEvent: reversion.BaseReversionEvent{
			Flow:                   reversion.FlowReversion,
			ReqID:                  "req_skip_leverage",
			Symbol:                 candidate.Symbol,
			Exchange:               candidate.Config.Exchange,
			SendNotify:             false,
			Timestamp:              time.Now(),
			EventID:                watermill.NewUUID(),
			Seq:                    1,
			Topic:                  reversion.TopicReversionCandidate,
			ExternalID:             orders.ExternalOrderID("ioc", candidate.Symbol),
			SettleTime:             now.Add(10 * time.Second),
			SupportLeverageOnOrder: true, // We explicitly set this to true to verify skipping ChangeLeverage!
		},
		Candidate: candidate,
	}

	err = bus.Publish(reversion.TopicReversionCandidate, startEvt)
	require.NoError(t, err)

	// Wait for completion event
	for {
		select {
		case msg, ok := <-compChan:
			require.True(t, ok)
			var compEvt reversion.ReversionCompletedEvent
			err := json.Unmarshal(msg.Payload, &compEvt)
			if err == nil && compEvt.Symbol == "BTC_USDT" {
				msg.Ack()
				// Wait for CreateOrder to actually be called to avoid asynchronous race conditions in parallel tests
				select {
				case <-createOrderCalled:
				case <-time.After(5 * time.Second):
					t.Fatal("Timeout waiting for CreateOrder to be called")
				}
				return
			}
			msg.Ack()
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for TopicReversionCompleted")
		}
	}
}
