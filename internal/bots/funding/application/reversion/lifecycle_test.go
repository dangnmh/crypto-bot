//nolint:testpackage // These tests exercise unexported lifecycle handlers directly.
package reversion

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
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/internal/infrastructure/watcher"
	"crypto-bot/internal/testutil/mocks"
	"crypto-bot/pkg/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestWatchStaticCloseDealPublishesTPClose(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	notifier := mocks.NewMockOrderNotifier(ctrl)
	rt := newReversionTestRuntime(t, client, notifier)

	fill := events.OrderFilledEvent{
		Flow:      events.FlowReversion,
		Symbol:    "BTC_USDT",
		OrderID:   "ioc-1",
		Side:      shared.SideOpenLong,
		CloseSide: shared.SideCloseLong,
		FillPrice: 100,
		FillVol:   2,
	}

	notifier.EXPECT().
		OnOrderDealBySymbolSide(gomock.Any(), "BTC_USDT", int(shared.SideCloseLong), time.Second, gomock.Any()).
		Do(func(_ context.Context, _ string, _ int, _ time.Duration, callback func(exchange.PersonalOrderDeal)) {
			callback(exchange.PersonalOrderDeal{
				Symbol:  "BTC_USDT",
				Side:    int(shared.SideCloseLong),
				Vol:     2,
				Price:   103,
				Fee:     0.2,
				Profit:  5,
				OrderID: "close-1",
			})
		})

	watchStaticCloseDeal(context.Background(), rt, fill)

	closeEvt := requirePositionClosedEvent(t, rt, events.TopicReversionPositionClosed)
	assert.Equal(t, events.FlowReversion, closeEvt.Flow)
	assert.Equal(t, "tp", closeEvt.Reason)
	assert.Equal(t, "static_tp_sl", closeEvt.Method)
	assert.InDelta(t, 103, closeEvt.ClosePrice, 1e-9)
	assert.True(t, rt.IsFlowTerminal(events.FlowReversion))
}

func TestWatchStaticCloseDealPublishesSLClose(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	notifier := mocks.NewMockOrderNotifier(ctrl)
	rt := newReversionTestRuntime(t, client, notifier)

	fill := events.OrderFilledEvent{
		Flow:      events.FlowReversion,
		Symbol:    "BTC_USDT",
		OrderID:   "ioc-1",
		Side:      shared.SideOpenLong,
		CloseSide: shared.SideCloseLong,
		FillPrice: 100,
		FillVol:   2,
	}

	notifier.EXPECT().
		OnOrderDealBySymbolSide(gomock.Any(), "BTC_USDT", int(shared.SideCloseLong), time.Second, gomock.Any()).
		Do(func(_ context.Context, _ string, _ int, _ time.Duration, callback func(exchange.PersonalOrderDeal)) {
			callback(exchange.PersonalOrderDeal{
				Symbol:  "BTC_USDT",
				Side:    int(shared.SideCloseLong),
				Vol:     2,
				Price:   97,
				Fee:     0.2,
				Profit:  -4,
				OrderID: "close-1",
			})
		})

	watchStaticCloseDeal(context.Background(), rt, fill)

	closeEvt := requirePositionClosedEvent(t, rt, events.TopicReversionPositionClosed)
	assert.Equal(t, "sl", closeEvt.Reason)
	assert.Equal(t, "static_tp_sl", closeEvt.Method)
	assert.InDelta(t, 97, closeEvt.ClosePrice, 1e-9)
	assert.True(t, rt.IsFlowTerminal(events.FlowReversion))
}

func TestCancelTimedOutOrder_PublishesNoFillTimeout(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	rt := newReversionTestRuntime(t, client, nil)

	client.EXPECT().
		CancelOrder(gomock.Any(), "BTC_USDT", "limit-1").
		Return(nil)

	cancelTimedOutOrder(context.Background(), rt, events.IOCFiredEvent{
		Flow:      events.FlowReversion,
		Symbol:    "BTC_USDT",
		OrderID:   "limit-1",
		OrderType: exchange.OrderTypeLimit,
	}, time.Second, time.Now())

	timeoutEvt := requireTimeoutEvent(t, rt, events.TopicReversionTimeout)
	assert.Equal(t, events.FlowReversion, timeoutEvt.Flow)
	assert.Equal(t, reversionReasonNoFill, timeoutEvt.Reason)
	assert.Equal(t, 1, timeoutEvt.CloseRetryCount)
	assert.True(t, rt.IsFlowTerminal(events.FlowReversion))
}

