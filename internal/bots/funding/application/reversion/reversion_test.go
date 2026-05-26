package reversion_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"crypto-bot/internal/bots/funding/application"
	"crypto-bot/internal/bots/funding/application/reversion"
	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/bots/funding/domain"
	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/internal/testutil/mocks"
	"crypto-bot/pkg/eventbus"
	"crypto-bot/pkg/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestStrategy_Execute_Success(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Mock dependencies
	mockClient := mocks.NewMockClient(ctrl)
	mockWs := mocks.NewMockSubscriber(ctrl)
	mockOrderNotifier := mocks.NewMockOrderNotifier(ctrl)
	mockTickerStore := mocks.NewMockTickerReader(ctrl)
	mockContractStore := mocks.NewMockContractReader(ctrl)
	mockPriceStore := mocks.NewMockPriceReader(ctrl)
	mockNotifier := mocks.NewMockNotifier(ctrl)

	// Set up Clock
	mockClock := mocks.NewMockClock(ctrl)
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	mockClock.EXPECT().Now().Return(now).AnyTimes()
	mockClock.EXPECT().GetServerTime().Return(now.UnixMilli()).AnyTimes()
	mockClock.EXPECT().LatencyMs().Return(int64(20)).AnyTimes()
	mockClock.EXPECT().Offset().Return(int64(0)).AnyTimes()
	mockClock.EXPECT().Until(gomock.Any()).DoAndReturn(func(target time.Time) time.Duration {
		return target.Sub(now)
	}).AnyTimes()
	mockClock.EXPECT().Sleep(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, d time.Duration) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d):
			return nil
		}
	}).AnyTimes()

	bus := eventbus.New(slog.Default())
	defer func() { _ = bus.Close() }()

	deps := application.Deps{
		Client:        mockClient,
		WsSub:         mockWs,
		OrderNotifier: mockOrderNotifier,
		TickerStore:   mockTickerStore,
		ContractStore: mockContractStore,
		PriceStore:    mockPriceStore,
		Clock:         mockClock,
		Log:           slog.Default(),
		Notifier:      mockNotifier,
		EventBus:      bus,
	}

	cfg := config.SymbolConfig{
		Symbol:         "BTC_USDT",
		MinFundingRate: 0.0001,
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
			Symbol: "BTC_USDT",
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

	// 2. Recheck expectations
	mockTickerStore.EXPECT().GetTicker(gomock.Any(), "BTC_USDT").Return(&store.TickerData{
		Symbol:      "BTC_USDT",
		FundingRate: 0.001,
		LastPrice:   60000.0,
		BestBid:     59990.0,
		BestAsk:     60000.0,
	}, nil)

	// 3. FireIOC expectations
	mockClient.EXPECT().CreateOrder(gomock.Any(), gomock.Any()).Return("ord_123", nil)
	mockClient.EXPECT().GetOrder(gomock.Any(), "ord_123").Return(&exchange.OrderInfo{
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
	subCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := bus.Subscribe(subCtx, reversion.TopicReversionCompleted)
	require.NoError(t, err)

	strategy := reversion.NewStrategy(cfg, globalCfg, deps)
	err = strategy.Execute(context.Background(), now.Add(5*time.Second), candidate)
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
				return
			}
			msg.Ack()
		case <-time.After(15 * time.Second):
			t.Fatal("Timeout waiting for TopicReversionCompleted")
		}
	}
}
