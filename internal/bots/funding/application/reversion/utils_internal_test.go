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
	"crypto-bot/internal/testutil/mocks"
	"crypto-bot/pkg/eventbus"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
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
		reversionTestLogger(),
	)

	assert.Equal(t, FlowReversion, s.Flow())
	assert.True(t, s.Enabled(config.SymbolConfig{
		FundingReversion: fundingdomain.FundingReversionConfig{Enabled: true},
	}))
	assert.False(t, s.Enabled(config.SymbolConfig{}))
}

func TestStatelessRunnerRetryWithBackoff(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	clock := mocks.NewMockClock(ctrl)
	clock.EXPECT().Sleep(gomock.Any(), gomock.Any()).Return(nil).Times(2)

	runner := &StatelessRunner{
		deps: strategy.Deps{Clock: clock},
		log:  reversionTestLogger(),
	}

	calls := 0
	attempts, err := runner.RetryWithBackoffOpts(context.Background(), 3, time.Millisecond, time.Millisecond, func() error {
		calls++
		if calls < 3 {
			return errors.New("retry")
		}
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 3, attempts)
	assert.Equal(t, 3, calls)
}

func TestStatelessRunnerRetryWithBackoffContextStopsSleep(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	clock := mocks.NewMockClock(ctrl)
	clock.EXPECT().Sleep(gomock.Any(), gomock.Any()).Return(context.Canceled)

	runner := &StatelessRunner{
		deps: strategy.Deps{Clock: clock},
		log:  reversionTestLogger(),
	}

	attempts, err := runner.RetryWithBackoff(context.Background(), 2, func() error {
		return errors.New("retry")
	})
	require.ErrorContains(t, err, "retry")
	assert.Equal(t, 1, attempts)
}

func TestStatelessRunnerAbortAndCleanupPublishLifecycle(t *testing.T) {
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
			WsSub:    ws,
			Clock:    clock,
			Notifier: n,
		},
		globalCfg: &config.Config{Symbols: []config.SymbolConfig{{Symbol: "BTC_USDT"}}},
		bus:       bus,
		log:       reversionTestLogger(),
	}

	ctx := context.Background()
	runner.abort(ctx, "BTC_USDT", "test-req-abort", "mexc", "not profitable")

	final := runner.calculateFinalPnL(PositionClosedEvent{
		BaseReversionEvent: BaseReversionEvent{Symbol: "BTC_USDT"},
		EntryPrice:         100,
		ClosePrice:         101,
		CloseVol:           2,
		GrossProfit:        2,
		NetProfit:          1.8,
		PnLPct:             1.0,
		VolumeUSDT:         202,
		Fee:                -0.1,
		HoldFee:            -0.1,
		HoldDurationMs:     250,
	})
	assert.Equal(t, "BTC_USDT", final.Symbol)
	assert.Equal(t, 1.8, final.NetPnL)
	assert.Equal(t, 1.0, final.PnLPct)
	assert.Equal(t, 202.0, final.VolumeUSDT)

	payload, err := json.Marshal(PositionClosedEvent{
		BaseReversionEvent: BaseReversionEvent{Symbol: "BTC_USDT"},
		CloseVol:           2,
		EntryPrice:         100,
		ClosePrice:         101,
	})
	require.NoError(t, err)
	msg := message.NewMessage(watermill.NewUUID(), payload)
	require.NoError(t, runner.handleCleanup(ctx, msg))
}

func TestStatelessRunnerHandlePositionUpdateFallbacks(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	clock := mocks.NewMockClock(ctrl)
	clock.EXPECT().Now().Return(time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)).AnyTimes()

	priceStore := mocks.NewMockPriceReader(ctrl)
	priceStore.EXPECT().GetPrice(gomock.Any(), "BTC_USDT", gomock.Any()).Return(&store.PriceData{
		LastPrice: 100,
		BestBid:   99,
		BestAsk:   101,
	}, nil).AnyTimes()

	bus := eventbus.New(reversionTestLogger())
	t.Cleanup(func() { _ = bus.Close() })

	runner := &StatelessRunner{
		deps: strategy.Deps{
			Clock:      clock,
			PriceStore: priceStore,
		},
		bus: bus,
		log: reversionTestLogger(),
	}

	runner.handlePositionUpdate(context.Background(), exchange.PersonalPositionUpdate{
		Symbol:       "BTC_USDT",
		PositionType: 2,
		HoldVol:      1,
	}, BaseReversionEvent{ReqID: "test-req-1", Symbol: "BTC_USDT", Topic: TopicReversionPositionWatchReady})
	runner.handlePositionUpdate(context.Background(), exchange.PersonalPositionUpdate{
		Symbol:          "BTC_USDT",
		HoldVol:         0,
		CloseVol:        1,
		OpenAvgPrice:    100,
		CloseAvgPrice:   101,
		CloseProfitLoss: 1,
		Fee:             -0.1,
		HoldFee:         -0.01,
	}, BaseReversionEvent{ReqID: "test-req-2", Symbol: "BTC_USDT", Topic: TopicReversionPositionWatchReady})
}

