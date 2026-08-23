package reversion

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"crypto-bot/internal/bots/funding/application/strategy"
	"crypto-bot/internal/bots/funding/config"
	fundingdomain "crypto-bot/internal/bots/funding/domain"
	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/notifier"
	"crypto-bot/internal/infrastructure/store"
	infraws "crypto-bot/internal/infrastructure/ws"
	"crypto-bot/internal/testutil/mocks"
	ordermanager "crypto-bot/internal/trading/ordermanager"
	"crypto-bot/pkg/eventbus"

	cache "github.com/patrickmn/go-cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func reversionTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestStrategyMetadataAndCleanup(t *testing.T) {
	t.Parallel()

	s := NewStrategy(
		&app.Engine{},
		&config.Config{},
		nil,
		nil,
		reversionTestLogger(),
	)

	assert.Equal(t, FlowIDFundingReversion, s.Flow())
	assert.True(t, s.Enabled(config.SymbolConfig{
		FundingReversion: fundingdomain.FundingReversionConfig{Enabled: true},
	}))
	assert.False(t, s.Enabled(config.SymbolConfig{}))
}

func TestStatelessRunnerAbortLifecycle(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	clock := mocks.NewMockClock(ctrl)
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	clock.EXPECT().Now().Return(now).AnyTimes()

	ws := mocks.NewMockSubscriber(ctrl)
	ws.EXPECT().UnsubscribeTicker(gomock.Any(), "BTC_USDT").Return(errors.New("unsubscribe failed")).AnyTimes()

	n := mocks.NewMockNotifier(ctrl)
	n.EXPECT().Send(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	bus := eventbus.New(reversionTestLogger())
	t.Cleanup(func() { _ = bus.Close() })

	runner := &StatelessRunner{
		deps: strategy.Deps{
			WsSub:    infraws.NewExchangeManagerAdapter(nil),
			Clock:    clock,
			Notifier: n,
		},
		globalCfg: &config.Config{Symbols: []config.SymbolConfig{{Symbol: "BTC_USDT"}}},
		bus:       bus,
		log:       reversionTestLogger(),
		cache:     cache.New(5*time.Minute, 10*time.Minute),
	}

	ctx := context.Background()
	runner.abortAfter(ctx, BaseReversionEvent{ReqID: "test-req-abort", Symbol: "BTC_USDT", Exchange: "mexc"}, "BTC_USDT", "not profitable")

	assert.Equal(t, 1, countTopic(bus, TopicReversionAbort))
}

func TestStatelessRunnerGetSymbolAndWaitUntilBranches(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	clock := mocks.NewMockClock(ctrl)
	target := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	clock.EXPECT().Until(target).Return(10 * time.Millisecond)
	clock.EXPECT().Sleep(gomock.Any(), 10*time.Millisecond).Return(nil)
	clock.EXPECT().Until(target).Return(time.Duration(0))

	runner := &StatelessRunner{
		deps:      strategy.Deps{Clock: clock, Notifier: testCaptureNotifier()},
		globalCfg: &config.Config{Symbols: []config.SymbolConfig{{Symbol: "BTC_USDT"}}},
		log:       reversionTestLogger(),
	}

	assert.True(t, runner.WaitUntil(context.Background(), "BTC_USDT", target))
	assert.True(t, runner.WaitUntil(context.Background(), "BTC_USDT", target))
}

func TestPublishEventWithoutBusOrNotification(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	clock := mocks.NewMockClock(ctrl)
	clock.EXPECT().Now().Return(time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)).AnyTimes()
	n := mocks.NewMockNotifier(ctrl)
	n.EXPECT().Send(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	bus := eventbus.New(reversionTestLogger())
	t.Cleanup(func() { _ = bus.Close() })

	runner := &StatelessRunner{
		deps: strategy.Deps{
			Clock:    clock,
			Notifier: n,
		},
		bus: bus,
		log: reversionTestLogger(),
	}

	require.NoError(t, runner.publishEvent(context.Background(), TopicReversionArmed, ArmedEvent{
		Symbol: "BTC_USDT",
	}))

	require.NoError(t, runner.publishEvent(context.Background(), TopicReversionAbort, AbortEvent{
		Symbol: "BTC_USDT",
		Reason: "boom",
	}))
}

type captureNotifier struct {
	events chan notifier.Event
}

func (n captureNotifier) Send(ctx context.Context, evt notifier.Event) error {
	select {
	case n.events <- evt:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (captureNotifier) Start(context.Context) error { return nil }
func (captureNotifier) Stop(context.Context) error  { return nil }

func testCaptureNotifier() captureNotifier {
	return captureNotifier{events: make(chan notifier.Event, 100)}
}

func TestPublishEventNotificationIncludesExchange(t *testing.T) {
	t.Parallel()

	bus := eventbus.New(reversionTestLogger())
	t.Cleanup(func() { _ = bus.Close() })

	notify := captureNotifier{events: make(chan notifier.Event, 1)}
	runner := &StatelessRunner{
		deps: strategy.Deps{
			Clock:    newReversionManualClock(time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)),
			Notifier: notify,
		},
		bus: bus,
		log: reversionTestLogger(),
	}

	require.NoError(t, runner.publishEvent(context.Background(), TopicReversionArmed, ArmedEvent{
		Symbol:   "BTC_USDT",
		Exchange: "bybit",
		Candidate: fundingdomain.Candidate{
			TradeIntent: fundingdomain.TradeIntent{
				Symbol:      "BTC_USDT",
				Side:        shared.SideOpenLong,
				FundingRate: 0.001,
			},
			Vol24USDT: 10_000_000,
			Volume:    1,
		},
	}))

	select {
	case evt := <-notify.events:
		assert.Contains(t, evt.Message, "[bybit]")
		assert.Contains(t, evt.Message, "BTC_USDT")
	case <-time.After(time.Second):
		require.Fail(t, "notification was not sent")
	}
}

func TestEventTraceSeqAndPreviousTopic(t *testing.T) {
	t.Parallel()

	bus := eventbus.New(reversionTestLogger())
	t.Cleanup(func() { _ = bus.Close() })

	runner := &StatelessRunner{
		deps: strategy.Deps{
			Clock:    newReversionManualClock(time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)),
			Notifier: testCaptureNotifier(),
		},
		bus: bus,
		log: reversionTestLogger(),
	}

	reqID := "trace-req-seq"
	candidateBase := BaseReversionEvent{ReqID: reqID, Symbol: "BTC_USDT", Seq: 1, Topic: TopicReversionCandidate}
	require.NoError(t, runner.publishEvent(context.Background(), TopicReversionCandidate, CandidateFoundEvent{
		BaseReversionEvent: candidateBase,
	}))
	armBase := nextReversionBase(candidateBase, "BTC_USDT", runner.deps.Clock.Now())
	require.NoError(t, runner.publishEvent(context.Background(), TopicReversionArmMarketReady, ArmMarketReadyEvent{
		BaseReversionEvent: armBase,
	}))
	armBase.Topic = TopicReversionArmMarketReady
	runner.abortAfter(context.Background(), armBase, "BTC_USDT", "trace_abort")

	events := timelineBaseEvents(t, bus)
	require.Len(t, events, 3)
	assert.Equal(t, int64(1), events[0].Seq)
	assert.Equal(t, TopicReversionCandidate, events[0].Topic)
	assert.Empty(t, events[0].PreviousTopic)
	assert.NotEmpty(t, events[0].EventID)

	assert.Equal(t, int64(2), events[1].Seq)
	assert.Equal(t, TopicReversionArmMarketReady, events[1].Topic)
	assert.Equal(t, TopicReversionCandidate, events[1].PreviousTopic)
	assert.NotEmpty(t, events[1].EventID)

	assert.Equal(t, int64(3), events[2].Seq)
	assert.Equal(t, TopicReversionAbort, events[2].Topic)
	assert.Equal(t, TopicReversionArmMarketReady, events[2].PreviousTopic)
	assert.NotEmpty(t, events[2].EventID)
}

func TestFirePlanCheckedDispatchesOrderManagerIntent(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	clock := newReversionManualClock(time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC))
	bus := eventbus.New(reversionTestLogger())
	t.Cleanup(func() { _ = bus.Close() })

	mockNotifier := mocks.NewMockNotifier(ctrl)
	mockNotifier.EXPECT().Send(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	runner := &StatelessRunner{
		deps: strategy.Deps{
			Clock:    clock,
			Notifier: mockNotifier,
		},
		notifier: mockNotifier,
		globalCfg: &config.Config{Symbols: []config.SymbolConfig{{
			Symbol: "BTC_USDT",
			FundingReversion: fundingdomain.FundingReversionConfig{
				PostSettleTimeout: 10_000_000_000,
			},
		}}},
		bus:   bus,
		log:   reversionTestLogger(),
		cache: cache.New(5*time.Minute, 10*time.Minute),
	}

	candidate := reversionTestCandidate()
	planEvt := FirePlanCheckedEvent{
		ReqID:          "trace-req-om",
		Symbol:         "BTC_USDT",
		Exchange:       "mexc",
		SettleTime:     clock.Now().Add(time.Second),
		Candidate:      candidate,
		Passed:         true,
		IOCPrice:       60000,
		AdjustedVolume: 10,
	}

	require.NoError(t, runner.handleFirePlanChecked(context.Background(), planEvt))
	assert.Equal(t, []string{ordermanager.TopicOrderIntent}, timelineTopics(bus))
}

func TestStatelessRunnerHandleWaitBranches(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	clock := mocks.NewMockClock(ctrl)
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	clock.EXPECT().Now().Return(now).AnyTimes()
	clock.EXPECT().Until(now.Add(-5 * time.Second)).Return(10 * time.Millisecond)
	clock.EXPECT().Sleep(gomock.Any(), 10*time.Millisecond).Return(context.Canceled)

	runner := &StatelessRunner{
		deps: strategy.Deps{Clock: clock, Notifier: testCaptureNotifier()},
		bus:  eventbus.New(reversionTestLogger()),
		log:  reversionTestLogger(),
	}
	t.Cleanup(func() { _ = runner.bus.Close() })

	require.NoError(t, runner.handleWait(context.Background(), ArmedEvent{
		Symbol: "BTC_USDT",
	}))
	require.ErrorIs(t, runner.handleWait(context.Background(), ArmedEvent{
		Symbol: "BTC_USDT", SettleTime: now,
	}), context.Canceled)
}

func TestStatelessRunnerHandleArmErrorPaths(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	clock := mocks.NewMockClock(ctrl)
	clock.EXPECT().Now().Return(time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)).AnyTimes()

	priceStore := mocks.NewMockPriceReader(ctrl)
	priceStore.EXPECT().SubscribePrice(gomock.Any(), "BTC_USDT").Return(nil).AnyTimes()
	priceStore.EXPECT().GetPrice(gomock.Any(), "BTC_USDT", gomock.Any()).Return(&store.PriceData{
		BestBid: 59990,
		BestAsk: 60000,
	}, nil).AnyTimes()

	runner := &StatelessRunner{
		deps: strategy.Deps{
			WsSub:      infraws.NewExchangeManagerAdapter(nil),
			Clock:      clock,
			PriceStore: priceStore,
			Notifier:   testCaptureNotifier(),
		},
		bus: eventbus.New(reversionTestLogger()),
		log: reversionTestLogger(),
	}
	t.Cleanup(func() { _ = runner.bus.Close() })

	err := runner.handleArm(context.Background(), CandidateFoundEvent{
		Candidate: fundingdomain.Candidate{TradeIntent: fundingdomain.TradeIntent{Symbol: "BTC_USDT"}},
	})
	require.NoError(t, err)
}