func TestCancelTimedOutOrder_SkipsCancelForIOC(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	rt := newReversionTestRuntime(t, client, nil)

	cancelTimedOutOrder(context.Background(), rt, events.IOCFiredEvent{
		Flow:      events.FlowReversion,
		Symbol:    "BTC_USDT",
		OrderID:   "ioc-1",
		OrderType: exchange.OrderTypeIOC,
	}, time.Second, time.Now())

	timeoutEvt := requireTimeoutEvent(t, rt, events.TopicReversionTimeout)
	assert.Equal(t, events.FlowReversion, timeoutEvt.Flow)
	assert.Equal(t, reversionReasonNoFill, timeoutEvt.Reason)
	assert.Zero(t, timeoutEvt.CloseRetryCount)
	assert.True(t, rt.IsFlowTerminal(events.FlowReversion))
}

func TestForceCloseTimedOutPosition_PublishesTimeoutAndClose(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	rt := newReversionTestRuntime(t, client, nil)

	client.EXPECT().
		ClosePosition(gomock.Any(), "BTC_USDT", shared.SideCloseLong, 2.0, 1).
		Return(nil)

	forceCloseTimedOutPosition(context.Background(), rt, events.OrderFilledEvent{
		Flow:      events.FlowReversion,
		Symbol:    "BTC_USDT",
		OrderID:   "ioc-1",
		CloseSide: shared.SideCloseLong,
		FillVol:   2,
	}, time.Second, time.Now())

	timeoutEvt := requireTimeoutEvent(t, rt, events.TopicReversionTimeout)
	assert.Equal(t, "force_close", timeoutEvt.Reason)
	assert.True(t, timeoutEvt.ForceCloseAttempted)
	assert.True(t, timeoutEvt.ForceCloseSucceeded)

	closeEvt := requirePositionClosedEvent(t, rt, events.TopicReversionPositionClosed)
	assert.Equal(t, "timeout_force_close", closeEvt.Reason)
	assert.Equal(t, reversionMethodFallbackClose, closeEvt.Method)
	assert.True(t, rt.IsFlowTerminal(events.FlowReversion))
}

func TestHandleRecheckPublishesConfirmedAndAbortCases(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	rt := newReversionRuntimeWithDeps(t, client, nil, cycle.Deps{
		TickerStore: &fakeTickerReader{ticker: &store.TickerData{
			Symbol:      "BTC_USDT",
			FundingRate: 0.02,
		}},
	})

	handleRecheck(context.Background(), rt)

	confirmed := requireConfirmedEvent(t, rt)
	assert.Equal(t, events.FlowReversion, confirmed.Flow)
	assert.Equal(t, shared.SideOpenLong, confirmed.Side)

	rt = newReversionRuntimeWithDeps(t, client, nil, cycle.Deps{
		TickerStore: &fakeTickerReader{ticker: &store.TickerData{
			Symbol:      "BTC_USDT",
			FundingRate: -0.02,
		}},
	})

	handleRecheck(context.Background(), rt)

	confirmed = requireConfirmedEvent(t, rt)
	assert.True(t, confirmed.FRChanged)
	assert.True(t, rt.IsFlowTerminal(events.FlowReversion))
}