type mockClosedPnLClient struct {
	exchange.Client
	closedInfo *exchange.ClosedPnLInfo
	closedErr  error
}

func (m *mockClosedPnLClient) GetRecentClosedPnL(ctx context.Context, symbol, extOrderID string, startTime time.Time) (*exchange.ClosedPnLInfo, error) {
	return m.closedInfo, m.closedErr
}

func TestStatelessRunnerHandlePositionUpdate_ClosedPnLEnrichment(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	clock := mocks.NewMockClock(ctrl)
	clock.EXPECT().Now().Return(time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)).AnyTimes()

	priceStore := mocks.NewMockPriceReader(ctrl)
	priceStore.EXPECT().GetPrice(gomock.Any(), "BTC_USDT", gomock.Any()).Return(&store.PriceData{
		LastPrice: 100,
		BestBid:   99,
		BestAsk:   101,
	}, nil).AnyTimes()

	mockCli := mocks.NewMockClient(ctrl)
	mockNotifier := mocks.NewMockNotifier(ctrl)
	mockNotifier.EXPECT().Send(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	t.Run("Enrichment success", func(t *testing.T) {
		t.Parallel()
		client := &mockClosedPnLClient{
			Client: mockCli,
			closedInfo: &exchange.ClosedPnLInfo{
				Symbol:     "BTC_USDT",
				EntryPrice: 100,
				ExitPrice:  105,
				ClosedSize: 2.0,
				GrossPnL:   10.0,
				Fee:        1.0,
				DurationMs: 60000,
			},
		}

		bus := eventbus.New(reversionTestLogger())
		t.Cleanup(func() { _ = bus.Close() })

		runner := &StatelessRunner{
			deps: strategy.Deps{
				Clock:      clock,
				PriceStore: priceStore,
				Client:     client,
				Notifier:   mockNotifier,
			},
			bus: bus,
			log: reversionTestLogger(),
		}

		var closedEvt PositionClosedEvent
		ch, err := bus.Subscribe(context.Background(), TopicReversionPositionClosed)
		require.NoError(t, err)

		runner.handlePositionUpdate(context.Background(), exchange.PersonalPositionUpdate{
			Symbol:          "BTC_USDT",
			HoldVol:         0,
			CloseVol:        1,
			OpenAvgPrice:    100,
			CloseAvgPrice:   101,
			CloseProfitLoss: 1,
			Fee:             -0.1,
			HoldFee:         -0.01,
		}, BaseReversionEvent{ReqID: "test-req-enrich", Symbol: "BTC_USDT", Topic: TopicReversionPositionWatchReady})

		select {
		case msg := <-ch:
			require.NoError(t, json.Unmarshal(msg.Payload, &closedEvt))
			msg.Ack()
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for closed event")
		}

		assert.Equal(t, 100.0, closedEvt.EntryPrice)
		assert.Equal(t, 105.0, closedEvt.ClosePrice)
		assert.Equal(t, 2.0, closedEvt.CloseVol)
		assert.Equal(t, 10.0, closedEvt.GrossProfit)
		assert.Equal(t, 1.0, closedEvt.Fee)
		assert.Equal(t, 9.0, closedEvt.NetProfit)
		assert.Equal(t, 5.0, closedEvt.PnLPct) // ((105 - 100) / 100) * 100
		assert.Equal(t, int64(60000), closedEvt.HoldDurationMs)
	})

	t.Run("Enrichment failure fallback", func(t *testing.T) {
		t.Parallel()
		client := &mockClosedPnLClient{
			Client:    mockCli,
			closedErr: errors.New("api error"),
		}

		bus := eventbus.New(reversionTestLogger())
		t.Cleanup(func() { _ = bus.Close() })

		runner := &StatelessRunner{
			deps: strategy.Deps{
				Clock:      clock,
				PriceStore: priceStore,
				Client:     client,
				Notifier:   mockNotifier,
			},
			bus: bus,
			log: reversionTestLogger(),
		}

		var closedEvt PositionClosedEvent
		ch, err := bus.Subscribe(context.Background(), TopicReversionPositionClosed)
		require.NoError(t, err)

		runner.handlePositionUpdate(context.Background(), exchange.PersonalPositionUpdate{
			Symbol:          "BTC_USDT",
			HoldVol:         0,
			CloseVol:        1,
			OpenAvgPrice:    100,
			CloseAvgPrice:   101,
			CloseProfitLoss: 1,
			Fee:             -0.1,
			HoldFee:         -0.01,
		}, BaseReversionEvent{ReqID: "test-req-fallback", Symbol: "BTC_USDT", Topic: TopicReversionPositionWatchReady})

		select {
		case msg := <-ch:
			require.NoError(t, json.Unmarshal(msg.Payload, &closedEvt))
			msg.Ack()
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for closed event")
		}

		// Check that it falls back gracefully to the original PersonalPositionUpdate/PriceStore metrics
		assert.Equal(t, 100.0, closedEvt.EntryPrice)
		assert.Equal(t, 101.0, closedEvt.ClosePrice)
		assert.Equal(t, 1.0, closedEvt.CloseVol)
		assert.Equal(t, 1.0, closedEvt.GrossProfit)
		assert.Equal(t, -0.1, closedEvt.Fee)
	})
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
		deps:      strategy.Deps{Clock: clock},
		globalCfg: &config.Config{Symbols: []config.SymbolConfig{{Symbol: "BTC_USDT"}}},
		log:       reversionTestLogger(),
	}

	symCfg, ok := runner.getSymbolConfig("BTC_USDT")
	require.True(t, ok)
	assert.Equal(t, "BTC_USDT", symCfg.Symbol)
	_, ok = runner.getSymbolConfig("ETH_USDT")
	assert.False(t, ok)

	assert.True(t, runner.WaitUntil(context.Background(), "BTC_USDT", target))
	assert.True(t, runner.WaitUntil(context.Background(), "BTC_USDT", target))
}