func TestStatelessRunnerHandleRecheckErrorPaths(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	clock := mocks.NewMockClock(ctrl)
	clock.EXPECT().Now().Return(time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)).AnyTimes()

	bus := eventbus.New(reversionTestLogger())
	t.Cleanup(func() { _ = bus.Close() })

	runner := &StatelessRunner{
		deps:      strategy.Deps{Clock: clock, Notifier: testCaptureNotifier()},
		globalCfg: &config.Config{},
		bus:       bus,
		log:       reversionTestLogger(),
	}

	client := mocks.NewMockClient(ctrl)
	runner.deps.Client = client
	client.EXPECT().GetFundingRates(gomock.Any(), []string{"BTC_USDT"}).Return(nil, errors.New("missing funding"))
	err := runner.handleRecheck(context.Background(), WaitCompleteEvent{
		Symbol: "BTC_USDT",
		Candidate: fundingdomain.Candidate{
			Config: fundingdomain.TradeConfig{
				MinFundingRate: 0.001,
			},
			TradeIntent: fundingdomain.TradeIntent{Symbol: "BTC_USDT", FundingRate: 0.01},
		},
	})
	require.ErrorContains(t, err, "no funding data for recheck")

	client.EXPECT().GetFundingRates(gomock.Any(), []string{"BTC_USDT"}).Return([]exchange.FundingRateResult{
		{Symbol: "BTC_USDT", Rate: -0.01},
	}, nil)
	err = runner.handleRecheck(context.Background(), WaitCompleteEvent{
		Symbol: "BTC_USDT",
		Candidate: fundingdomain.Candidate{
			Config: fundingdomain.TradeConfig{
				MinFundingRate: 0.001,
			},
			TradeIntent: fundingdomain.TradeIntent{Symbol: "BTC_USDT", FundingRate: 0.01},
		},
	})
	require.ErrorContains(t, err, "FR sign flip")

	client.EXPECT().GetFundingRates(gomock.Any(), []string{"BTC_USDT"}).Return([]exchange.FundingRateResult{
		{Symbol: "BTC_USDT", Rate: 0.0001},
	}, nil)
	err = runner.handleRecheck(context.Background(), WaitCompleteEvent{
		Symbol: "BTC_USDT",
		Candidate: fundingdomain.Candidate{
			Config: fundingdomain.TradeConfig{
				MinFundingRate: 0.001,
			},
			TradeIntent: fundingdomain.TradeIntent{Symbol: "BTC_USDT", FundingRate: 0.01},
		},
	})
	require.ErrorContains(t, err, "FR below threshold")
}

