package cycle_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"crypto-bot/internal/bots/funding/application/cycle"
	"crypto-bot/internal/bots/funding/application/events"
	"crypto-bot/internal/bots/funding/config"
	fundingdomain "crypto-bot/internal/bots/funding/domain"
	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/notifier"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/internal/testutil/mocks"
	"crypto-bot/pkg/tracectx"
	"crypto-bot/pkg/types"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestRuntimeAccessorsAndStateHelpers(t *testing.T) {
	t.Parallel()

	rt := newRuntimeWithDepsForTest(t, cycle.Deps{})

	assert.Equal(t, "req-1", rt.GetReqID())
	assert.Equal(t, "BTC_USDT", rt.Config().Symbol)
	assert.NotNil(t, rt.Global())
	assert.NotNil(t, rt.Deps())
	assert.NotNil(t, rt.Log())

	rt.UpdateCandidate(func(c *fundingdomain.Candidate) {
		c.Symbol = "ETH_USDT"
		c.Volume = 7
	})
	assert.Equal(t, "ETH_USDT", rt.CandidateCopy().Symbol)
	assert.Equal(t, 7.0, rt.CandidateCopy().Volume)

	rt.AppendResult("result")
	rt.MarkReversionOrder("")
	_, ok := rt.ReversionFill()
	assert.False(t, ok)

	fill := events.OrderFilledEvent{Symbol: "BTC_USDT", FillVol: 2}
	rt.MarkReversionFill(fill)
	gotFill, ok := rt.ReversionFill()
	require.True(t, ok)
	assert.Equal(t, 2.0, gotFill.FillVol)
	assert.False(t, rt.TryMarkReversionFill(events.OrderFilledEvent{Symbol: "BTC_USDT", OrderID: "", FillVol: 3}))
	assert.True(t, rt.TryMarkReversionFill(events.OrderFilledEvent{Symbol: "BTC_USDT", OrderID: "order-2", FillVol: 3}))
	gotFill, ok = rt.ReversionFill()
	require.True(t, ok)
	assert.Equal(t, 3.0, gotFill.FillVol)

	assert.NoError(t, rt.PublishStart(time.Now()))
	rt.Publish(context.Background(), events.TopicTrapSkipped, events.TrapSkippedEvent{Symbol: "BTC_USDT"})
	rt.DumpTimeline(discardCycleLogger())
}

func TestRuntimeBuildEnrichRefreshAndRisk(t *testing.T) {
	t.Parallel()

	priceStore := store.NewPriceStore()
	priceStore.UpdatePrice("BTC_USDT", &store.PriceData{
		LastPrice: 101,
		BestBid:   100,
		BestAsk:   102,
		UpdatedAt: time.Now(),
	})
	contract := &fakeContractReader{data: &store.ContractData{
		Symbol:       "BTC_USDT",
		PriceUnit:    0.1,
		VolUnit:      1,
		MinVol:       1,
		PriceScale:   1,
		VolScale:     0,
		ContractSize: 1,
		TakerFeeRate: 0.001,
		MakerFeeRate: 0.0005,
	}}
	rt := newRuntimeWithDepsForTest(t, cycle.Deps{
		PriceStore:    priceStore,
		ContractStore: contract,
	})

	candidate := rt.BuildCandidate(&store.TickerData{
		Symbol:      "BTC_USDT",
		FundingRate: 0.02,
		LastPrice:   100,
		BestBid:     99,
		BestAsk:     101,
		Volume24:    1000,
		Amount24:    100000,
	})
	assert.Equal(t, shared.SideOpenLong, candidate.Side)
	assert.True(t, rt.Enrich(context.Background(), &candidate))
	assert.Equal(t, 0.1, candidate.PriceUnit)
	require.NoError(t, rt.RefreshPrice(context.Background(), &candidate))
	assert.Equal(t, 102.0, candidate.BestAsk)

	assert.NoError(t, rt.CycleRiskAllowsReversion(candidate))
	assert.NoError(t, rt.CycleRiskAllowsTrap(candidate, 10))
	assert.Equal(t, 2.0, cycle.CalcSpreadPct(100, 102))

	contract.err = errors.New("missing")
	assert.False(t, rt.Enrich(context.Background(), &candidate))
}