func TestPublishEventWithoutBusOrNotification(t *testing.T) {
	t.Parallel()

	runner := &StatelessRunner{log: reversionTestLogger()}
	require.NoError(t, runner.publishEvent(context.Background(), TopicReversionCompleted, ReversionCompletedEvent{
		BaseReversionEvent: BaseReversionEvent{Symbol: "BTC_USDT"},
	}))

	ctrl := gomock.NewController(t)
	clock := mocks.NewMockClock(ctrl)
	clock.EXPECT().Now().Return(time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)).AnyTimes()
	n := mocks.NewMockNotifier(ctrl)
	n.EXPECT().Send(gomock.Any(), gomock.AssignableToTypeOf(notifier.Event{})).Return(nil).AnyTimes()

	bus := eventbus.New(reversionTestLogger())
	t.Cleanup(func() { _ = bus.Close() })
	runner.bus = bus
	runner.deps.Clock = clock
	runner.deps.Notifier = n

	require.NoError(t, runner.publishEvent(context.Background(), TopicReversionError, ErrorEvent{
		BaseReversionEvent: BaseReversionEvent{Symbol: "BTC_USDT"},
		Error:              "boom",
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
		BaseReversionEvent: BaseReversionEvent{
			Symbol:     "BTC_USDT",
			Exchange:   "bybit",
			SendNotify: true,
		},
		Volume: 1,
	}))

	select {
	case evt := <-notify.events:
		assert.Equal(t, "bybit", evt.Exchange)
		assert.Equal(t, "BTC_USDT", evt.Symbol)
	case <-time.After(time.Second):
		require.Fail(t, "notification was not sent")
	}
}

