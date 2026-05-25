package reversion

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"crypto-bot/internal/bots/funding/application"
	"crypto-bot/internal/bots/funding/config"
	fundingdomain "crypto-bot/internal/bots/funding/domain"
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

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	client.EXPECT().CloseAllPositions(gomock.Any(), "BTC_USDT").Return(errors.New("close failed"))

	strategy := NewStrategy(
		config.SymbolConfig{
			Symbol: "BTC_USDT",
			FundingReversion: fundingdomain.FundingReversionConfig{
				Enabled: true,
			},
		},
		&config.Config{},
		application.Deps{Client: client, Log: reversionTestLogger()},
	)

	assert.Equal(t, FlowReversion, strategy.Flow())
	assert.True(t, strategy.Enabled(config.SymbolConfig{
		FundingReversion: fundingdomain.FundingReversionConfig{Enabled: true},
	}))
	assert.False(t, strategy.Enabled(config.SymbolConfig{}))
	require.ErrorContains(t, strategy.CleanupOpenExposure(context.Background()), "close failed")
}

func TestStatelessRunnerRetryWithBackoff(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	clock := mocks.NewMockClock(ctrl)
	clock.EXPECT().Sleep(gomock.Any(), gomock.Any()).Return(nil).Times(2)

	runner := &StatelessRunner{
		deps: application.Deps{Clock: clock},
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
		deps: application.Deps{Clock: clock},
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
		deps: application.Deps{
			WsSub:    ws,
			Clock:    clock,
			Notifier: n,
		},
		globalCfg: &config.Config{Symbols: []config.SymbolConfig{{Symbol: "BTC_USDT"}}},
		bus:       bus,
		log:       reversionTestLogger(),
	}

	ctx := context.Background()
	runner.abort(ctx, "BTC_USDT", "not profitable")

	final := runner.calculateFinalPnL(PositionClosedEvent{
		BaseReversionEvent: BaseReversionEvent{Symbol: "BTC_USDT"},
		EntryPrice:         100,
		ClosePrice:         101,
		CloseVol:           2,
		GrossProfit:        2,
		NetProfit:          1.8,
		Fee:                -0.1,
		HoldFee:            -0.1,
		HoldDurationMs:     250,
	})
	assert.Equal(t, "BTC_USDT", final.Symbol)
	assert.Equal(t, 1.8, final.NetPnL)

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
		deps: application.Deps{
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
	})
	runner.handlePositionUpdate(context.Background(), exchange.PersonalPositionUpdate{
		Symbol:          "BTC_USDT",
		HoldVol:         0,
		CloseVol:        1,
		OpenAvgPrice:    100,
		CloseAvgPrice:   101,
		CloseProfitLoss: 1,
		Fee:             -0.1,
		HoldFee:         -0.01,
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
		deps:      application.Deps{Clock: clock},
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

func TestStatelessRunnerHandleWaitBranches(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	clock := mocks.NewMockClock(ctrl)
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	clock.EXPECT().Now().Return(now).AnyTimes()
	clock.EXPECT().Until(now.Add(-2 * time.Second)).Return(10 * time.Millisecond)
	clock.EXPECT().Sleep(gomock.Any(), 10*time.Millisecond).Return(context.Canceled)

	runner := &StatelessRunner{
		deps: application.Deps{Clock: clock},
		bus:  eventbus.New(reversionTestLogger()),
		log:  reversionTestLogger(),
	}
	t.Cleanup(func() { _ = runner.bus.Close() })

	require.NoError(t, runner.handleWait(context.Background(), ArmedEvent{
		BaseReversionEvent: BaseReversionEvent{Symbol: "BTC_USDT"},
	}))
	require.ErrorIs(t, runner.handleWait(context.Background(), ArmedEvent{
		BaseReversionEvent: BaseReversionEvent{Symbol: "BTC_USDT"},
		SettleTime:         now,
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
		deps: application.Deps{WsSub: ws, Clock: clock},
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
		deps:      application.Deps{Clock: clock},
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
		deps:      application.Deps{Clock: clock},
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
		BaseReversionEvent: BaseReversionEvent{Symbol: "BTC_USDT"},
		SettleTime:         time.Now().Add(time.Second),
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
		BaseReversionEvent: BaseReversionEvent{Symbol: "BTC_USDT"},
		SettleTime:         time.Now().Add(time.Second),
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
		deps: application.Deps{
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

	runner.publishReversionCritical(context.Background(), "BTC_USDT", "critical")
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
		deps: application.Deps{
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

	require.NoError(t, runner.timeoutGuard(context.Background(), IOCFiredEvent{
		BaseReversionEvent: BaseReversionEvent{Symbol: "BTC_USDT"},
		SettleTime:         now.Add(-time.Second),
	}))

	require.NoError(t, runner.timeoutGuard(context.Background(), IOCFiredEvent{
		BaseReversionEvent: BaseReversionEvent{Symbol: "ETH_USDT"},
	}))
}