func TestSetupFillWatcherPublishesFillOrNoFillTimeout(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	notifier := mocks.NewMockOrderNotifier(ctrl)
	rt := newReversionTestRuntime(t, client, notifier)
	evt := events.IOCFiredEvent{
		Flow:      events.FlowReversion,
		Symbol:    "BTC_USDT",
		OrderID:   "ioc-1",
		Side:      shared.SideOpenLong,
		CloseSide: shared.SideCloseLong,
		TPPrice:   110,
		SLPrice:   95,
	}

	notifier.EXPECT().
		OnOrderUpdate(gomock.Any(), "ioc-1", 5*time.Second, gomock.Any()).
		Do(func(_ context.Context, _ string, _ time.Duration, callback func(exchange.WsOrderDeal)) {
			callback(exchange.WsOrderDeal{
				OrderID:      "ioc-1",
				State:        3,
				DealAvgPrice: 100,
				DealVol:      2,
				TakerFee:     0.1,
				MakerFee:     0.2,
				Profit:       3,
			})
		})
	notifier.EXPECT().RemoveOrderCallback("ioc-1")
	notifier.EXPECT().
		OnOrderDealBySymbolSide(gomock.Any(), "BTC_USDT", int(shared.SideCloseLong), time.Second, gomock.Any())

	setupFillWatcher(context.Background(), rt, evt)

	fill := requireOrderFilledEvent(t, rt, events.TopicReversionOrderFilled)
	assert.Equal(t, "ioc-1", fill.OrderID)
	assert.InDelta(t, 0.3, fill.Fee, 1e-9)
	assert.Equal(t, 3.0, fill.Profit)

	notifier = mocks.NewMockOrderNotifier(ctrl)
	rt = newReversionTestRuntime(t, client, notifier)
	notifier.EXPECT().
		OnOrderUpdate(gomock.Any(), "ioc-2", 5*time.Second, gomock.Any()).
		Do(func(_ context.Context, _ string, _ time.Duration, callback func(exchange.WsOrderDeal)) {
			callback(exchange.WsOrderDeal{OrderID: "ioc-2", State: 3})
		})
	notifier.EXPECT().RemoveOrderCallback("ioc-2")

	evt.OrderID = "ioc-2"
	setupFillWatcher(context.Background(), rt, evt)

	timeoutEvt := requireTimeoutEvent(t, rt, events.TopicReversionTimeout)
	assert.Equal(t, reversionReasonNoFill, timeoutEvt.Reason)
}

func TestHandleFireIOCPublishesSuccessAndLatencyAbort(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	clock := &fakeReversionClock{latency: 10}
	priceStore := store.NewPriceStore()
	priceStore.UpdatePrice("BTC_USDT", &store.PriceData{
		LastPrice: 100,
		BestBid:   99,
		BestAsk:   101,
		UpdatedAt: time.Now(),
	})
	rt := newReversionRuntimeWithDeps(t, client, nil, cycle.Deps{
		Clock:      clock,
		PriceStore: priceStore,
	})

	client.EXPECT().
		CreateOrder(gomock.Any(), gomock.AssignableToTypeOf(exchange.SubmitOrderRequest{})).
		Return("ioc-1", nil)

	handleFireIOC(context.Background(), rt)

	fired := requireIOCFiredEvent(t, rt)
	assert.Equal(t, "ioc-1", fired.OrderID)
	assert.Empty(t, fired.Error)

	clock = &fakeReversionClock{latency: (2 * time.Second).Milliseconds()}
	rt = newReversionRuntimeWithDeps(t, client, nil, cycle.Deps{Clock: clock})
	handleFireIOC(context.Background(), rt)
	abort := requireAbortEvent(t, rt)
	assert.Equal(t, "latency too high", abort.Reason)
}

func TestHandleArmRefreshesPriceAndPublishesArmed(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	ws := mocks.NewMockSubscriber(ctrl)
	priceStore := store.NewPriceStore()
	priceStore.UpdatePrice("BTC_USDT", &store.PriceData{
		LastPrice: 100,
		BestBid:   99,
		BestAsk:   101,
		Volume24:  1000,
		UpdatedAt: time.Now(),
	})
	rt := newReversionRuntimeWithDeps(t, client, nil, cycle.Deps{
		WsSub:      ws,
		PriceStore: priceStore,
	})

	ws.EXPECT().SubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil)

	handleArm(context.Background(), rt)

	env := requireEventTopic(t, rt, events.TopicReversionArmed)
	armed, err := cycle.Unmarshal[events.ArmedEvent](env.Payload)
	require.NoError(t, err)
	assert.True(t, armed.SafetyPassed)
	assert.Equal(t, 100.0, armed.LastPrice)
	assert.Greater(t, rt.CandidateCopy().Volume, 0.0)
}

func TestHandleArmAbortsOnSubscribeAndRefreshFailure(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	ws := mocks.NewMockSubscriber(ctrl)
	rt := newReversionRuntimeWithDeps(t, client, nil, cycle.Deps{WsSub: ws})

	ws.EXPECT().SubscribeTicker(gomock.Any(), "BTC_USDT").Return(errors.New("ws down"))

	handleArm(context.Background(), rt)

	assert.Equal(t, "WS subscribe failed", requireAbortEvent(t, rt).Reason)

	ws = mocks.NewMockSubscriber(ctrl)
	rt = newReversionRuntimeWithDeps(t, client, nil, cycle.Deps{
		WsSub:      ws,
		PriceStore: store.NewPriceStore(),
	})
	ws.EXPECT().SubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil)
	ws.EXPECT().UnsubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil)

	handleArm(context.Background(), rt)

	assert.Equal(t, "refresh price failed", requireAbortEvent(t, rt).Reason)
}

