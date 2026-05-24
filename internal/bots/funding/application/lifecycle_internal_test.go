package application

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"
	"unsafe"

	"crypto-bot/internal/bots/funding/application/cycle"
	"crypto-bot/internal/bots/funding/application/events"
	"crypto-bot/internal/bots/funding/config"
	fundingdomain "crypto-bot/internal/bots/funding/domain"
	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/internal/infrastructure/watcher"
	"crypto-bot/internal/testutil/mocks"
	"crypto-bot/pkg/eventbus"
	"crypto-bot/pkg/types"
	pkgws "crypto-bot/pkg/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestHandleScanPublishesCandidates(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	o := newApplicationOrchestrator(t, cycle.Deps{
		TickerStore: &appTickerReader{ticker: &store.TickerData{
			Symbol:      "BTC_USDT",
			FundingRate: 0.02,
			LastPrice:   100,
			BestBid:     99,
			BestAsk:     101,
			Volume24:    1000,
		}},
		ContractStore: &appContractReader{contract: &store.ContractData{
			Symbol:       "BTC_USDT",
			PriceUnit:    0.1,
			MinVol:       1,
			PriceScale:   1,
			VolScale:     0,
			ContractSize: 1,
		}},
		WsSub: mocks.NewMockSubscriber(ctrl),
	})

	o.handleScan(context.Background())

	requireTopic(t, o.rt, events.TopicScanCandidateFound)
	requireTopic(t, o.rt, events.TopicReversionCandidate)
	trap := requireCandidateFound(t, o.rt, events.TopicTrapCandidate)
	assert.Equal(t, events.FlowTrap, trap.Flow)
	assert.Equal(t, shared.SideOpenShort, trap.Side)
}

func TestHandleScanAbortBranches(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	ws := mocks.NewMockSubscriber(ctrl)

	o := newApplicationOrchestrator(t, cycle.Deps{
		TickerStore: &appTickerReader{err: errors.New("missing")},
		WsSub:       ws,
	})
	o.handleScan(context.Background())
	assert.Equal(t, "no ticker data", requireAbort(t, o.rt).Reason)

	o = newApplicationOrchestrator(t, cycle.Deps{
		TickerStore: &appTickerReader{ticker: &store.TickerData{Symbol: "BTC_USDT", FundingRate: 0.001}},
		WsSub:       ws,
	})
	o.handleScan(context.Background())
	assert.Equal(t, "FR below threshold", requireAbort(t, o.rt).Reason)

	o = newApplicationOrchestrator(t, cycle.Deps{
		TickerStore: &appTickerReader{ticker: &store.TickerData{
			Symbol:      "BTC_USDT",
			FundingRate: 0.02,
			Amount24:    999,
		}},
		WsSub: ws,
	})
	o.rt.Global().System.Safety.MinVol24USD = 1000
	o.handleScan(context.Background())
	assert.Equal(t, "24h volume below threshold", requireAbort(t, o.rt).Reason)

	o = newApplicationOrchestrator(t, cycle.Deps{
		TickerStore: &appTickerReader{ticker: &store.TickerData{
			Symbol:      "BTC_USDT",
			FundingRate: 0.02,
		}},
		ContractStore: &appContractReader{err: errors.New("no contract")},
		WsSub:         ws,
	})
	o.handleScan(context.Background())
	assert.Equal(t, "enrichment failed", requireAbort(t, o.rt).Reason)
}