func TestEventTraceSeqAndPreviousTopic(t *testing.T) {
	t.Parallel()

	bus := eventbus.New(reversionTestLogger())
	t.Cleanup(func() { _ = bus.Close() })

	runner := &StatelessRunner{
		deps: strategy.Deps{Clock: newReversionManualClock(time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC))},
		bus:  bus,
		log:  reversionTestLogger(),
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

func TestPositionWatcherArmedBeforeIOCSubmit(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	clock := newReversionManualClock(time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC))
	bus := eventbus.New(reversionTestLogger())
	t.Cleanup(func() { _ = bus.Close() })

	var operations []string
	watcher := mocks.NewMockOrderNotifier(ctrl)
	watcher.EXPECT().OnPositionUpdate(gomock.Any(), "BTC_USDT", 20*time.Second, gomock.Any()).Do(
		func(context.Context, string, time.Duration, func(exchange.PersonalPositionUpdate)) {
			operations = append(operations, "watcher_armed")
		},
	)
	client := mocks.NewMockClient(ctrl)
	client.EXPECT().CreateOrder(gomock.Any(), gomock.Any()).DoAndReturn(
		func(context.Context, exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
			operations = append(operations, "ioc_submitted")
			return exchange.CreateOrderResult{OrderID: "ord-seq", TPSLSubmitted: false}, nil
		},
	)

	runner := &StatelessRunner{
		deps: strategy.Deps{
			Client:        client,
			OrderNotifier: watcher,
			Clock:         clock,
		},
		globalCfg: &config.Config{Symbols: []config.SymbolConfig{{
			Symbol: "BTC_USDT",
			FundingReversion: fundingdomain.FundingReversionConfig{
				PostSettleTimeout: 10_000_000_000,
			},
		}}},
		bus: bus,
		log: reversionTestLogger(),
	}

	candidate := reversionTestCandidate()
	fireEvt := FireWindowReachedEvent{
		BaseReversionEvent: BaseReversionEvent{ReqID: "trace-req-watcher", Symbol: "BTC_USDT", SettleTime: clock.Now().Add(time.Second)},
		Candidate:          candidate,
		FireTimestamp:      clock.Now(),
	}
	require.NoError(t, runner.handleFireWindowReached(context.Background(), fireEvt))
	require.Equal(t, []string{"watcher_armed"}, operations)

	watchReady := timelineEvent[PositionWatchReadyEvent](t, bus, TopicReversionPositionWatchReady)
	require.NoError(t, runner.handlePositionWatchReady(context.Background(), watchReady))
	assert.Equal(t, []string{"watcher_armed", "ioc_submitted"}, operations)
	assert.Equal(t, []string{TopicReversionPositionWatchReady, TopicReversionIOCSubmitted}, timelineTopics(bus))
	for _, topic := range timelineTopics(bus) {
		assert.NotContains(t, topic, "position:")
	}
}

func TestWatcherFillBeforeOutcomeDoesNotDuplicateOrderFilled(t *testing.T) {
	t.Parallel()

	clock := newReversionManualClock(time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC))
	bus := eventbus.New(reversionTestLogger())
	t.Cleanup(func() { _ = bus.Close() })

	runner := &StatelessRunner{
		deps: strategy.Deps{Clock: clock},
		globalCfg: &config.Config{Symbols: []config.SymbolConfig{{
			Symbol: "BTC_USDT",
			FundingReversion: fundingdomain.FundingReversionConfig{
				PostSettleTimeout: 10_000_000_000,
			},
		}}},
		bus: bus,
		log: reversionTestLogger(),
	}

	reqID := "trace-req-no-duplicate-fill"
	require.NoError(t, runner.publishEvent(context.Background(), TopicReversionOrderFilled, OrderFilledEvent{
		BaseReversionEvent: BaseReversionEvent{ReqID: reqID, Symbol: "BTC_USDT"},
		OrderID:            "ord-fill",
		FillVol:            1,
	}))
	require.NoError(t, runner.handleIOCOutcomeChecked(context.Background(), IOCOutcomeCheckedEvent{
		BaseReversionEvent: BaseReversionEvent{ReqID: reqID, Symbol: "BTC_USDT"},
		IOCEvent: IOCSubmittedEvent{
			BaseReversionEvent: BaseReversionEvent{ReqID: reqID, Symbol: "BTC_USDT"},
			OrderID:            "ord-fill",
		},
		OrderID: "ord-fill",
		Outcome: IOCOutcomeFilled,
		HoldVol: 1,
	}))

	assert.Equal(t, 1, countTopic(bus, TopicReversionOrderFilled))
	assert.Equal(t, 1, countTopic(bus, TopicReversionTimeoutGuardScheduled))
}