func TestHandleTimeoutForceClosesRecordedFill(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	rt := newReversionTestRuntime(t, client, nil)
	fill := events.OrderFilledEvent{
		Flow:      events.FlowReversion,
		Symbol:    "BTC_USDT",
		CloseSide: shared.SideCloseLong,
		FillVol:   2,
	}
	rt.MarkReversionFill(fill)

	client.EXPECT().
		ClosePosition(gomock.Any(), "BTC_USDT", shared.SideCloseLong, 2.0, 1).
		Return(nil)

	handleTimeout(context.Background(), rt, events.IOCFiredEvent{
		Flow:      events.FlowReversion,
		Symbol:    "BTC_USDT",
		OrderID:   "ioc-1",
		OrderType: exchange.OrderTypeLimit,
	})

	timeoutEvt := requireTimeoutEvent(t, rt, events.TopicReversionTimeout)
	assert.Equal(t, "force_close", timeoutEvt.Reason)
}

func TestRegisterSubscribesHandlers(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	rt := newReversionTestRuntime(t, client, nil)

	Register(context.Background(), rt)

	require.NotEmpty(t, rt.JourneyEvents())
}

func TestSubscribeWaitPublishesWaitComplete(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	rt := newReversionTestRuntime(t, client, nil)

	subscribeWait(context.Background(), rt)
	rt.Publish(events.TopicReversionArmed, events.ArmedEvent{Flow: events.FlowReversion, Symbol: "BTC_USDT"})

	require.Eventually(t, func() bool {
		for _, evt := range rt.JourneyEvents() {
			if evt.Topic == events.TopicReversionWaitComplete {
				return true
			}
		}
		return false
	}, time.Second, 10*time.Millisecond)
}

func TestForceClosePositionFallsBackAndCriticalPublish(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	rt := newReversionTestRuntime(t, client, nil)

	client.EXPECT().
		ClosePosition(gomock.Any(), "BTC_USDT", shared.SideCloseLong, 2.0, 1).
		Return(errors.New("exact failed")).Times(3)
	client.EXPECT().
		CloseAllPositions(gomock.Any(), "BTC_USDT").
		Return(nil)

	retries, err := forceClosePosition(context.Background(), rt, "BTC_USDT", shared.SideCloseLong, 2, 1)
	require.NoError(t, err)
	assert.Equal(t, 4, retries)

	publishReversionCritical(rt, "BTC_USDT", "critical")
	abort := requireAbortEvent(t, rt)
	assert.Equal(t, "critical", abort.Reason)
}

func newReversionTestRuntime(t *testing.T, client exchange.Client, notifier watcher.OrderNotifier) *cycle.Runtime {
	t.Helper()
	return newReversionRuntimeWithDeps(t, client, notifier, cycle.Deps{})
}

