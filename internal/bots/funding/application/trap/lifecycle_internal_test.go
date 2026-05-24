package trap

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
	"crypto-bot/internal/infrastructure/watcher"
	"crypto-bot/internal/testutil/mocks"
	"crypto-bot/pkg/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestCloseTimedOutTrapPositionClosesFilledTrapBeforeCancel(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	clock := mocks.NewMockClock(ctrl)
	rt := newTrapTestRuntimeWithDeps(t, client, nil, clock, nil)

	fill := events.OrderFilledEvent{
		Flow:      events.FlowTrap,
		Symbol:    "BTC_USDT",
		OrderID:   "trap-1",
		CloseSide: shared.SideCloseShort,
		FillVol:   2,
	}

	client.EXPECT().
		ClosePosition(gomock.Any(), "BTC_USDT", shared.SideCloseShort, 2.0, 1).
		Return(nil)

	startedAt := time.Now()
	closeTimedOutTrapPosition(context.Background(), rt, fill, time.Second, startedAt, startedAt.Add(time.Second))

	timeoutEvt := requireTrapTimeoutEvent(t, rt, events.TopicTrapTimeout)
	assert.Equal(t, "force_close", timeoutEvt.Reason)
	assert.True(t, timeoutEvt.ForceCloseAttempted)
	assert.True(t, timeoutEvt.ForceCloseSucceeded)

	closeEvt := requireTrapPositionClosedEvent(t, rt)
	assert.Equal(t, "timeout_force_close", closeEvt.Reason)
	assert.Equal(t, trapMethodFallbackClose, closeEvt.Method)
	assert.True(t, rt.IsFlowTerminal(events.FlowTrap))
}

func TestSubscribeFillWatcherStoresPendingTrapOrder(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	notifier := mocks.NewMockOrderNotifier(ctrl)
	rt := newTrapTestRuntimeWithDeps(t, client, notifier, nil, nil)

	subscribeFillWatcher(context.Background(), rt)
	rt.Publish(context.Background(), events.TopicTrapOrderPlaced, events.TrapFiredEvent{
		Symbol:    "BTC_USDT",
		OrderID:   "trap-1",
		Side:      shared.SideOpenLong,
		CloseSide: shared.SideCloseShort,
	})

	var order events.TrapFiredEvent
	var hasOrder, hasFill, terminal bool
	require.Eventually(t, func() bool {
		order, hasOrder, _, hasFill, terminal = rt.TrapSnapshot()
		return hasOrder
	}, time.Second, 10*time.Millisecond)
	assert.Equal(t, "trap-1", order.OrderID)
	assert.False(t, hasFill)
	assert.False(t, terminal)
}

func TestSubscribeFillWatcherIgnoresEmptyOrderID(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	notifier := mocks.NewMockOrderNotifier(ctrl)
	rt := newTrapTestRuntimeWithDeps(t, client, notifier, nil, nil)

	subscribeFillWatcher(context.Background(), rt)
	rt.Publish(context.Background(), events.TopicTrapOrderPlaced, events.TrapFiredEvent{})
}

func TestPositionUpdatePublishesTrapFill(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	notifier := mocks.NewMockOrderNotifier(ctrl)
	rt := newTrapTestRuntimeWithDeps(t, client, notifier, nil, nil)

	rt.MarkTrapOrder(events.TrapFiredEvent{
		Flow:      events.FlowTrap,
		Symbol:    "BTC_USDT",
		OrderID:   "trap-fill",
		Side:      shared.SideOpenLong,
		CloseSide: shared.SideCloseShort,
	})
	notifier.EXPECT().OnPositionUpdate(gomock.Any(), "BTC_USDT", 30*time.Second, gomock.Any()).
		Do(func(_ context.Context, _ string, _ time.Duration, callback func(exchange.PersonalPositionUpdate)) {
			callback(exchange.PersonalPositionUpdate{
				Symbol:       "BTC_USDT",
				PositionType: 1,
				HoldVol:      3,
				HoldAvgPrice: 101,
			})
		})

	rt.SubscribePositionLifecycle(context.Background(), "req-1", "BTC_USDT")

	fill := requireTrapOrderFilledEvent(t, rt)
	assert.Equal(t, events.FlowTrap, fill.Flow)
	assert.Equal(t, "trap-fill", fill.OrderID)
	assert.Equal(t, 101.0, fill.FillPrice)
	assert.Equal(t, 3.0, fill.FillVol)
}