func TestIOCNoPositionOutcomesAbortWithoutTimeoutGuard(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		reqID      string
		orderState int
		wantReason string
	}{
		{
			name:       "canceled no fill",
			reqID:      "trace-req-ioc-canceled",
			orderState: exchange.OrderStateCanceled,
			wantReason: reversionReasonIOCCanceledNoPosition,
		},
		{
			name:       "unknown no position",
			reqID:      "trace-req-ioc-unknown",
			orderState: 1,
			wantReason: reversionReasonIOCUnknownNoPosition,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			clock := newReversionManualClock(time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC))
			bus := eventbus.New(reversionTestLogger())
			t.Cleanup(func() { _ = bus.Close() })

			client := mocks.NewMockClient(ctrl)
			client.EXPECT().GetOrder(gomock.Any(), gomock.Any(), "ord-none").Return(&exchange.OrderInfo{
				OrderID: "ord-none",
				Symbol:  "BTC_USDT",
				State:   tt.orderState,
			}, nil).AnyTimes()
			client.EXPECT().GetOpenPositions(gomock.Any(), "BTC_USDT").Return(nil, nil)

			runner := &StatelessRunner{
				deps: strategy.Deps{
					Client: client,
					Clock:  clock,
				},
				bus: bus,
				log: reversionTestLogger(),
			}

			submitted := IOCSubmittedEvent{
				BaseReversionEvent: BaseReversionEvent{ReqID: tt.reqID, Symbol: "BTC_USDT"},
				OrderID:            "ord-none",
			}
			outcome := runner.resolveIOCOutcome(context.Background(), submitted)
			assert.Equal(t, tt.wantReason, outcome.Reason)
			require.NoError(t, runner.handleIOCOutcomeChecked(context.Background(), outcome))

			abort := timelineEvent[AbortEvent](t, bus, TopicReversionAbort)
			assert.Equal(t, tt.wantReason, abort.Reason)
			assert.Equal(t, 0, countTopic(bus, TopicReversionTimeoutGuardScheduled))
		})
	}
}

func TestIOCPartialFillSchedulesTimeoutGuard(t *testing.T) {
	t.Parallel()

	clock := newReversionManualClock(time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC))
	bus := eventbus.New(reversionTestLogger())
	t.Cleanup(func() { _ = bus.Close() })

	runner := &StatelessRunner{
		deps: strategy.Deps{Clock: clock},
		globalCfg: &config.Config{Symbols: []config.SymbolConfig{{
			Symbol: "BTC_USDT",
			FundingReversion: fundingdomain.FundingReversionConfig{
				PostSettleTimeout: 10_000_000_000,
			},
		}}},
		bus: bus,
		log: reversionTestLogger(),
	}

	reqID := "trace-req-partial-fill"
	require.NoError(t, runner.handleIOCOutcomeChecked(context.Background(), IOCOutcomeCheckedEvent{
		BaseReversionEvent: BaseReversionEvent{ReqID: reqID, Symbol: "BTC_USDT"},
		IOCEvent: IOCSubmittedEvent{
			BaseReversionEvent: BaseReversionEvent{ReqID: reqID, Symbol: "BTC_USDT"},
			OrderID:            "ord-partial",
		},
		OrderID: "ord-partial",
		Outcome: IOCOutcomePartialFilled,
		HoldVol: 0.5,
	}))

	assert.Equal(t, 1, countTopic(bus, TopicReversionTimeoutGuardScheduled))
	assert.Equal(t, 0, countTopic(bus, TopicReversionAbort))
}