func TestTrapCleanupCancelAndCloseBranches(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	o := newApplicationOrchestrator(t, cycle.Deps{Client: client})

	order := events.TrapFiredEvent{Symbol: "BTC_USDT", OrderID: "trap-1"}
	client.EXPECT().CancelOrder(gomock.Any(), "BTC_USDT", "trap-1").Return(nil)
	o.cancelOpenTrapOrder(context.Background(), order)
	requireTopic(t, o.rt, events.TopicTrapTimeout)

	client = mocks.NewMockClient(ctrl)
	o = newApplicationOrchestrator(t, cycle.Deps{Client: client})
	client.EXPECT().CancelOrder(gomock.Any(), "BTC_USDT", "trap-1").Return(errors.New("cancel failed")).Times(3)
	client.EXPECT().CancelAllOpenOrders(gomock.Any(), "BTC_USDT").Return(nil)
	o.cancelOpenTrapOrder(context.Background(), order)
	requireTopic(t, o.rt, events.TopicTrapTimeout)

	client = mocks.NewMockClient(ctrl)
	o = newApplicationOrchestrator(t, cycle.Deps{Client: client})
	client.EXPECT().CancelOrder(gomock.Any(), "BTC_USDT", "trap-1").Return(errors.New("cancel failed")).Times(3)
	client.EXPECT().CancelAllOpenOrders(gomock.Any(), "BTC_USDT").Return(errors.New("all failed")).Times(3)
	o.cancelOpenTrapOrder(context.Background(), order)
	cancelErr := requireError(t, o.rt)
	assert.Contains(t, cancelErr.Error, "critical_trap_cancel_failed")

	client = mocks.NewMockClient(ctrl)
	o = newApplicationOrchestrator(t, cycle.Deps{Client: client})
	fill := events.OrderFilledEvent{Symbol: "BTC_USDT", CloseSide: shared.SideCloseLong, FillVol: 2}
	client.EXPECT().ClosePosition(gomock.Any(), "BTC_USDT", shared.SideCloseLong, 2.0, 1).Return(nil)
	o.closeFilledTrapPosition(context.Background(), fill)
	closed := requirePositionClosed(t, o.rt)
	assert.Equal(t, "reversion_cleanup_close", closed.Reason)

	client = mocks.NewMockClient(ctrl)
	o = newApplicationOrchestrator(t, cycle.Deps{Client: client})
	client.EXPECT().ClosePosition(gomock.Any(), "BTC_USDT", shared.SideCloseLong, 2.0, 1).
		Return(errors.New("exact failed")).Times(3)
	client.EXPECT().CloseAllPositions(gomock.Any(), "BTC_USDT").Return(errors.New("all failed")).Times(3)
	o.closeFilledTrapPosition(context.Background(), fill)
	errEvt := requireError(t, o.rt)
	assert.Contains(t, errEvt.Error, "critical_trap_close_failed")

	client = mocks.NewMockClient(ctrl)
	o = newApplicationOrchestrator(t, cycle.Deps{Client: client})
	client.EXPECT().ClosePosition(gomock.Any(), "BTC_USDT", shared.SideCloseLong, 2.0, 1).
		Return(errors.New("exact failed")).Times(3)
	client.EXPECT().CloseAllPositions(gomock.Any(), "BTC_USDT").Return(nil)
	o.closeFilledTrapPosition(context.Background(), fill)
	requireTopic(t, o.rt, events.TopicTrapPositionClosed)
}

func TestCleanupFunctionFinalizesCycleOnce(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	ws := mocks.NewMockSubscriber(ctrl)
	priceStore := store.NewPriceStore()
	o := newApplicationOrchestrator(t, cycle.Deps{
		Client:     client,
		WsSub:      ws,
		PriceStore: priceStore,
	})
	o.rt.MarkTrapOrder(events.TrapFiredEvent{Symbol: "BTC_USDT", OrderID: "trap-1"})

	client.EXPECT().CancelOrder(gomock.Any(), "BTC_USDT", "trap-1").Return(nil)
	ws.EXPECT().UnsubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil)

	done := make(chan struct{}, 1)
	cleanup := o.makeCleanupFn(context.Background(), done)
	cleanup(events.TopicReversionTimeout, events.FlowReversion, "force_close")
	cleanup(events.TopicReversionTimeout, events.FlowReversion, "ignored")

	requireTopic(t, o.rt, events.TopicCleanupStarted)
	requireTopic(t, o.rt, events.TopicCleanupCompleted)
	requireTopic(t, o.rt, events.TopicCycleCompleted)
	requireTopic(t, o.rt, events.TopicCycleFinalPnL)
	require.Len(t, done, 1)
}