func TestSubscribeFireTrapSkipsDisabledHedgeTrap(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	rt := newTrapTestRuntime(t, client)

	subscribeFireTrap(context.Background(), rt)
	rt.Publish(context.Background(), events.TopicTrapCandidate, struct{}{})
}

func TestFireStaticTrapPublishesOrderPlaced(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	clock := mocks.NewMockClock(ctrl)
	rt := newTrapTestRuntimeWithDeps(t, client, nil, clock, nil)
	rt.SetCandidate(testTrapCandidate(shared.SideOpenShort))

	client.EXPECT().
		CreateOrder(gomock.Any(), gomock.AssignableToTypeOf(exchange.SubmitOrderRequest{})).
		DoAndReturn(func(_ context.Context, req exchange.SubmitOrderRequest) (string, error) {
			assert.Equal(t, int(shared.SideOpenLong), req.Side)
			assert.Equal(t, exchange.OrderTypeLimit, req.Type)
			return "trap-1", nil
		})

	fireStaticTrap(context.Background(), rt)

	evt := requireTrapFiredEvent(t, rt)
	assert.Equal(t, "trap-1", evt.OrderID)
	assert.Equal(t, trapSourceStaticLimit, evt.Source)
	assert.Equal(t, shared.SideOpenLong, evt.Side)
}

func TestFireStaticTrapSkipsInvalidAndOrderFailure(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	rt := newTrapTestRuntime(t, client)
	invalid := testTrapCandidate(shared.SideOpenShort)
	invalid.BestBid = 0
	rt.SetCandidate(invalid)

	fireStaticTrap(context.Background(), rt)

	skipped := requireTrapSkippedEvent(t, rt)
	assert.Equal(t, string(fundingdomain.TrapSkipReasonInvalidPrice), skipped.Reason)

	rt = newTrapTestRuntime(t, client)
	rt.SetCandidate(testTrapCandidate(shared.SideOpenShort))
	client.EXPECT().
		CreateOrder(gomock.Any(), gomock.Any()).
		Return("", errors.New("exchange down"))

	fireStaticTrap(context.Background(), rt)

	skipped = requireTrapSkippedEvent(t, rt)
	assert.Equal(t, string(fundingdomain.TrapSkipReasonOrderFailed), skipped.Reason)
	assert.Equal(t, "exchange down", skipped.Error)
}

func TestVerifyTrapWallAndFireOBTrap(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	depth := newStaticDepthReader(testOrderBook())
	rt := newTrapTestRuntimeWithDeps(t, client, nil, nil, depth)
	candidate := testTrapCandidate(shared.SideOpenShort)
	rt.SetCandidate(candidate)

	wall, ok := verifyTrapWall(context.Background(), rt, candidate, 98, time.Now())
	require.True(t, ok)
	assert.Equal(t, 98.1, wall.trapPrice)

	trapCandidate := candidate
	trapCandidate.Side = shared.SideOpenLong
	trapCandidate.CloseSide = shared.SideCloseLong

	client.EXPECT().
		CreateOrder(gomock.Any(), gomock.AssignableToTypeOf(exchange.SubmitOrderRequest{})).
		DoAndReturn(func(_ context.Context, req exchange.SubmitOrderRequest) (string, error) {
			assert.Equal(t, exchange.OrderTypePostOnly, req.Type)
			assert.Equal(t, int(shared.SideOpenLong), req.Side)
			assert.Equal(t, wall.trapPrice, req.Price)
			return "ob-trap-1", nil
		})

	fireOBTrap(context.Background(), rt, trapCandidate, wall)

	evt := requireTrapFiredEvent(t, rt)
	assert.Equal(t, "ob-trap-1", evt.OrderID)
	assert.Equal(t, trapSourceOBMonitor, evt.Source)
}

func TestHandleFireTrapUsesOrderBookWall(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	clock := mocks.NewMockClock(ctrl)
	depth := newStaticDepthReader(testOrderBook())
	rt := newTrapTestRuntimeWithDeps(t, client, nil, clock, depth)
	rt.SetCandidate(testTrapCandidate(shared.SideOpenShort))

	clock.EXPECT().Until(gomock.Any()).Return(time.Millisecond)
	clock.EXPECT().Sleep(gomock.Any(), time.Millisecond).Return(nil)
	client.EXPECT().
		CreateOrder(gomock.Any(), gomock.AssignableToTypeOf(exchange.SubmitOrderRequest{})).
		Return("ob-trap-1", nil)

	handleFireTrap(context.Background(), rt)

	evt := requireTrapFiredEvent(t, rt)
	assert.Equal(t, trapSourceOBMonitor, evt.Source)
	assert.Equal(t, "ob-trap-1", evt.OrderID)
}