func newReversionRuntimeWithDeps(
	t *testing.T,
	client exchange.Client,
	notifier watcher.OrderNotifier,
	deps cycle.Deps,
) *cycle.Runtime {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.SymbolConfig{
		Symbol:              "BTC_USDT",
		MaxPriceDiffPercent: 0.2,
		MarginUSDT:          100,
		Leverage:            5,
		ParsedOpenType:      exchange.OpenTypeIsolated,
		ParsedPositionMode:  1,
		FundingReversion: fundingdomain.FundingReversionConfig{
			PostSettleTimeout: types.Duration(time.Second),
			MaxLatency:        types.Duration(time.Second),
		},
	}
	if deps.Client == nil {
		deps.Client = client
	}
	if deps.OrderNotifier == nil {
		deps.OrderNotifier = notifier
	}
	if deps.WsSub == nil {
		deps.WsSub = nil
	}
	if deps.Clock == nil {
		deps.Clock = &fakeReversionClock{}
	}
	if deps.Log == nil {
		deps.Log = logger
	}
	rt := cycle.NewRuntime(cfg, &config.Config{System: &config.SystemConfig{}}, cycle.Deps{
		Client:        deps.Client,
		WsSub:         deps.WsSub,
		OrderNotifier: deps.OrderNotifier,
		TickerStore:   deps.TickerStore,
		PriceStore:    deps.PriceStore,
		Clock:         deps.Clock,
		Log:           deps.Log,
	})
	rt.Begin("req-1", time.Now().Add(100*time.Millisecond), logger)
	rt.SetCandidate(fundingdomain.Candidate{
		Config: cycle.ToTradeConfig(cfg),
		TradeIntent: fundingdomain.TradeIntent{
			Symbol:      "BTC_USDT",
			Side:        shared.SideOpenLong,
			CloseSide:   shared.SideCloseLong,
			FundingRate: 0.02,
		},
		ContractSpec: fundingdomain.ContractSpec{
			PriceScale:   2,
			PriceUnit:    0.1,
			MinVol:       1,
			VolScale:     0,
			ContractSize: 1,
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

type fakeTickerReader struct {
	ticker *store.TickerData
	err    error
}

func (r *fakeTickerReader) GetTicker(context.Context, string) (*store.TickerData, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.ticker, nil
}

func (r *fakeTickerReader) GetAllTickers(context.Context) []*store.TickerData {
	if r.ticker == nil {
		return nil
	}
	return []*store.TickerData{r.ticker}
}

type fakeReversionClock struct {
	latency int64
}

func (c *fakeReversionClock) Now() time.Time { return time.Now() }
func (c *fakeReversionClock) Until(time.Time) time.Duration {
	return 0
}
func (c *fakeReversionClock) GetServerTime() int64 { return time.Now().UnixMilli() }
func (c *fakeReversionClock) LatencyMs() int64     { return c.latency }
func (c *fakeReversionClock) Offset() int64        { return 0 }
func (c *fakeReversionClock) IsHealthy() bool      { return true }
func (c *fakeReversionClock) MsUntilTarget(targetServerTimeMs int64) int64 {
	return targetServerTimeMs - time.Now().UnixMilli()
}
func (c *fakeReversionClock) Sleep(context.Context, time.Duration) error { return nil }

func requireEventTopic(t *testing.T, rt *cycle.Runtime, topic string) events.JournalEnvelope {
	t.Helper()
	log := rt.JourneyEvents()
	for i := range log {
		evt := log[i]
		if evt.Topic == topic {
			return evt
		}
	}
	t.Fatalf("topic %q not found", topic)
	return events.JournalEnvelope{}
}

func requirePositionClosedEvent(t *testing.T, rt *cycle.Runtime, topic string) events.PositionClosedEvent {
	t.Helper()
	env := requireEventTopic(t, rt, topic)
	evt, err := cycle.Unmarshal[events.PositionClosedEvent](env.Payload)
	require.NoError(t, err)
	return evt
}

func requireOrderFilledEvent(t *testing.T, rt *cycle.Runtime, topic string) events.OrderFilledEvent {
	t.Helper()
	env := requireEventTopic(t, rt, topic)
	evt, err := cycle.Unmarshal[events.OrderFilledEvent](env.Payload)
	require.NoError(t, err)
	return evt
}

func requireConfirmedEvent(t *testing.T, rt *cycle.Runtime) events.ConfirmedEvent {
	t.Helper()
	env := requireEventTopic(t, rt, events.TopicReversionConfirmed)
	evt, err := cycle.Unmarshal[events.ConfirmedEvent](env.Payload)
	require.NoError(t, err)
	return evt
}

func requireIOCFiredEvent(t *testing.T, rt *cycle.Runtime) events.IOCFiredEvent {
	t.Helper()
	env := requireEventTopic(t, rt, events.TopicReversionIOCFired)
	evt, err := cycle.Unmarshal[events.IOCFiredEvent](env.Payload)
	require.NoError(t, err)
	return evt
}

func requireAbortEvent(t *testing.T, rt *cycle.Runtime) events.CycleAbortEvent {
	t.Helper()
	env := requireEventTopic(t, rt, events.TopicReversionAbort)
	evt, err := cycle.Unmarshal[events.CycleAbortEvent](env.Payload)
	require.NoError(t, err)
	return evt
}

func requireTimeoutEvent(t *testing.T, rt *cycle.Runtime, topic string) events.CycleTimeoutEvent {
	t.Helper()
	env := requireEventTopic(t, rt, topic)
	evt, err := cycle.Unmarshal[events.CycleTimeoutEvent](env.Payload)
	require.NoError(t, err)
	return evt
}