func TestStatelessRunnerHandleFireIOCEarlyErrors(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	clock := mocks.NewMockClock(ctrl)
	clock.EXPECT().Now().Return(time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)).AnyTimes()

	runner := &StatelessRunner{
		deps:      strategy.Deps{Clock: clock, Notifier: testCaptureNotifier()},
		globalCfg: &config.Config{},
		bus:       eventbus.New(reversionTestLogger()),
		log:       reversionTestLogger(),
	}
	t.Cleanup(func() { _ = runner.bus.Close() })

	err := runner.handleFireIOC(context.Background(), ConfirmedEvent{
		Symbol: "BTC_USDT",
	})
	require.ErrorContains(t, err, "settle time not found")

	clock.EXPECT().LatencyMs().Return(int64(200))
	err = runner.handleFireIOC(context.Background(), ConfirmedEvent{
		Symbol: "BTC_USDT", SettleTime: time.Now().Add(time.Second),
		Candidate: fundingdomain.Candidate{
			Config: fundingdomain.TradeConfig{
				FundingReversion: fundingdomain.FundingReversionConfig{
					MaxLatency: 50_000_000,
				},
			},
		},
	})
	require.ErrorContains(t, err, "latency too high")
}

type reversionManualClock struct {
	now        time.Time
	latencyMs  int64
	offsetMs   int64
	syncCalled bool
}