func TestHandleFireTrapFallsBackWhenDepthMissing(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	clock := mocks.NewMockClock(ctrl)
	depth := &staticDepthReader{err: errors.New("no depth")}
	rt := newTrapTestRuntimeWithDeps(t, client, nil, clock, depth)
	rt.SetCandidate(testTrapCandidate(shared.SideOpenShort))

	clock.EXPECT().Until(gomock.Any()).Return(time.Millisecond)
	clock.EXPECT().Sleep(gomock.Any(), time.Millisecond).Return(nil)
	client.EXPECT().
		CreateOrder(gomock.Any(), gomock.Any()).
		Return("static-trap-1", nil)

	handleFireTrap(context.Background(), rt)

	evt := requireTrapFiredEvent(t, rt)
	assert.Equal(t, trapSourceStaticLimit, evt.Source)
}

func TestHandleFireTrapSkipsWhenContextCancelled(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	clock := mocks.NewMockClock(ctrl)
	rt := newTrapTestRuntimeWithDeps(t, client, nil, clock, newStaticDepthReader(testOrderBook()))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	clock.EXPECT().Until(gomock.Any()).Return(time.Millisecond)
	clock.EXPECT().Sleep(gomock.Any(), time.Millisecond).Return(context.Canceled)

	handleFireTrap(ctx, rt)
	assert.Empty(t, findTrapEvents(rt, events.TopicTrapOrderPlaced))
}

func TestHandleFireTrapSkipsWhenWallNotVerified(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	clock := mocks.NewMockClock(ctrl)
	depth := &sequenceDepthReader{books: []*shared.OrderBook{
		testOrderBook(),
		{Bids: []shared.OrderBookEntry{{Price: 99, Volume: 1}}, Asks: []shared.OrderBookEntry{{Price: 101, Volume: 1}}},
	}}
	rt := newTrapTestRuntimeWithDeps(t, client, nil, clock, depth)
	rt.SetCandidate(testTrapCandidate(shared.SideOpenShort))

	clock.EXPECT().Until(gomock.Any()).Return(time.Millisecond)
	clock.EXPECT().Sleep(gomock.Any(), time.Millisecond).Return(nil)

	handleFireTrap(context.Background(), rt)

	skipped := requireTrapSkippedEvent(t, rt)
	assert.Equal(t, string(fundingdomain.TrapSkipReasonWallNotVerified), skipped.Reason)
	requireTrapEventTopic(t, rt, events.TopicTrapWallVerified)
}

func TestVerifyTrapWallFailsOnMissingDepthOrInvalidTrapPrice(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	rt := newTrapTestRuntimeWithDeps(t, client, nil, nil, &staticDepthReader{err: errors.New("depth down")})
	candidate := testTrapCandidate(shared.SideOpenShort)

	_, ok := verifyTrapWall(context.Background(), rt, candidate, 98, time.Now())
	assert.False(t, ok)

	rt = newTrapTestRuntimeWithDeps(t, client, nil, nil, newStaticDepthReader(&shared.OrderBook{
		Bids: []shared.OrderBookEntry{{Price: 99, Volume: 1}},
		Asks: []shared.OrderBookEntry{{Price: 101, Volume: 1}},
	}))
	_, ok = verifyTrapWall(context.Background(), rt, candidate, 98, time.Now())
	assert.False(t, ok)
}

func TestFireOBTrapSkipsInvalidVolumeAndOrderFailure(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	rt := newTrapTestRuntime(t, client)
	candidate := testTrapCandidate(shared.SideOpenLong)
	candidate.Config.FundingTrap.SizeRatio = 0
	candidate.Config.FundingTrap.MaxNotionalUSDT = 0
	candidate.Volume = 0

	fireOBTrap(context.Background(), rt, candidate, wallVerification{price: 103, trapPrice: 103.1})

	skipped := requireTrapSkippedEvent(t, rt)
	assert.Equal(t, string(fundingdomain.TrapSkipReasonInvalidVolume), skipped.Reason)

	rt = newTrapTestRuntime(t, client)
	candidate = testTrapCandidate(shared.SideOpenLong)
	client.EXPECT().
		CreateOrder(gomock.Any(), gomock.AssignableToTypeOf(exchange.SubmitOrderRequest{})).
		Return("", errors.New("order failed"))

	fireOBTrap(context.Background(), rt, candidate, wallVerification{price: 103, trapPrice: 103.1})

	skipped = requireTrapSkippedEvent(t, rt)
	assert.Equal(t, string(fundingdomain.TrapSkipReasonOrderFailed), skipped.Reason)
	assert.Equal(t, "order failed", skipped.Error)
}