func TestTimeoutForceClosePathCompletes(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	clock := newReversionManualClock(time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC))
	bus := eventbus.New(reversionTestLogger())
	t.Cleanup(func() { _ = bus.Close() })

	client := mocks.NewMockClient(ctrl)
	client.EXPECT().CloseAllPositions(gomock.Any(), "BTC_USDT").Return(nil)
	mockNotifier := mocks.NewMockNotifier(ctrl)
	mockNotifier.EXPECT().Send(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	runner := &StatelessRunner{
		deps: strategy.Deps{
			Client:   client,
			Clock:    clock,
			Notifier: mockNotifier,
		},
		bus: bus,
		log: reversionTestLogger(),
	}

	reqID := "trace-req-force-close"
	ioc := IOCSubmittedEvent{
		BaseReversionEvent: BaseReversionEvent{ReqID: reqID, Symbol: "BTC_USDT"},
		OrderID:            "ord-timeout",
		Side:               shared.SideOpenLong,
	}
	require.NoError(t, runner.handleTimeoutPositionChecked(context.Background(), TimeoutPositionCheckedEvent{
		BaseReversionEvent: BaseReversionEvent{ReqID: reqID, Symbol: "BTC_USDT"},
		IOCEvent:           ioc,
		Timeout:            10 * time.Second,
		StartedAt:          clock.Now().Add(-10 * time.Second),
		HoldVol:            1.25,
	}))

	initiated := timelineEvent[ForceCloseInitiatedEvent](t, bus, TopicReversionForceCloseInitiated)
	require.NoError(t, runner.handleForceCloseInitiated(context.Background(), initiated))
	completed := timelineEvent[ForceCloseCompletedEvent](t, bus, TopicReversionForceCloseCompleted)
	require.NoError(t, runner.handleForceCloseCompleted(context.Background(), completed))
	timeout := timelineEvent[TimeoutEvent](t, bus, TopicReversionTimeout)
	require.NoError(t, runner.handleTimeout(context.Background(), timeout))

	assert.Equal(t, []string{
		TopicReversionForceCloseInitiated,
		TopicReversionForceCloseCompleted,
		TopicReversionTimeout,
		TopicReversionPositionClosed,
	}, timelineTopics(bus))
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
		deps: strategy.Deps{Clock: clock},
		bus:  eventbus.New(reversionTestLogger()),
		log:  reversionTestLogger(),
	}
	t.Cleanup(func() { _ = runner.bus.Close() })

	require.NoError(t, runner.handleWait(context.Background(), ArmedEvent{
		BaseReversionEvent: BaseReversionEvent{Symbol: "BTC_USDT"},
	}))
	require.ErrorIs(t, runner.handleWait(context.Background(), ArmedEvent{
		BaseReversionEvent: BaseReversionEvent{Symbol: "BTC_USDT", SettleTime: now},
	}), context.Canceled)
}

func TestStatelessRunnerHandleArmErrorPaths(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	clock := mocks.NewMockClock(ctrl)
	clock.EXPECT().Now().Return(time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)).AnyTimes()

	ws := mocks.NewMockSubscriber(ctrl)
	ws.EXPECT().SubscribeTicker(gomock.Any(), "BTC_USDT").Return(errors.New("sub failed"))

	runner := &StatelessRunner{
		deps: strategy.Deps{WsSub: ws, Clock: clock},
		bus:  eventbus.New(reversionTestLogger()),
		log:  reversionTestLogger(),
	}
	t.Cleanup(func() { _ = runner.bus.Close() })

	err := runner.handleArm(context.Background(), CandidateFoundEvent{
		Candidate: fundingdomain.Candidate{TradeIntent: fundingdomain.TradeIntent{Symbol: "BTC_USDT"}},
	})
	require.ErrorContains(t, err, "WS subscribe failed")
}