func TestSubscribeCleanupHandlesTerminalPublication(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	ws := mocks.NewMockSubscriber(ctrl)
	o := newApplicationOrchestrator(t, cycle.Deps{WsSub: ws})

	ws.EXPECT().UnsubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil)

	done := make(chan struct{}, 1)
	o.subscribeCleanup(context.Background(), done)
	o.rt.RecordAndPublish(context.Background(), "req-1", events.TopicTrapAbort, events.CycleAbortEvent{
		Flow:   events.FlowTrap,
		Symbol: "BTC_USDT",
		Reason: "aborted",
	})

	require.Eventually(t, func() bool {
		return len(done) == 1
	}, time.Second, 10*time.Millisecond)
	requireTopic(t, o.rt, events.TopicCleanupStarted)
	requireTopic(t, o.rt, events.TopicCleanupCompleted)
}

func TestSubscribeCleanupHandlesFallbackReasons(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		topic   string
		payload any
	}{
		{name: "invalid position closed", topic: events.TopicReversionPositionClosed, payload: "bad-payload"},
		{name: "timeout default reason", topic: events.TopicReversionTimeout, payload: events.CycleTimeoutEvent{Flow: events.FlowTrap}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			ws := mocks.NewMockSubscriber(ctrl)
			o := newApplicationOrchestrator(t, cycle.Deps{WsSub: ws})
			ws.EXPECT().UnsubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil)

			done := make(chan struct{}, 1)
			o.subscribeCleanup(context.Background(), done)
			o.rt.Publish(context.Background(), tt.topic, tt.payload)

			require.Eventually(t, func() bool {
				return len(done) == 1
			}, time.Second, 10*time.Millisecond)
			requireTopic(t, o.rt, events.TopicCleanupStarted)
			requireTopic(t, o.rt, events.TopicCleanupCompleted)
		})
	}
}

func TestSetupEventChainWiresSubscribers(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	notifier := mocks.NewMockOrderNotifier(ctrl)
	o := newApplicationOrchestrator(t, cycle.Deps{
		OrderNotifier: notifier,
		TickerStore:   &appTickerReader{err: errors.New("cancelled")},
	})

	notifier.EXPECT().
		OnPositionUpdate(gomock.Any(), "BTC_USDT", 30*time.Second, gomock.Any())

	done := make(chan struct{}, 1)
	o.setupEventChain(context.Background(), done)

	assert.Empty(t, done)
}

func TestCycleOrchestratorRunCancelledContext(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	notifier := mocks.NewMockOrderNotifier(ctrl)
	o := newApplicationOrchestrator(t, cycle.Deps{OrderNotifier: notifier})

	notifier.EXPECT().
		OnPositionUpdate(gomock.Any(), "BTC_USDT", 30*time.Second, gomock.Any())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	o.Run(ctx, time.Now().Add(time.Hour))

	requireTopic(t, o.rt, events.TopicCycleStarted)
}

func TestTerminalFlowAndUnmarshalHelper(t *testing.T) {
	t.Parallel()

	assert.Equal(t, events.FlowTrap, terminalFlow(events.TopicTrapTimeout))
	assert.Equal(t, events.FlowTrap, terminalFlow(events.TopicTrapPositionClosed))
	assert.Equal(t, events.FlowTrap, terminalFlow(events.TopicTrapAbort))
	assert.Equal(t, events.FlowReversion, terminalFlow(events.TopicReversionTimeout))
	assert.Equal(t, events.FlowReversion, terminalFlow(events.TopicReversionPositionClosed))
	assert.Equal(t, events.FlowReversion, terminalFlow(events.TopicReversionAbort))
	assert.Empty(t, terminalFlow(events.TopicScanStart))

	evt, err := unmarshal[events.CycleTimeoutEvent]([]byte(`{"symbol":"BTC_USDT","reason":"timeout"}`))
	require.NoError(t, err)
	assert.Equal(t, "timeout", evt.Reason)

	_, err = unmarshal[events.CycleTimeoutEvent]([]byte(`{`))
	require.Error(t, err)
}