func newReversionManualClock(now time.Time) *reversionManualClock {
	return &reversionManualClock{now: now, latencyMs: 20}
}

func (c *reversionManualClock) SyncNow(ctx context.Context) {
	c.syncCalled = true
}

func (c *reversionManualClock) Now() time.Time {
	return c.now
}

func (c *reversionManualClock) Until(target time.Time) time.Duration {
	return target.Sub(c.now)
}

func (c *reversionManualClock) GetServerTime() int64 {
	return c.now.UnixMilli()
}

func (c *reversionManualClock) LatencyMs() int64 {
	return c.latencyMs
}

func (c *reversionManualClock) Offset() int64 {
	return c.offsetMs
}

func (c *reversionManualClock) IsHealthy() bool {
	return true
}

func (c *reversionManualClock) MsUntilTarget(targetServerTimeMs int64) int64 {
	return targetServerTimeMs - c.now.UnixMilli()
}

func (c *reversionManualClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.now = c.now.Add(d)
	return nil
}

func reversionTestCandidate() fundingdomain.Candidate {
	return fundingdomain.Candidate{
		Config: fundingdomain.TradeConfig{
			Symbol:              "BTC_USDT",
			MaxPriceDiffPercent: 0.01,
			MarginUSDT:          100,
			Leverage:            0,
			MinFundingRate:      0.001,
			FundingReversion: fundingdomain.FundingReversionConfig{
				Enabled:           true,
				PostSettleTimeout: 10_000_000_000, // 10s
				BufferTime:        150_000_000,    // 150ms
				MaxLatency:        50_000_000,     // 50ms
			},
		},
		Symbol:       "BTC_USDT",
		Side:         shared.SideOpenLong,
		CloseSide:    shared.SideCloseLong,
		PriceUnit:    0.01,
		VolUnit:      1,
		MinVol:       1,
		PriceScale:   2,
		VolScale:     4,
		ContractSize: 0.001,
		LastPrice:    60000,
		BestBid:      59990,
		BestAsk:      60000,
		Volume24:     1000,
		Vol24USDT:    60_000_000,
		Volume:       1,
	}
}