func TestSetupFillWatcherDoesNotPublishFillWithoutPosition(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	notifier := mocks.NewMockOrderNotifier(ctrl)
	rt := newTrapTestRuntimeWithDeps(t, client, notifier, nil, nil)

	setupFillWatcher(context.Background(), rt, "trap-1", shared.SideOpenLong, shared.SideCloseLong)

	assert.Empty(t, findTrapEvents(rt, events.TopicTrapOrderFilled))
}

func TestSetupFillWatcherSkipsEmptyOrderID(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	notifier := mocks.NewMockOrderNotifier(ctrl)
	rt := newTrapTestRuntimeWithDeps(t, client, notifier, nil, nil)

	setupFillWatcher(context.Background(), rt, "", shared.SideOpenLong, shared.SideCloseLong)
	assert.Empty(t, findTrapEvents(rt, events.TopicTrapOrderFilled))
}

func TestPositionUpdatePublishesTrapPositionClosed(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	notifier := mocks.NewMockOrderNotifier(ctrl)
	rt := newTrapTestRuntimeWithDeps(t, client, notifier, nil, nil)
	rt.MarkTrapFill(events.OrderFilledEvent{
		Flow:      events.FlowTrap,
		Symbol:    "BTC_USDT",
		Side:      shared.SideOpenLong,
		CloseSide: shared.SideCloseLong,
		FillVol:   2,
	})
	notifier.EXPECT().OnPositionUpdate(gomock.Any(), "BTC_USDT", 30*time.Second, gomock.Any()).
		Do(func(_ context.Context, _ string, _ time.Duration, callback func(exchange.PersonalPositionUpdate)) {
			callback(exchange.PersonalPositionUpdate{
				Symbol:          "BTC_USDT",
				PositionType:    1,
				HoldVol:         0,
				CloseVol:        2,
				CloseAvgPrice:   103,
				CloseProfitLoss: 5,
				Fee:             0.1,
			})
		})

	rt.SubscribePositionLifecycle(context.Background(), "req-1", "BTC_USDT")

	closed := requireTrapPositionClosedEvent(t, rt)
	assert.Equal(t, "position_update_closed", closed.Reason)
	assert.Equal(t, "ws_position", closed.Method)
	assert.Equal(t, 103.0, closed.ClosePrice)
}

func TestHandleTrailingPlacesTrackOrderWithActivation(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	notifier := mocks.NewMockOrderNotifier(ctrl)
	rt := newTrapTestRuntimeWithDeps(t, client, notifier, nil, nil)
	c := testTrapCandidate(shared.SideOpenLong)
	c.Config.FundingTrap.Trailing.Enabled = true
	c.Config.FundingTrap.Trailing.ActivationPct = 0.01
	c.Config.FundingTrap.Trailing.CallbackPct = 0.005
	rt.SetCandidate(c)
	fill := events.OrderFilledEvent{
		Flow:      events.FlowTrap,
		Symbol:    "BTC_USDT",
		CloseSide: shared.SideCloseLong,
		FillPrice: 100,
		FillVol:   2,
	}

	client.EXPECT().
		CreateTrackOrder(gomock.Any(), gomock.AssignableToTypeOf(exchange.SubmitTrackOrderRequest{})).
		DoAndReturn(func(_ context.Context, req exchange.SubmitTrackOrderRequest) (string, error) {
			assert.Equal(t, int(shared.SideCloseLong), req.Side)
			assert.Equal(t, 101.0, req.ActivePrice)
			assert.True(t, req.ReduceOnly)
			return "track-1", nil
		})
	handleTrailing(context.Background(), rt, fill)

	env := requireTrapEventTopic(t, rt, events.TopicTrapTrailingPlaced)
	evt, err := cycle.Unmarshal[events.TrailingPlacedEvent](env.Payload)
	require.NoError(t, err)
	assert.Equal(t, "track-1", evt.TrackID)
}