func TestSpawnWorkerSkipsDisabledSymbol(t *testing.T) {
	t.Parallel()

	s := &Sniper{
		disabled: map[string]string{"BTC_USDT": "manual"},
		log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	err := s.spawnWorker(context.Background(), config.SymbolConfig{Symbol: "BTC_USDT"})()

	require.NoError(t, err)
	reason, disabled := s.disabledReason("BTC_USDT")
	assert.True(t, disabled)
	assert.Equal(t, "manual", reason)
}

func TestWirePersonalWSRegistersAndDispatchesHandlers(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pool := pkgws.NewPool("wss://example.test", 10, logger)
	adapter := &personalWSAdapter{}
	s := &Sniper{
		engine: &app.Engine{
			WS:      pool,
			Adapter: adapter,
			Bus:     eventbus.New(logger),
		},
		orderNotifier: watcher.NewOrderWatcher(eventbus.New(logger), logger),
		log:           logger,
	}

	s.wirePersonalWS(context.Background())

	callPoolHandler(t, pool, "personal.position", []byte("ok"))

	adapter.err = errors.New("parse failed")
	callPoolHandler(t, pool, "personal.position", []byte("bad"))
}

func TestWirePersonalWSSkipsMissingDependencies(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, sniper := range []*Sniper{
		{},
		{engine: &app.Engine{}},
		{engine: &app.Engine{WS: pkgws.NewPool("wss://example.test", 10, logger)}},
		{
			engine: &app.Engine{
				WS:      pkgws.NewPool("wss://example.test", 10, logger),
				Adapter: &personalWSAdapter{},
			},
		},
	} {
		assert.NotPanics(t, func() { sniper.wirePersonalWS(context.Background()) })
	}
}

func TestSpawnWorkerSkipsWhenCycleWindowSleepIsCancelled(t *testing.T) {
	t.Parallel()

	s := &Sniper{
		cfg:      &config.Config{},
		stores:   storelessCentralStore(),
		timeSync: &appClock{until: time.Minute, sleepErr: context.Canceled},
		disabled: map[string]string{},
		log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	future := time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339)

	err := s.spawnWorker(context.Background(), config.SymbolConfig{
		Symbol:         "BTC_USDT",
		SimulateSettle: future,
	})()

	require.NoError(t, err)
}

func TestSpawnWorkerSkipsMissedDeadline(t *testing.T) {
	t.Parallel()

	s := &Sniper{
		cfg:      &config.Config{},
		stores:   storelessCentralStore(),
		timeSync: &appClock{},
		disabled: map[string]string{},
		log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	future := time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339)

	err := s.spawnWorker(context.Background(), config.SymbolConfig{
		Symbol:         "BTC_USDT",
		SimulateSettle: future,
	})()

	require.NoError(t, err)
}

func TestSpawnWorkerRunsCycleAndAbortsCleanlyWithoutTickerStore(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	bus := eventbus.New(logger)
	t.Cleanup(func() { require.NoError(t, bus.Close()) })
	ws := mocks.NewMockSubscriber(ctrl)
	ws.EXPECT().UnsubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil)

	s := &Sniper{
		cfg:           &config.Config{System: &config.SystemConfig{}},
		client:        mocks.NewMockClient(ctrl),
		ws:            ws,
		orderNotifier: watcher.NewOrderWatcher(bus, logger),
		stores:        storelessCentralStore(),
		timeSync:      &appClock{until: time.Minute},
		disabled:      map[string]string{},
		log:           logger,
	}
	future := time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339)

	err := s.spawnWorker(context.Background(), config.SymbolConfig{
		Symbol:         "BTC_USDT",
		SimulateSettle: future,
		FundingReversion: fundingdomain.FundingReversionConfig{
			Enabled: true,
		},
	})()

	require.NoError(t, err)
}

func TestGetNextSettleTimeSimulateAndFallback(t *testing.T) {
	t.Parallel()

	future := time.Now().Add(2 * time.Minute).UTC().Format(time.RFC3339)
	got, err := GetNextSettleTime(context.Background(), future, "BTC_USDT", nil)
	require.NoError(t, err)
	assert.True(t, got.After(time.Now()))

	_, err = GetNextSettleTime(context.Background(), "bad", "BTC_USDT", nil)
	require.Error(t, err)

	expected := time.Now().Add(time.Hour)
	got, err = GetNextSettleTime(context.Background(), "", "BTC_USDT", appFundingReader{settle: expected})
	require.NoError(t, err)
	assert.Equal(t, expected, got)
}