func timelineTopics(bus *eventbus.Bus) []string {
	timeline := bus.Timeline()
	topics := make([]string, 0, len(timeline))
	for _, entry := range timeline {
		topics = append(topics, entry.Topic)
	}
	return topics
}

func timelineBaseEvents(t *testing.T, bus *eventbus.Bus) []BaseReversionEvent {
	t.Helper()
	timeline := bus.Timeline()
	events := make([]BaseReversionEvent, 0, len(timeline))
	for _, entry := range timeline {
		var base BaseReversionEvent
		require.NoError(t, json.Unmarshal(entry.Payload, &base))
		events = append(events, base)
	}
	return events
}

func countTopic(bus *eventbus.Bus, topic string) int {
	count := 0
	for _, entry := range bus.Timeline() {
		if entry.Topic == topic {
			count++
		}
	}
	return count
}

func TestStatelessRunnerSyncNowInvocation(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	clock := newReversionManualClock(time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC))
	bus := eventbus.New(reversionTestLogger())
	t.Cleanup(func() { _ = bus.Close() })

	client := mocks.NewMockClient(ctrl)
	client.EXPECT().GetFundingRates(gomock.Any(), []string{"BTC_USDT"}).Return([]exchange.FundingRateResult{
		{Symbol: "BTC_USDT", Rate: 0.01},
	}, nil).AnyTimes()

	ws := mocks.NewMockSubscriber(ctrl)
	ws.EXPECT().SubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil).AnyTimes()

	priceStore := mocks.NewMockPriceReader(ctrl)
	priceStore.EXPECT().GetPrice(gomock.Any(), "BTC_USDT", gomock.Any()).Return(&store.PriceData{
		LastPrice: 100,
		BestBid:   99,
		BestAsk:   101,
	}, nil).AnyTimes()
	priceStore.EXPECT().SubscribePrice(gomock.Any(), "BTC_USDT").Return(nil).AnyTimes()

	runner := &StatelessRunner{
		deps: strategy.Deps{
			Clock:      clock,
			Client:     client,
			WsSub:      infraws.NewExchangeManagerAdapter(nil),
			PriceStore: priceStore,
			Notifier:   testCaptureNotifier(),
		},
		globalCfg: &config.Config{
			Symbols: []config.SymbolConfig{
				{
					Symbol:         "BTC_USDT",
					Exchange:       "mexc",
					MinFundingRate: 0.001,
				},
			},
		},
		bus: bus,
		log: reversionTestLogger(),
	}

	// 1. Verify handleArm triggers SyncNow
	assert.False(t, clock.syncCalled)
	err := runner.handleArm(context.Background(), CandidateFoundEvent{
		Symbol: "BTC_USDT",
		Candidate: fundingdomain.Candidate{
			TradeIntent: fundingdomain.TradeIntent{Symbol: "BTC_USDT", FundingRate: 0.01},
		},
	})
	require.NoError(t, err)
	assert.True(t, clock.syncCalled)

	// Reset tracker
	clock.syncCalled = false

	// 2. Verify handleRecheck triggers SyncNow
	assert.False(t, clock.syncCalled)
	err = runner.handleRecheck(context.Background(), WaitCompleteEvent{
		Symbol: "BTC_USDT",
		Candidate: fundingdomain.Candidate{
			Config: fundingdomain.TradeConfig{
				MinFundingRate: 0.001,
			},
			TradeIntent: fundingdomain.TradeIntent{Symbol: "BTC_USDT", FundingRate: 0.01},
		},
	})
	require.NoError(t, err)
	assert.True(t, clock.syncCalled)
}