func TestRuntimeClockAndRetryHelpers(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{}
	rt := newRuntimeWithDepsForTest(t, cycle.Deps{Clock: clock})

	assert.True(t, rt.WaitUntil(context.Background(), time.Now().Add(time.Second)))
	assert.True(t, rt.Sleep(context.Background(), time.Millisecond))

	var tries int
	attempts, err := rt.RetryWithBackoffOpts(context.Background(), 3, time.Millisecond, time.Millisecond, func() error {
		tries++
		if tries < 2 {
			return errors.New("try again")
		}
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 2, attempts)

	_, err = rt.RetryWithBackoffOpts(context.Background(), 2, time.Millisecond, time.Millisecond, func() error {
		return errors.New("still failing")
	})
	require.Error(t, err)

	var wrapperTries int
	attempts, err = rt.RetryWithBackoff(context.Background(), 2, func() error {
		wrapperTries++
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 1, attempts)

	sleepErrClock := &fakeClock{sleepErr: context.Canceled}
	rt = newRuntimeWithDepsForTest(t, cycle.Deps{Clock: sleepErrClock})
	attempts, err = rt.RetryWithBackoffOpts(context.Background(), 3, time.Millisecond, time.Millisecond, func() error {
		return errors.New("sleep stops retry")
	})
	require.Error(t, err)
	assert.Equal(t, 1, attempts)
	assert.False(t, rt.Sleep(context.Background(), time.Millisecond))

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	assert.False(t, rt.WaitUntil(cancelled, time.Now().Add(-time.Second)))
}

func TestRuntimeSubscribeReceivesPublishedMessage(t *testing.T) {
	t.Parallel()

	rt := newRuntimeWithDepsForTest(t, cycle.Deps{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	received := make(chan struct{}, 1)
	rt.Subscribe(ctx, events.TopicTrapSkipped, func(_ *message.Message) {
		received <- struct{}{}
	})

	rt.Publish(context.Background(), events.TopicTrapSkipped, events.TrapSkippedEvent{Symbol: "BTC_USDT"})

	require.Eventually(t, func() bool {
		select {
		case <-received:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)

	cancel()
}

func TestRuntimeNotifiesImportantTradingEvents(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	n := mocks.NewMockNotifier(ctrl)
	sent := make(chan notifier.Event, 1)
	n.EXPECT().
		Send(gomock.Any(), gomock.AssignableToTypeOf(notifier.Event{})).
		DoAndReturn(func(ctx context.Context, evt notifier.Event) error {
			require.Equal(t, "req-1", tracectx.CorrelationID(ctx))
			require.Equal(t, "cycle-1", tracectx.CycleID(ctx))
			sent <- evt
			return nil
		})

	ctx := tracectx.WithReqID(context.Background(), "req-1")
	ctx = tracectx.WithCycleID(ctx, "cycle-1")
	rt := newRuntimeWithDepsForTest(t, cycle.Deps{Notifier: n})
	rt.RecordAndPublishCtx(ctx, "req-1", events.TopicReversionAbort, events.CycleAbortEvent{
		Flow:   events.FlowReversion,
		Symbol: "BTC_USDT",
		Reason: "risk blocked",
	})

	require.Eventually(t, func() bool {
		select {
		case evt := <-sent:
			return evt.Level == notifier.LevelTrading && evt.Symbol == "BTC_USDT"
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)
}

func TestRuntimeExcursionRecordsOnlyAfterFill(t *testing.T) {
	t.Parallel()

	priceStore := store.NewPriceStore()
	priceStore.UpdatePrice("BTC_USDT", &store.PriceData{
		LastPrice: 100,
		UpdatedAt: time.Now(),
	})
	rt := newRuntimeWithDepsForTest(t, cycle.Deps{PriceStore: priceStore})

	assert.False(t, rt.FinalizeExcursion(context.Background(), "req-1"))

	rt.RecordAndPublish(context.Background(), "req-1", events.TopicTrapOrderFilled, events.OrderFilledEvent{
		Flow:   events.FlowTrap,
		Symbol: "BTC_USDT",
	})
	assert.True(t, rt.FinalizeExcursion(context.Background(), "req-1"))

	rt.StartExcursionPriceStream(context.Background(), "req-1")
	priceStore.UpdatePrice("BTC_USDT", &store.PriceData{
		LastPrice: 105,
		UpdatedAt: time.Now(),
	})
	require.Eventually(t, func() bool {
		for _, evt := range rt.JourneyEvents() {
			if evt.Topic == events.TopicExcursionPriceObserved {
				return true
			}
		}
		return false
	}, time.Second, 10*time.Millisecond)
	rt.StopExcursionPriceStream()
}

func TestRuntimeUnmarshal(t *testing.T) {
	t.Parallel()

	evt, err := cycle.Unmarshal[events.CycleAbortEvent]([]byte(`{"symbol":"BTC_USDT","reason":"x"}`))
	require.NoError(t, err)
	assert.Equal(t, "x", evt.Reason)
}

func TestRuntimeSubscriptionsAbortAndWSOrderEvents(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	ws := mocks.NewMockSubscriber(ctrl)
	orderNotifier := mocks.NewMockOrderNotifier(ctrl)
	rt := newRuntimeWithDepsForTest(t, cycle.Deps{
		WsSub:         ws,
		OrderNotifier: orderNotifier,
	})

	ws.EXPECT().SubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil)
	require.NoError(t, rt.SubscribeAll(context.Background()))
	ws.EXPECT().UnsubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil)
	rt.UnsubscribeAll(context.Background())

	rt.Abort(context.Background(), "req-1", "scan", "bad scan")
	requireTopicInRuntime(t, rt, events.TopicScanCandidateFound)
	requireTopicInRuntime(t, rt, events.TopicReversionAbort)

	orderNotifier.EXPECT().
		OnPositionUpdate(gomock.Any(), "BTC_USDT", 30*time.Second, gomock.Any()).
		Do(func(_ context.Context, _ string, _ time.Duration, callback func(exchange.PersonalPositionUpdate)) {
			callback(exchange.PersonalPositionUpdate{
				Symbol:         "BTC_USDT",
				PositionType:   1,
				HoldVol:        2,
				HoldAvgPrice:   100,
				LiquidatePrice: 80,
				Realized:       3,
				Leverage:       5,
			})
		})
	rt.SubscribePositionLifecycle(context.Background(), "req-1", "BTC_USDT")

	requireTopicInRuntime(t, rt, events.TopicPositionUpdated)
}

func TestRuntimePublishesReversionPositionClosedFromWSPositionUpdate(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	orderNotifier := mocks.NewMockOrderNotifier(ctrl)
	rt := newRuntimeWithDepsForTest(t, cycle.Deps{OrderNotifier: orderNotifier})
	rt.MarkReversionFill(events.OrderFilledEvent{
		Flow:      events.FlowReversion,
		Symbol:    "BTC_USDT",
		Side:      shared.SideOpenLong,
		CloseSide: shared.SideCloseLong,
		FillVol:   2,
	})

	orderNotifier.EXPECT().
		OnPositionUpdate(gomock.Any(), "BTC_USDT", 30*time.Second, gomock.Any()).
		Do(func(_ context.Context, _ string, _ time.Duration, callback func(exchange.PersonalPositionUpdate)) {
			callback(exchange.PersonalPositionUpdate{
				Symbol:          "BTC_USDT",
				PositionType:    1,
				HoldVol:         0,
				CloseVol:        2,
				CloseAvgPrice:   101,
				CloseProfitLoss: 4,
				Fee:             0.1,
			})
			callback(exchange.PersonalPositionUpdate{
				Symbol:          "BTC_USDT",
				PositionType:    1,
				HoldVol:         0,
				CloseVol:        2,
				CloseAvgPrice:   101,
				CloseProfitLoss: 4,
			})
		})

	rt.SubscribePositionLifecycle(context.Background(), "req-1", "BTC_USDT")

	closed := requireRuntimePositionClosed(t, rt, events.TopicReversionPositionClosed)
	assert.Equal(t, events.FlowReversion, closed.Flow)
	assert.Equal(t, "position_update_closed", closed.Reason)
	assert.Equal(t, "ws_position", closed.Method)
	assert.InDelta(t, 101, closed.ClosePrice, 1e-9)
	assert.InDelta(t, 2, closed.CloseVol, 1e-9)
	assert.InDelta(t, 4, closed.Profit, 1e-9)
	assert.InDelta(t, 0.1, closed.Fee, 1e-9)
	assert.True(t, rt.IsFlowTerminal(events.FlowReversion))
	assert.Equal(t, 1, countRuntimeTopic(rt, events.TopicReversionPositionClosed))
}

func TestRuntimePublishesTrapPositionClosedFromWSPositionUpdate(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	orderNotifier := mocks.NewMockOrderNotifier(ctrl)
	rt := newRuntimeWithDepsForTest(t, cycle.Deps{OrderNotifier: orderNotifier})
	rt.MarkTrapFill(events.OrderFilledEvent{
		Flow:      events.FlowTrap,
		Symbol:    "BTC_USDT",
		Side:      shared.SideOpenShort,
		CloseSide: shared.SideCloseShort,
		FillVol:   3,
	})

	orderNotifier.EXPECT().
		OnPositionUpdate(gomock.Any(), "BTC_USDT", 30*time.Second, gomock.Any()).
		Do(func(_ context.Context, _ string, _ time.Duration, callback func(exchange.PersonalPositionUpdate)) {
			callback(exchange.PersonalPositionUpdate{
				Symbol:           "BTC_USDT",
				PositionType:     2,
				HoldVol:          0,
				NewCloseAvgPrice: 99,
				CloseVol:         3,
				Realized:         5,
			})
		})

	rt.SubscribePositionLifecycle(context.Background(), "req-1", "BTC_USDT")

	closed := requireRuntimePositionClosed(t, rt, events.TopicTrapPositionClosed)
	assert.Equal(t, events.FlowTrap, closed.Flow)
	assert.InDelta(t, 99, closed.ClosePrice, 1e-9)
	assert.InDelta(t, 3, closed.CloseVol, 1e-9)
	assert.InDelta(t, 5, closed.Profit, 1e-9)
	trapOrder, hasTrapOrder, trapFill, hasTrapFill, terminal := rt.TrapSnapshot()
	assert.Empty(t, trapOrder)
	assert.False(t, hasTrapOrder)
	assert.Equal(t, events.FlowTrap, trapFill.Flow)
	assert.True(t, hasTrapFill)
	assert.True(t, terminal)
}

func TestRuntimePublishesPositionClosedFromFlatWSPositionUpdate(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	orderNotifier := mocks.NewMockOrderNotifier(ctrl)
	rt := newRuntimeWithDepsForTest(t, cycle.Deps{OrderNotifier: orderNotifier})
	rt.MarkReversionFill(events.OrderFilledEvent{
		Flow:      events.FlowReversion,
		Symbol:    "BTC_USDT",
		Side:      shared.SideOpenLong,
		CloseSide: shared.SideCloseLong,
		FillVol:   2,
	})

	orderNotifier.EXPECT().
		OnPositionUpdate(gomock.Any(), "BTC_USDT", 30*time.Second, gomock.Any()).
		Do(func(_ context.Context, _ string, _ time.Duration, callback func(exchange.PersonalPositionUpdate)) {
			callback(exchange.PersonalPositionUpdate{
				Symbol:          "BTC_USDT",
				PositionType:    0,
				HoldVol:         0,
				CloseVol:        2,
				CloseAvgPrice:   100.5,
				CloseProfitLoss: 3,
			})
		})

	rt.SubscribePositionLifecycle(context.Background(), "req-1", "BTC_USDT")

	closed := requireRuntimePositionClosed(t, rt, events.TopicReversionPositionClosed)
	assert.Equal(t, events.FlowReversion, closed.Flow)
	assert.Equal(t, "position_update_closed", closed.Reason)
	assert.Equal(t, "ws_position", closed.Method)
	assert.InDelta(t, 100.5, closed.ClosePrice, 1e-9)
	assert.InDelta(t, 2, closed.CloseVol, 1e-9)
}

func TestRuntimeIgnoresAmbiguousFlatWSPositionUpdate(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	orderNotifier := mocks.NewMockOrderNotifier(ctrl)
	rt := newRuntimeWithDepsForTest(t, cycle.Deps{OrderNotifier: orderNotifier})
	rt.MarkReversionFill(events.OrderFilledEvent{
		Flow:    events.FlowReversion,
		Symbol:  "BTC_USDT",
		Side:    shared.SideOpenLong,
		FillVol: 2,
	})
	rt.MarkTrapFill(events.OrderFilledEvent{
		Flow:    events.FlowTrap,
		Symbol:  "BTC_USDT",
		Side:    shared.SideOpenShort,
		FillVol: 3,
	})

	orderNotifier.EXPECT().
		OnPositionUpdate(gomock.Any(), "BTC_USDT", 30*time.Second, gomock.Any()).
		Do(func(_ context.Context, _ string, _ time.Duration, callback func(exchange.PersonalPositionUpdate)) {
			callback(exchange.PersonalPositionUpdate{
				Symbol:       "BTC_USDT",
				PositionType: 0,
				HoldVol:      0,
			})
		})

	rt.SubscribePositionLifecycle(context.Background(), "req-1", "BTC_USDT")

	assert.Equal(t, 0, countRuntimeTopic(rt, events.TopicReversionPositionClosed))
	assert.Equal(t, 0, countRuntimeTopic(rt, events.TopicTrapPositionClosed))
}

func TestRuntimePositionWatcherUsesConfiguredCloseWindow(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	orderNotifier := mocks.NewMockOrderNotifier(ctrl)
	logger := discardCycleLogger()
	rt := cycle.NewRuntime(config.SymbolConfig{
		Symbol: "BTC_USDT",
		FundingTrap: fundingdomain.FundingTrapConfig{
			PostSettleTimeout: types.Duration(time.Minute),
		},
	}, &config.Config{System: &config.SystemConfig{}}, cycle.Deps{
		Log:           logger,
		OrderNotifier: orderNotifier,
	})
	rt.Begin(context.Background(), "req-1", time.Now().Add(time.Minute), logger)
	t.Cleanup(func() {
		require.NoError(t, rt.CloseBus())
	})

	orderNotifier.EXPECT().
		OnPositionUpdate(gomock.Any(), "BTC_USDT", 65*time.Second, gomock.Any())

	rt.SubscribePositionLifecycle(context.Background(), "req-1", "BTC_USDT")
}

type fakeContractReader struct {
	data *store.ContractData
	err  error
}

func (r *fakeContractReader) GetContract(context.Context, string) (*store.ContractData, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.data, nil
}

type fakeClock struct {
	calls    int
	sleepErr error
}

func (c *fakeClock) Now() time.Time { return time.Now() }
func (c *fakeClock) Until(time.Time) time.Duration {
	return time.Millisecond
}
func (c *fakeClock) GetServerTime() int64 { return time.Now().UnixMilli() }
func (c *fakeClock) LatencyMs() int64     { return 10 }
func (c *fakeClock) Offset() int64        { return 0 }
func (c *fakeClock) IsHealthy() bool      { return true }
func (c *fakeClock) MsUntilTarget(targetServerTimeMs int64) int64 {
	return targetServerTimeMs - time.Now().UnixMilli()
}
func (c *fakeClock) Sleep(context.Context, time.Duration) error {
	c.calls++
	return c.sleepErr
}

func newRuntimeWithDepsForTest(t *testing.T, deps cycle.Deps) *cycle.Runtime {
	t.Helper()

	logger := discardCycleLogger()
	if deps.Log == nil {
		deps.Log = logger
	}
	if deps.Clock == nil {
		deps.Clock = &fakeClock{}
	}
	cfg := config.SymbolConfig{
		Symbol:              "BTC_USDT",
		MaxPriceDiffPercent: 0.2,
		MarginUSDT:          100,
		Leverage:            5,
		FundingReversion: fundingdomain.FundingReversionConfig{
			StopLossPct:       0.01,
			PostSettleTimeout: types.Duration(time.Second),
		},
		FundingTrap: fundingdomain.FundingTrapConfig{
			StopLossPct: 0.01,
		},
	}
	global := &config.Config{System: &config.SystemConfig{}}
	rt := cycle.NewRuntime(cfg, global, deps)
	rt.Begin(context.Background(), "req-1", time.Now().Add(time.Minute), logger)
	rt.SetCandidate(fundingdomain.Candidate{
		Config: cycle.ToTradeConfig(cfg),
		TradeIntent: fundingdomain.TradeIntent{
			Symbol:    "BTC_USDT",
			Side:      shared.SideOpenLong,
			CloseSide: shared.SideCloseLong,
		},
		ContractSpec: fundingdomain.ContractSpec{
			PriceUnit:    0.1,
			PriceScale:   1,
			VolScale:     0,
			ContractSize: 1,
			MinVol:       1,
		},
		MarketData: fundingdomain.MarketData{
			LastPrice: 100,
			BestBid:   99,
			BestAsk:   101,
		},
	})
	t.Cleanup(func() {
		require.NoError(t, rt.CloseBus())
	})
	return rt
}

func discardCycleLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func requireTopicInRuntime(t *testing.T, rt *cycle.Runtime, topic string) {
	t.Helper()
	evts := rt.JourneyEvents()
	for i := range evts {
		if evts[i].Topic == topic {
			return
		}
	}
	t.Fatalf("topic %q not found", topic)
}

func requireRuntimePositionClosed(t *testing.T, rt *cycle.Runtime, topic string) events.PositionClosedEvent {
	t.Helper()
	evts := rt.JourneyEvents()
	for i := range evts {
		if evts[i].Topic != topic {
			continue
		}
		evt, err := cycle.Unmarshal[events.PositionClosedEvent](evts[i].Payload)
		require.NoError(t, err)
		return evt
	}
	t.Fatalf("topic %q not found", topic)
	return events.PositionClosedEvent{}
}

func countRuntimeTopic(rt *cycle.Runtime, topic string) int {
	count := 0
	evts := rt.JourneyEvents()
	for i := range evts {
		if evts[i].Topic == topic {
			count++
		}
	}
	return count
}