func newApplicationOrchestrator(t *testing.T, deps cycle.Deps) *CycleOrchestrator {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if deps.Log == nil {
		deps.Log = logger
	}
	if deps.Clock == nil {
		deps.Clock = &appClock{}
	}
	cfg := config.SymbolConfig{
		Symbol:              "BTC_USDT",
		MinFundingRate:      0.01,
		MaxPriceDiffPercent: 0.2,
		MarginUSDT:          100,
		Leverage:            5,
		ParsedOpenType:      exchange.OpenTypeIsolated,
		ParsedPositionMode:  1,
		FundingReversion: fundingdomain.FundingReversionConfig{
			StopLossPct:       0.01,
			PostSettleTimeout: types.Duration(time.Second),
		},
		FundingTrap: fundingdomain.FundingTrapConfig{
			Enabled:     true,
			StopLossPct: 0.01,
		},
	}
	o := NewCycleOrchestrator(cfg, &config.Config{System: &config.SystemConfig{}}, deps)
	o.rt.Begin(context.Background(), "req-1", time.Now(), logger)
	o.rt.SetCandidate(fundingdomain.Candidate{
		Config: cycle.ToTradeConfig(cfg),
		TradeIntent: fundingdomain.TradeIntent{
			Symbol:    "BTC_USDT",
			Side:      shared.SideOpenLong,
			CloseSide: shared.SideCloseLong,
		},
	})
	t.Cleanup(func() {
		require.NoError(t, o.rt.CloseBus())
	})
	return o
}

func storelessCentralStore() *app.CentralStore {
	return app.NewCentralStore()
}

func callPoolHandler(t *testing.T, pool *pkgws.Pool, channel string, data []byte) {
	t.Helper()

	handlers := reflect.ValueOf(pool).Elem().FieldByName("handlers")
	//nolint:gosec // Test-only access to private handler registry so registered callbacks can be exercised.
	handlerMap := *(*map[string][]pkgws.Handler)(unsafe.Pointer(handlers.UnsafeAddr()))
	values := handlerMap[channel]
	require.NotEmpty(t, values, "missing handler for %s", channel)
	values[0](data)
}

type personalWSAdapter struct {
	err error
}

func (a *personalWSAdapter) SubscribeTicker(context.Context, string) error          { return nil }
func (a *personalWSAdapter) UnsubscribeTicker(context.Context, string) error        { return nil }
func (a *personalWSAdapter) SubscribeKline(context.Context, string) error           { return nil }
func (a *personalWSAdapter) UnsubscribeKline(context.Context, string) error         { return nil }
func (a *personalWSAdapter) SubscribeDepth(context.Context, string, string) error   { return nil }
func (a *personalWSAdapter) UnsubscribeDepth(context.Context, string, string) error { return nil }
func (a *personalWSAdapter) SubscribePersonal(context.Context) error                { return nil }
func (a *personalWSAdapter) SetPool(*pkgws.Pool)                                    {}
func (a *personalWSAdapter) GetPingConfig() (interface{}, time.Duration)            { return nil, 0 }
func (a *personalWSAdapter) GetAuthHook(string, string) func(*pkgws.Client)         { return nil }
func (a *personalWSAdapter) GetChannelExtractor() func([]byte) string               { return nil }
func (a *personalWSAdapter) ParseTicker([]byte) (string, *store.PriceData, error) {
	return "", nil, nil
}
func (a *personalWSAdapter) ParseDepth([]byte) (string, *shared.OrderBook, error) {
	return "", nil, nil
}
func (a *personalWSAdapter) ParseKline([]byte) (string, *shared.Kline, error) {
	return "", nil, nil
}
func (a *personalWSAdapter) ParseOrder([]byte) (*exchange.WsOrderDeal, error) {
	if a.err != nil {
		return nil, a.err
	}
	return &exchange.WsOrderDeal{Symbol: "BTC_USDT", OrderID: "order-1"}, nil
}
func (a *personalWSAdapter) ParseOrderDeal([]byte) (*exchange.PersonalOrderDeal, error) {
	if a.err != nil {
		return nil, a.err
	}
	return &exchange.PersonalOrderDeal{Symbol: "BTC_USDT", Side: 1, OrderID: "order-1"}, nil
}
func (a *personalWSAdapter) ParseTrackOrder([]byte) (*exchange.PersonalTrackOrderUpdate, error) {
	if a.err != nil {
		return nil, a.err
	}
	return &exchange.PersonalTrackOrderUpdate{ID: "track-1", Symbol: "BTC_USDT", OrderID: "order-1"}, nil
}
func (a *personalWSAdapter) ParsePosition([]byte) (*exchange.PersonalPositionUpdate, error) {
	if a.err != nil {
		return nil, a.err
	}
	return &exchange.PersonalPositionUpdate{Symbol: "BTC_USDT"}, nil
}