func TestFormatReversionNotification_FormatMatch(t *testing.T) {
	t.Parallel()

	armedEvt := ArmedEvent{
		Symbol:     "MOVE_USDT",
		Exchange:   "mexc_futures",
		OrderID:    "846414742811231839",
		ExternalID: "23082026145000MOVEMEXCFUTURES",
		ReqID:      "23082026145000MOVEMEXCFUTURESREVERSION214:50",
		Candidate: fundingdomain.Candidate{
			TradeIntent: fundingdomain.TradeIntent{
				Symbol:      "MOVE_USDT",
				Side:        shared.SideOpenShort,
				FundingRate: -0.004,
			},
			LastPrice: 0.00774,
			Vol24USDT: 2_130_000,
			Volume:    24.80,
			Config: fundingdomain.TradeConfig{
				MarginUSDT: 5.0,
				Leverage:   5,
			},
		},
	}

	formatted := formatReversionNotification(TopicReversionArmed, armedEvt)
	expected := `🟡 [FUNDING_REVERSION] [mexc_futures] [CANDIDATE]
• Symbol: MOVE_USDT | Side: Short
• Margin: 5.00 USDT | Leverage: 5x
• Price: 0.007740 | Size: 0.19 USDT
• FR: -0.4% | Vol24h: $2.13m
• Order ID: 846414742811231839
• Client ID: 23082026145000MOVEMEXCFUTURES
• Req ID: 23082026145000MOVEMEXCFUTURESREVERSION214:50`

	assert.Equal(t, expected, formatted)

	abortEvt := AbortEvent{
		Symbol:   "MOVE_USDT",
		Exchange: "mexc_futures",
		ReqID:    "req_abort_123",
		Reason:   "FR below threshold",
	}
	formattedAbort := formatReversionNotification(TopicReversionAbort, abortEvt)
	expectedAbort := `🔴 [FUNDING_REVERSION] [mexc_futures] [ABORTED]
• Symbol: MOVE_USDT
• Reason: FR below threshold
• Req ID: req_abort_123`
	assert.Equal(t, expectedAbort, formattedAbort)
}