func TestHandleTrailingDisabledDoesNothing(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	rt := newTrapTestRuntime(t, client)
	c := testTrapCandidate(shared.SideOpenLong)
	c.Config.FundingTrap.Trailing.Enabled = false
	rt.SetCandidate(c)

	handleTrailing(context.Background(), rt, events.OrderFilledEvent{
		Flow:      events.FlowTrap,
		Symbol:    "BTC_USDT",
		CloseSide: shared.SideCloseLong,
		FillPrice: 100,
		FillVol:   2,
	})

	assert.Empty(t, findTrapEvents(rt, events.TopicTrapTrailingPlaced))
}

func TestHandleTrailingFallbackClosesWhenTrackOrderFails(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	rt := newTrapTestRuntime(t, client)
	c := testTrapCandidate(shared.SideOpenLong)
	c.Config.FundingTrap.Trailing.Enabled = true
	rt.SetCandidate(c)
	fill := events.OrderFilledEvent{
		Flow:      events.FlowTrap,
		Symbol:    "BTC_USDT",
		CloseSide: shared.SideCloseLong,
		FillVol:   2,
	}

	client.EXPECT().
		CreateTrackOrder(gomock.Any(), gomock.Any()).
		Return("", errors.New("track failed"))
	client.EXPECT().
		ClosePosition(gomock.Any(), "BTC_USDT", shared.SideCloseLong, 2.0, 1).
		Return(nil)

	handleTrailing(context.Background(), rt, fill)

	closed := requireTrapPositionClosedEvent(t, rt)
	assert.Equal(t, "trailing_failed_fallback", closed.Reason)
}