func TestStatelessRunnerHandleRecheckErrorPaths(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	clock := mocks.NewMockClock(ctrl)
	clock.EXPECT().Now().Return(time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)).AnyTimes()

	bus := eventbus.New(reversionTestLogger())
	t.Cleanup(func() { _ = bus.Close() })

	runner := &StatelessRunner{
		deps:      strategy.Deps{Clock: clock},
		globalCfg: &config.Config{},
		bus:       bus,
		log:       reversionTestLogger(),
	}
	err := runner.handleRecheck(context.Background(), WaitCompleteEvent{
		BaseReversionEvent: BaseReversionEvent{Symbol: "BTC_USDT"},
		Candidate: fundingdomain.Candidate{
			TradeIntent: fundingdomain.TradeIntent{Symbol: "BTC_USDT", FundingRate: 0.01},
		},
	})
	require.ErrorContains(t, err, "symbol config not found")

	tickerStore := mocks.NewMockTickerReader(ctrl)
	tickerStore.EXPECT().GetTicker(gomock.Any(), "BTC_USDT").Return(nil, errors.New("missing ticker"))
	runner.deps.TickerStore = tickerStore
	runner.globalCfg.Symbols = []config.SymbolConfig{{Symbol: "BTC_USDT", MinFundingRate: 0.001}}
	err = runner.handleRecheck(context.Background(), WaitCompleteEvent{
		BaseReversionEvent: BaseReversionEvent{Symbol: "BTC_USDT"},
		Candidate: fundingdomain.Candidate{
			TradeIntent: fundingdomain.TradeIntent{Symbol: "BTC_USDT", FundingRate: 0.01},
		},
	})
	require.ErrorContains(t, err, "no ticker for recheck")

	tickerStore.EXPECT().GetTicker(gomock.Any(), "BTC_USDT").Return(&store.TickerData{
		Symbol:      "BTC_USDT",
		FundingRate: -0.01,
	}, nil)
	err = runner.handleRecheck(context.Background(), WaitCompleteEvent{
		BaseReversionEvent: BaseReversionEvent{Symbol: "BTC_USDT"},
		Candidate: fundingdomain.Candidate{
			TradeIntent: fundingdomain.TradeIntent{Symbol: "BTC_USDT", FundingRate: 0.01},
		},
	})
	require.ErrorContains(t, err, "FR sign flip")

	tickerStore.EXPECT().GetTicker(gomock.Any(), "BTC_USDT").Return(&store.TickerData{
		Symbol:      "BTC_USDT",
		FundingRate: 0.0001,
	}, nil)
	err = runner.handleRecheck(context.Background(), WaitCompleteEvent{
		BaseReversionEvent: BaseReversionEvent{Symbol: "BTC_USDT"},
		Candidate: fundingdomain.Candidate{
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
		deps:      strategy.Deps{Clock: clock},
		globalCfg: &config.Config{},
		bus:       eventbus.New(reversionTestLogger()),
		log:       reversionTestLogger(),
	}
	t.Cleanup(func() { _ = runner.bus.Close() })

	err := runner.handleFireIOC(context.Background(), ConfirmedEvent{
		BaseReversionEvent: BaseReversionEvent{Symbol: "BTC_USDT"},
	})
	require.ErrorContains(t, err, "settle time not found")

	err = runner.handleFireIOC(context.Background(), ConfirmedEvent{
		BaseReversionEvent: BaseReversionEvent{Symbol: "BTC_USDT", SettleTime: time.Now().Add(time.Second)},
	})
	require.ErrorContains(t, err, "symbol config not found")

	clock.EXPECT().LatencyMs().Return(int64(200))
	runner.globalCfg.Symbols = []config.SymbolConfig{{
		Symbol: "BTC_USDT",
		FundingReversion: fundingdomain.FundingReversionConfig{
			MaxLatency: 50_000_000,
		},
	}}
	err = runner.handleFireIOC(context.Background(), ConfirmedEvent{
		BaseReversionEvent: BaseReversionEvent{Symbol: "BTC_USDT", SettleTime: time.Now().Add(time.Second)},
	})
	require.ErrorContains(t, err, "latency too high")
}

func TestStatelessRunnerTimeoutHelpers(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	clock := mocks.NewMockClock(ctrl)
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	clock.EXPECT().Now().Return(now).AnyTimes()
	clock.EXPECT().Sleep(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	client := mocks.NewMockClient(ctrl)
	client.EXPECT().GetOpenPositions(gomock.Any(), "BTC_USDT").Return([]exchange.Position{
		{Symbol: "BTC_USDT", HoldVol: 1.5},
		{Symbol: "BTC_USDT", HoldVol: 0.5},
	}, nil)
	client.EXPECT().CloseAllPositions(gomock.Any(), "BTC_USDT").Return(errors.New("close failed"))
	client.EXPECT().CloseAllPositions(gomock.Any(), "ETH_USDT").Return(nil)

	bus := eventbus.New(reversionTestLogger())
	t.Cleanup(func() { _ = bus.Close() })
	n := mocks.NewMockNotifier(ctrl)
	n.EXPECT().Send(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	runner := &StatelessRunner{
		deps: strategy.Deps{
			Client:   client,
			Clock:    clock,
			Notifier: n,
		},
		bus: bus,
		log: reversionTestLogger(),
	}

	holdVol, err := runner.getHoldVolume(context.Background(), "BTC_USDT")
	require.NoError(t, err)
	assert.Equal(t, 2.0, holdVol)

	retries, err := runner.forceClosePosition(context.Background(), "BTC_USDT", 1)
	require.ErrorContains(t, err, "close failed")
	assert.Equal(t, 1, retries)

	retries, err = runner.forceClosePosition(context.Background(), "ETH_USDT", 1)
	require.NoError(t, err)
	assert.Equal(t, 1, retries)

	runner.publishReversionCritical(context.Background(), BaseReversionEvent{ReqID: "test-req-crit", Symbol: "BTC_USDT"}, "BTC_USDT", "critical")
}

func TestStatelessRunnerTimeoutGuardNoFillAndMissingConfig(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	clock := mocks.NewMockClock(ctrl)
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	clock.EXPECT().Until(gomock.Any()).Return(time.Duration(0)).AnyTimes()
	clock.EXPECT().Now().Return(now).AnyTimes()

	client := mocks.NewMockClient(ctrl)
	client.EXPECT().GetOpenPositions(gomock.Any(), "BTC_USDT").Return(nil, nil)

	bus := eventbus.New(reversionTestLogger())
	t.Cleanup(func() { _ = bus.Close() })
	n := mocks.NewMockNotifier(ctrl)
	n.EXPECT().Send(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	runner := &StatelessRunner{
		deps: strategy.Deps{
			Client:   client,
			Clock:    clock,
			Notifier: n,
		},
		globalCfg: &config.Config{Symbols: []config.SymbolConfig{{
			Symbol: "BTC_USDT",
			FundingReversion: fundingdomain.FundingReversionConfig{
				PostSettleTimeout: 10_000_000,
			},
		}}},
		bus: bus,
		log: reversionTestLogger(),
	}

	require.NoError(t, runner.waitTimeoutDeadline(context.Background(), TimeoutGuardScheduledEvent{
		BaseReversionEvent: BaseReversionEvent{Symbol: "BTC_USDT"},
		IOCEvent: IOCSubmittedEvent{
			BaseReversionEvent: BaseReversionEvent{Symbol: "BTC_USDT", SettleTime: now.Add(-time.Second)},
		},
		Timeout:   10 * time.Millisecond,
		StartedAt: now,
	}))

	require.NoError(t, runner.timeoutGuard(context.Background(), IOCSubmittedEvent{
		BaseReversionEvent: BaseReversionEvent{Symbol: "ETH_USDT"},
	}))
}

type reversionManualClock struct {
	now       time.Time
	latencyMs int64
	offsetMs  int64
}

func newReversionManualClock(now time.Time) *reversionManualClock {
	return &reversionManualClock{now: now, latencyMs: 20}
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
			Leverage:            1,
		},
		TradeIntent: fundingdomain.TradeIntent{
			Symbol:    "BTC_USDT",
			Side:      shared.SideOpenLong,
			CloseSide: shared.SideCloseLong,
		},
		ContractSpec: fundingdomain.ContractSpec{
			PriceUnit:    0.01,
			VolUnit:      1,
			MinVol:       1,
			PriceScale:   2,
			VolScale:     4,
			ContractSize: 0.001,
		},
		MarketData: fundingdomain.MarketData{
			LastPrice: 60000,
			BestBid:   59990,
			BestAsk:   60000,
			Volume24:  1000,
			Amount24:  60_000_000,
		},
		TradePlan: fundingdomain.TradePlan{
			Volume: 1,
		},
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

func timelineEvent[T any](t *testing.T, bus *eventbus.Bus, topic string) T {
	t.Helper()
	for _, entry := range bus.Timeline() {
		if entry.Topic != topic {
			continue
		}
		var evt T
		require.NoError(t, json.Unmarshal(entry.Payload, &evt))
		return evt
	}
	var zero T
	require.Failf(t, "event not found", "topic %s not found in timeline %v", topic, timelineTopics(bus))
	return zero
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