type appTickerReader struct {
	ticker *store.TickerData
	err    error
}

func (r *appTickerReader) GetTicker(context.Context, string) (*store.TickerData, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.ticker, nil
}

func (r *appTickerReader) GetAllTickers(context.Context) []*store.TickerData {
	if r.ticker == nil {
		return nil
	}
	return []*store.TickerData{r.ticker}
}

type appContractReader struct {
	contract *store.ContractData
	err      error
}

type appFundingReader struct {
	settle time.Time
	err    error
}

func (r appFundingReader) GetFunding(context.Context, string) (*store.FundingData, error) {
	return &store.FundingData{}, nil
}

func (r appFundingReader) GetSettleTime(context.Context, string) (time.Time, error) {
	if r.err != nil {
		return time.Time{}, r.err
	}
	return r.settle, nil
}

func (r *appContractReader) GetContract(context.Context, string) (*store.ContractData, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.contract, nil
}

type appClock struct {
	until    time.Duration
	sleepErr error
}

func (c *appClock) Now() time.Time                { return time.Now() }
func (c *appClock) Until(time.Time) time.Duration { return c.until }
func (c *appClock) GetServerTime() int64          { return time.Now().UnixMilli() }
func (c *appClock) LatencyMs() int64              { return 0 }
func (c *appClock) Offset() int64                 { return 0 }
func (c *appClock) IsHealthy() bool               { return true }
func (c *appClock) MsUntilTarget(targetServerTimeMs int64) int64 {
	return targetServerTimeMs - time.Now().UnixMilli()
}
func (c *appClock) Sleep(context.Context, time.Duration) error { return c.sleepErr }

func requireTopic(t *testing.T, rt *cycle.Runtime, topic string) events.JournalEnvelope {
	t.Helper()
	evts := rt.JourneyEvents()
	for i := range evts {
		if evts[i].Topic == topic {
			return evts[i]
		}
	}
	t.Fatalf("topic %q not found", topic)
	return events.JournalEnvelope{}
}

func requireCandidateFound(t *testing.T, rt *cycle.Runtime, topic string) events.CandidateFoundEvent {
	t.Helper()
	env := requireTopic(t, rt, topic)
	evt, err := cycle.Unmarshal[events.CandidateFoundEvent](env.Payload)
	require.NoError(t, err)
	return evt
}

func requireAbort(t *testing.T, rt *cycle.Runtime) events.CycleAbortEvent {
	t.Helper()
	env := requireTopic(t, rt, events.TopicReversionAbort)
	evt, err := cycle.Unmarshal[events.CycleAbortEvent](env.Payload)
	require.NoError(t, err)
	return evt
}

func requirePositionClosed(t *testing.T, rt *cycle.Runtime) events.PositionClosedEvent {
	t.Helper()
	env := requireTopic(t, rt, events.TopicTrapPositionClosed)
	evt, err := cycle.Unmarshal[events.PositionClosedEvent](env.Payload)
	require.NoError(t, err)
	return evt
}

func requireError(t *testing.T, rt *cycle.Runtime) events.CycleErrorEvent {
	t.Helper()
	env := requireTopic(t, rt, events.TopicTrapError)
	evt, err := cycle.Unmarshal[events.CycleErrorEvent](env.Payload)
	require.NoError(t, err)
	return evt
}