func TestHandleTrapOrderTimeoutCancelsUnfilledOrder(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	clock := mocks.NewMockClock(ctrl)
	rt := newTrapTestRuntimeWithDeps(t, client, nil, clock, nil)
	order := events.TrapFiredEvent{
		Flow:    events.FlowTrap,
		Symbol:  "BTC_USDT",
		OrderID: "trap-1",
	}

	clock.EXPECT().Sleep(gomock.Any(), time.Second).Return(nil)
	clock.EXPECT().Sleep(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	client.EXPECT().CancelOrder(gomock.Any(), "BTC_USDT", "trap-1").Return(nil)

	handleTrapOrderTimeout(context.Background(), rt, order)

	requireTopic := requireTrapEventTopic(t, rt, events.TopicTrapTimeout)
	timeoutEvt, err := cycle.Unmarshal[events.CycleTimeoutEvent](requireTopic.Payload)
	require.NoError(t, err)
	assert.Equal(t, events.FlowTrap, timeoutEvt.Flow)
}

func TestHandleTrapOrderTimeoutClosesRecordedFill(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	clock := mocks.NewMockClock(ctrl)
	rt := newTrapTestRuntimeWithDeps(t, client, nil, clock, nil)
	fill := events.OrderFilledEvent{
		Flow:      events.FlowTrap,
		Symbol:    "BTC_USDT",
		CloseSide: shared.SideCloseLong,
		FillVol:   2,
	}
	rt.MarkTrapFill(fill)

	clock.EXPECT().Sleep(gomock.Any(), time.Second).Return(nil)
	client.EXPECT().
		ClosePosition(gomock.Any(), "BTC_USDT", shared.SideCloseLong, 2.0, 1).
		Return(nil)

	handleTrapOrderTimeout(context.Background(), rt, events.TrapFiredEvent{
		Flow:    events.FlowTrap,
		Symbol:  "BTC_USDT",
		OrderID: "trap-1",
	})

	closed := requireTrapPositionClosedEvent(t, rt)
	assert.Equal(t, "timeout_force_close", closed.Reason)
	assert.True(t, rt.IsFlowTerminal(events.FlowTrap))
}

func TestHandleTrapOrderTimeoutCriticalCancelFailure(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	clock := mocks.NewMockClock(ctrl)
	rt := newTrapTestRuntimeWithDeps(t, client, nil, clock, nil)

	clock.EXPECT().Sleep(gomock.Any(), time.Second).Return(nil)
	clock.EXPECT().Sleep(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	client.EXPECT().
		CancelOrder(gomock.Any(), "BTC_USDT", "trap-1").
		Return(errors.New("cancel failed")).Times(3)
	client.EXPECT().
		CancelAllOpenOrders(gomock.Any(), "BTC_USDT").
		Return(errors.New("cancel all failed")).Times(3)

	handleTrapOrderTimeout(context.Background(), rt, events.TrapFiredEvent{
		Flow:    events.FlowTrap,
		Symbol:  "BTC_USDT",
		OrderID: "trap-1",
	})

	abort := requireTrapAbortEvent(t, rt)
	assert.Contains(t, abort.Reason, "critical_trap_cancel_failed")
	assert.True(t, trapTerminal(rt))
}

func TestHandleTrapOrderTimeoutStopsOnSleepErrorOrTerminal(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	clock := mocks.NewMockClock(ctrl)
	rt := newTrapTestRuntimeWithDeps(t, client, nil, clock, nil)
	clock.EXPECT().Sleep(gomock.Any(), time.Second).Return(context.Canceled)

	handleTrapOrderTimeout(context.Background(), rt, events.TrapFiredEvent{
		Flow:    events.FlowTrap,
		Symbol:  "BTC_USDT",
		OrderID: "trap-1",
	})

	assert.Empty(t, findTrapEvents(rt, events.TopicTrapTimedOut))

	clock = mocks.NewMockClock(ctrl)
	rt = newTrapTestRuntimeWithDeps(t, client, nil, clock, nil)
	rt.MarkTrapTerminal()
	clock.EXPECT().Sleep(gomock.Any(), time.Second).Return(nil)

	handleTrapOrderTimeout(context.Background(), rt, events.TrapFiredEvent{
		Flow:    events.FlowTrap,
		Symbol:  "BTC_USDT",
		OrderID: "trap-1",
	})

	assert.Empty(t, findTrapEvents(rt, events.TopicTrapTimedOut))
}

func TestCloseTimedOutTrapPositionFallsBackToCloseAll(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	clock := mocks.NewMockClock(ctrl)
	rt := newTrapTestRuntimeWithDeps(t, client, nil, clock, nil)
	fill := events.OrderFilledEvent{
		Flow:      events.FlowTrap,
		Symbol:    "BTC_USDT",
		CloseSide: shared.SideCloseLong,
		FillVol:   2,
	}

	client.EXPECT().
		ClosePosition(gomock.Any(), "BTC_USDT", shared.SideCloseLong, 2.0, 1).
		Return(errors.New("exact failed")).Times(3)
	clock.EXPECT().Sleep(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	client.EXPECT().
		CloseAllPositions(gomock.Any(), "BTC_USDT").
		Return(nil)

	now := time.Now()
	closeTimedOutTrapPosition(context.Background(), rt, fill, time.Second, now, now.Add(time.Second))

	closed := requireTrapPositionClosedEvent(t, rt)
	assert.Equal(t, "timeout_force_close", closed.Reason)
}

func TestCloseTimedOutTrapPositionCriticalFailure(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	clock := mocks.NewMockClock(ctrl)
	rt := newTrapTestRuntimeWithDeps(t, client, nil, clock, nil)
	fill := events.OrderFilledEvent{
		Flow:      events.FlowTrap,
		Symbol:    "BTC_USDT",
		CloseSide: shared.SideCloseLong,
		FillVol:   2,
	}

	client.EXPECT().
		ClosePosition(gomock.Any(), "BTC_USDT", shared.SideCloseLong, 2.0, 1).
		Return(errors.New("exact failed")).Times(3)
	clock.EXPECT().Sleep(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	client.EXPECT().
		CloseAllPositions(gomock.Any(), "BTC_USDT").
		Return(errors.New("all failed")).Times(3)

	now := time.Now()
	closeTimedOutTrapPosition(context.Background(), rt, fill, time.Second, now, now.Add(time.Second))

	abort := requireTrapAbortEvent(t, rt)
	assert.Contains(t, abort.Reason, "critical_trap_close_failed")
	assert.True(t, trapTerminal(rt))
}

func TestFallbackCloseAfterTrailingFailureFallbacksToCloseAll(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	clock := mocks.NewMockClock(ctrl)
	rt := newTrapTestRuntimeWithDeps(t, client, nil, clock, nil)
	fill := events.OrderFilledEvent{
		Flow:      events.FlowTrap,
		Symbol:    "BTC_USDT",
		CloseSide: shared.SideCloseLong,
		FillVol:   2,
	}

	client.EXPECT().
		ClosePosition(gomock.Any(), "BTC_USDT", shared.SideCloseLong, 2.0, 1).
		Return(errors.New("exact failed")).Times(3)
	clock.EXPECT().Sleep(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	client.EXPECT().
		CloseAllPositions(gomock.Any(), "BTC_USDT").
		Return(nil)

	fallbackCloseAfterTrailingFailure(context.Background(), rt, fill)

	closed := requireTrapPositionClosedEvent(t, rt)
	assert.Equal(t, "trailing_failed_fallback", closed.Reason)
}

func TestFallbackCloseAfterTrailingFailureCriticalFailure(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	clock := mocks.NewMockClock(ctrl)
	rt := newTrapTestRuntimeWithDeps(t, client, nil, clock, nil)
	fill := events.OrderFilledEvent{
		Flow:      events.FlowTrap,
		Symbol:    "BTC_USDT",
		CloseSide: shared.SideCloseLong,
		FillVol:   2,
	}

	client.EXPECT().
		ClosePosition(gomock.Any(), "BTC_USDT", shared.SideCloseLong, 2.0, 1).
		Return(errors.New("exact failed")).Times(3)
	clock.EXPECT().Sleep(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	client.EXPECT().
		CloseAllPositions(gomock.Any(), "BTC_USDT").
		Return(errors.New("all failed")).Times(3)

	fallbackCloseAfterTrailingFailure(context.Background(), rt, fill)

	abort := requireTrapAbortEvent(t, rt)
	assert.Contains(t, abort.Reason, "critical_close_failed")
	assert.True(t, trapTerminal(rt))
}

func TestTrapRegisterAndOutcomeTerminal(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	rt := newTrapTestRuntime(t, client)

	Register(context.Background(), rt)

	assert.True(t, OutcomeTerminal(fundingdomain.TrapOutcomeClosed))
	assert.True(t, IsTrapOutcomeTerminal(fundingdomain.TrapOutcomeTimeout))
	assert.False(t, OutcomeTerminal(fundingdomain.TrapOutcome("open")))
}

func newTrapTestRuntime(t *testing.T, client exchange.Client) *cycle.Runtime {
	t.Helper()
	return newTrapTestRuntimeWithDeps(t, client, nil, nil, nil)
}

func newTrapTestRuntimeWithDeps(
	t *testing.T,
	client exchange.Client,
	notifier watcher.OrderNotifier,
	clock shared.Clock,
	depthReader interface {
		GetDepth(context.Context, string) (*shared.OrderBook, error)
	},
) *cycle.Runtime {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.SymbolConfig{
		Symbol:             "BTC_USDT",
		Leverage:           5,
		ParsedOpenType:     exchange.OpenTypeIsolated,
		ParsedPositionMode: 1,
		FundingTrap: fundingdomain.FundingTrapConfig{
			PostSettleTimeout: types.Duration(time.Second),
			SizeRatio:         0.5,
			DepthPct:          0.01,
			TakeProfitPct:     0.01,
			StopLossPct:       0.01,
		},
	}
	global := &config.Config{System: &config.SystemConfig{}}
	rt := cycle.NewRuntime(cfg, global, cycle.Deps{
		Client:        client,
		OrderNotifier: notifier,
		Clock:         clock,
		DepthStore:    depthReader,
		Log:           logger,
	})
	rt.Begin(context.Background(), "req-1", time.Now(), logger)
	rt.SetCandidate(fundingdomain.Candidate{
		Config: cycle.ToTradeConfig(cfg),
		TradeIntent: fundingdomain.TradeIntent{
			Symbol:    "BTC_USDT",
			Side:      shared.SideOpenLong,
			CloseSide: shared.SideCloseShort,
		},
		ContractSpec: fundingdomain.ContractSpec{
			PriceScale: 2,
		},
	})
	t.Cleanup(func() {
		require.NoError(t, rt.CloseBus())
	})
	return rt
}

func testTrapCandidate(side shared.Side) fundingdomain.Candidate {
	return fundingdomain.Candidate{
		Config: fundingdomain.TradeConfig{
			Symbol:              "BTC_USDT",
			MaxPriceDiffPercent: 0.2,
			MarginUSDT:          100,
			Leverage:            5,
			ParsedOpenType:      exchange.OpenTypeIsolated,
			ParsedPositionMode:  1,
			FundingReversion: fundingdomain.FundingReversionConfig{
				StopLossPct: 0.01,
			},
			FundingTrap: fundingdomain.FundingTrapConfig{
				SizeRatio:     0.5,
				DepthPct:      0.01,
				TakeProfitPct: 0.01,
				StopLossPct:   0.01,
			},
		},
		TradeIntent: fundingdomain.TradeIntent{
			Symbol:    "BTC_USDT",
			Side:      side,
			CloseSide: shared.CloseSideFor(side),
		},
		ContractSpec: fundingdomain.ContractSpec{
			PriceUnit:    0.1,
			PriceScale:   1,
			VolScale:     0,
			MinVol:       1,
			ContractSize: 1,
		},
		MarketData: fundingdomain.MarketData{
			LastPrice: 100,
			BestBid:   100,
			BestAsk:   101,
		},
		TradePlan: fundingdomain.TradePlan{
			Volume: 10,
		},
	}
}

type staticDepthReader struct {
	ob  *shared.OrderBook
	err error
}

type sequenceDepthReader struct {
	books []*shared.OrderBook
	idx   int
}

func (r *sequenceDepthReader) GetDepth(context.Context, string) (*shared.OrderBook, error) {
	if r.idx >= len(r.books) {
		return r.books[len(r.books)-1], nil
	}
	book := r.books[r.idx]
	r.idx++
	return book, nil
}

func newStaticDepthReader(ob *shared.OrderBook) *staticDepthReader {
	return &staticDepthReader{ob: ob}
}

func (r *staticDepthReader) GetDepth(context.Context, string) (*shared.OrderBook, error) {
	return r.ob, r.err
}

func testOrderBook() *shared.OrderBook {
	return &shared.OrderBook{
		Bids: []shared.OrderBookEntry{
			{Price: 99.5, Volume: 1},
			{Price: 99.0, Volume: 1},
			{Price: 98.0, Volume: 10},
			{Price: 97.5, Volume: 1},
		},
		Asks: []shared.OrderBookEntry{
			{Price: 101.5, Volume: 1},
			{Price: 102.0, Volume: 1},
			{Price: 103.0, Volume: 10},
			{Price: 103.5, Volume: 1},
		},
	}
}

func requireTrapEventTopic(t *testing.T, rt *cycle.Runtime, topic string) events.JournalEnvelope {
	t.Helper()
	log := rt.JourneyEvents()
	for i := range log {
		if log[i].Topic == topic {
			return log[i]
		}
	}
	t.Fatalf("topic %q not found", topic)
	return events.JournalEnvelope{}
}

func findTrapEvents(rt *cycle.Runtime, topic string) []events.JournalEnvelope {
	var result []events.JournalEnvelope
	evts := rt.JourneyEvents()
	for i := range evts {
		if evts[i].Topic == topic {
			result = append(result, evts[i])
		}
	}
	return result
}

func trapTerminal(rt *cycle.Runtime) bool {
	_, hasOrder, _, hasFill, terminal := rt.TrapSnapshot()
	return terminal || hasOrder && hasFill && rt.IsFlowTerminal(events.FlowTrap)
}

func requireTrapPositionClosedEvent(t *testing.T, rt *cycle.Runtime) events.PositionClosedEvent {
	t.Helper()
	env := requireTrapEventTopic(t, rt, events.TopicTrapPositionClosed)
	evt, err := cycle.Unmarshal[events.PositionClosedEvent](env.Payload)
	require.NoError(t, err)
	return evt
}

func requireTrapFiredEvent(t *testing.T, rt *cycle.Runtime) events.TrapFiredEvent {
	t.Helper()
	env := requireTrapEventTopic(t, rt, events.TopicTrapOrderPlaced)
	evt, err := cycle.Unmarshal[events.TrapFiredEvent](env.Payload)
	require.NoError(t, err)
	return evt
}

func requireTrapSkippedEvent(t *testing.T, rt *cycle.Runtime) events.TrapSkippedEvent {
	t.Helper()
	env := requireTrapEventTopic(t, rt, events.TopicTrapSkipped)
	evt, err := cycle.Unmarshal[events.TrapSkippedEvent](env.Payload)
	require.NoError(t, err)
	return evt
}

func requireTrapOrderFilledEvent(t *testing.T, rt *cycle.Runtime) events.OrderFilledEvent {
	t.Helper()
	env := requireTrapEventTopic(t, rt, events.TopicTrapOrderFilled)
	evt, err := cycle.Unmarshal[events.OrderFilledEvent](env.Payload)
	require.NoError(t, err)
	return evt
}

func requireTrapTimeoutEvent(t *testing.T, rt *cycle.Runtime, topic string) events.CycleTimeoutEvent {
	t.Helper()
	env := requireTrapEventTopic(t, rt, topic)
	evt, err := cycle.Unmarshal[events.CycleTimeoutEvent](env.Payload)
	require.NoError(t, err)
	return evt
}

func requireTrapAbortEvent(t *testing.T, rt *cycle.Runtime) events.CycleAbortEvent {
	t.Helper()
	env := requireTrapEventTopic(t, rt, events.TopicTrapAbort)
	evt, err := cycle.Unmarshal[events.CycleAbortEvent](env.Payload)
	require.NoError(t, err)
	return evt
}
