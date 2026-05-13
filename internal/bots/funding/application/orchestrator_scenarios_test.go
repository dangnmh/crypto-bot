package application_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"crypto-bot/internal/bots/funding/application"
	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/bots/funding/domain"
	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/internal/testutil/mocks"

	"go.uber.org/mock/gomock"
)

type orchestratorMocks struct {
	tickerStore   *mocks.MockTickerReader
	contractStore *mocks.MockContractReader
	priceStore    *mocks.MockPriceReader
	fundingStore  *mocks.MockFundingReader
	klineStore    *mocks.MockKlineReadWriter
	depthStore    *mocks.MockDepthReader
	subscriber    *mocks.MockSubscriber
	clock         *mocks.MockClock
	client        *mocks.MockClient
	ws            *mocks.MockOrderNotifier
}

func setupOrchestrator(t *testing.T, ctrl *gomock.Controller) (*application.CycleOrchestrator, orchestratorMocks) {
	m := orchestratorMocks{
		tickerStore:   mocks.NewMockTickerReader(ctrl),
		contractStore: mocks.NewMockContractReader(ctrl),
		priceStore:    mocks.NewMockPriceReader(ctrl),
		fundingStore:  mocks.NewMockFundingReader(ctrl),
		klineStore:    mocks.NewMockKlineReadWriter(ctrl),
		depthStore:    mocks.NewMockDepthReader(ctrl),
		subscriber:    mocks.NewMockSubscriber(ctrl),
		clock:         mocks.NewMockClock(ctrl),
		client:        mocks.NewMockClient(ctrl),
		ws:            mocks.NewMockOrderNotifier(ctrl),
	}

	cfg := config.SymbolConfig{
		Symbol:         "BTC_USDT",
		MinFundingRate: 0.001,
		FundingReversion: domain.FundingReversionConfig{
			Trailing: domain.TrailingConfig{Enabled: true},
		},
		FundingTrap: domain.FundingTrapConfig{
			Enabled:  true,
			Trailing: domain.TrailingConfig{Enabled: true},
		},
	}
	global := &config.Config{System: &config.SystemConfig{Safety: config.SafetyConfig{
		BufferTime:        10 * 1000000,
		PostSettleTimeout: 10 * 1000000000,
	}}}

	deps := application.Deps{
		Client:        m.client,
		WsSub:         m.subscriber,
		OrderNotifier: m.ws,
		TickerStore:   m.tickerStore,
		ContractStore: m.contractStore,
		PriceStore:    m.priceStore,
		FundingStore:  m.fundingStore,
		KlineStore:    m.klineStore,
		DepthStore:    m.depthStore,
		Clock:         m.clock,
		Log:           slog.Default(),
	}

	m.subscriber.EXPECT().UnsubscribeTicker("BTC_USDT").Return(nil).AnyTimes()
	m.depthStore.EXPECT().GetDepth(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	m.client.EXPECT().CloseAllPositions(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	m.client.EXPECT().CreateTrackOrder(gomock.Any(), gomock.Any()).Return("track_1", nil).AnyTimes()
	m.clock.EXPECT().Sleep(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, d time.Duration) error {
		if d >= 5*time.Second {
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}).AnyTimes()

	return application.NewCycleOrchestrator(cfg, global, deps), m
}

func TestCycleOrchestrator_ScanFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(m orchestratorMocks)
	}{
		{
			name: "Ticker Retrieval Error",
			setup: func(m orchestratorMocks) {
				m.tickerStore.EXPECT().GetTicker(gomock.Any(), "BTC_USDT").Return(nil, errors.New("network error"))
			},
		},
		{
			name: "FR Below Threshold",
			setup: func(m orchestratorMocks) {
				m.tickerStore.EXPECT().GetTicker(gomock.Any(), "BTC_USDT").Return(&store.TickerData{
					Symbol:      "BTC_USDT",
					FundingRate: 0.0001, // Below 0.001 threshold
				}, nil)
			},
		},
		{
			name: "Enrichment Failure",
			setup: func(m orchestratorMocks) {
				m.tickerStore.EXPECT().GetTicker(gomock.Any(), "BTC_USDT").Return(&store.TickerData{
					Symbol:      "BTC_USDT",
					FundingRate: 0.005,
				}, nil)
				m.contractStore.EXPECT().GetContract(gomock.Any(), "BTC_USDT").Return(nil, errors.New("contract missing"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			o, m := setupOrchestrator(t, ctrl)
			tt.setup(m)

			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()

			o.Run(ctx, time.Now())
		})
	}
}

func TestCycleOrchestrator_RecheckSignFlip(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	o, m := setupOrchestrator(t, ctrl)

	// Scan Phase
	m.tickerStore.EXPECT().GetTicker(gomock.Any(), "BTC_USDT").Return(&store.TickerData{
		Symbol:      "BTC_USDT",
		FundingRate: 0.005,
		LastPrice:   50000,
		BestBid:     49999,
		BestAsk:     50001,
	}, nil).Times(1)
	m.contractStore.EXPECT().GetContract(gomock.Any(), "BTC_USDT").Return(&store.ContractData{
		PriceUnit:    0.1,
		VolUnit:      1,
		MinVol:       1,
		ContractSize: 0.001,
		TakerFeeRate: 0.0006,
	}, nil).AnyTimes()

	// Arm Phase
	m.subscriber.EXPECT().SubscribeTicker("BTC_USDT").Return(nil).AnyTimes()
	m.priceStore.EXPECT().GetPrice(gomock.Any(), "BTC_USDT", gomock.Any()).Return(&store.PriceData{
		BestBid: 49999, BestAsk: 50001, LastPrice: 50000,
	}, nil).AnyTimes()

	// Wait Phase
	m.clock.EXPECT().Until(gomock.Any()).Return(time.Duration(0)).AnyTimes()

	// Recheck Phase -> Sign flipped from +0.005 to -0.001
	m.tickerStore.EXPECT().GetTicker(gomock.Any(), "BTC_USDT").Return(&store.TickerData{
		Symbol:      "BTC_USDT",
		FundingRate: -0.001,
	}, nil).Times(1)

	// Abort will be triggered, ensure unsubscribe is called
	m.subscriber.EXPECT().UnsubscribeTicker("BTC_USDT").Return(nil).AnyTimes()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	o.Run(ctx, time.Now())
}

func TestCycleOrchestrator_CreateOrderFails(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	o, m := setupOrchestrator(t, ctrl)

	m.tickerStore.EXPECT().GetTicker(gomock.Any(), "BTC_USDT").Return(&store.TickerData{
		Symbol:      "BTC_USDT",
		FundingRate: 0.005,
		LastPrice:   50000,
		BestBid:     49999,
		BestAsk:     50001,
	}, nil).AnyTimes()
	m.contractStore.EXPECT().GetContract(gomock.Any(), "BTC_USDT").Return(&store.ContractData{
		PriceUnit: 0.1, VolUnit: 1, MinVol: 1, ContractSize: 0.001,
	}, nil).AnyTimes()
	m.subscriber.EXPECT().SubscribeTicker("BTC_USDT").Return(nil).AnyTimes()
	m.priceStore.EXPECT().GetPrice(gomock.Any(), "BTC_USDT", gomock.Any()).Return(&store.PriceData{
		BestBid: 49999, BestAsk: 50001, LastPrice: 50000,
	}, nil).AnyTimes()
	m.clock.EXPECT().Until(gomock.Any()).Return(time.Duration(0)).AnyTimes()

	m.clock.EXPECT().LatencyMs().Return(int64(50)).AnyTimes()
	m.clock.EXPECT().Now().Return(time.Now()).AnyTimes()
	m.clock.EXPECT().GetServerTime().Return(time.Now().UnixMilli()).AnyTimes()
	m.clock.EXPECT().Offset().Return(int64(0)).AnyTimes()

	// FAIL IOC CREATION
	m.client.EXPECT().CreateOrder(gomock.Any(), gomock.Any()).Return("", errors.New("API error"))
	m.subscriber.EXPECT().UnsubscribeTicker("BTC_USDT").Return(nil).AnyTimes()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	o.Run(ctx, time.Now())
}

func TestCycleOrchestrator_PartialFillIOC(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	o, m := setupOrchestrator(t, ctrl)

	m.tickerStore.EXPECT().GetTicker(gomock.Any(), "BTC_USDT").Return(&store.TickerData{
		Symbol:      "BTC_USDT",
		FundingRate: 0.005,
		LastPrice:   50000,
		BestBid:     49999,
		BestAsk:     50001,
	}, nil).AnyTimes()
	m.contractStore.EXPECT().GetContract(gomock.Any(), "BTC_USDT").Return(&store.ContractData{
		PriceUnit: 0.1, VolUnit: 1, MinVol: 1, ContractSize: 0.001,
	}, nil).AnyTimes()
	m.subscriber.EXPECT().SubscribeTicker("BTC_USDT").Return(nil).AnyTimes()
	m.priceStore.EXPECT().GetPrice(gomock.Any(), "BTC_USDT", gomock.Any()).Return(&store.PriceData{
		BestBid: 49999, BestAsk: 50001, LastPrice: 50000,
	}, nil).AnyTimes()
	m.clock.EXPECT().Until(gomock.Any()).Return(time.Duration(0)).AnyTimes()
	m.clock.EXPECT().LatencyMs().Return(int64(50)).AnyTimes()
	m.clock.EXPECT().Now().Return(time.Now()).AnyTimes()
	m.clock.EXPECT().GetServerTime().Return(time.Now().UnixMilli()).AnyTimes()
	m.clock.EXPECT().Offset().Return(int64(0)).AnyTimes()

	m.depthStore.EXPECT().GetDepth(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()

	m.client.EXPECT().CreateOrder(gomock.Any(), gomock.Any()).Return("ioc_1", nil).AnyTimes()

	// Partially Filled and Canceled Simulation
	m.ws.EXPECT().OnOrderUpdate(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(orderID string, duration time.Duration, callback func(exchange.WsOrderDeal)) {
		go func() {
			time.Sleep(10 * time.Millisecond)
			callback(exchange.WsOrderDeal{
				OrderID:      orderID,
				State:        exchange.OrderStatePartial,
				DealVol:      0.5,
				DealAvgPrice: 50000,
			})
			time.Sleep(10 * time.Millisecond)
			callback(exchange.WsOrderDeal{
				OrderID:      orderID,
				State:        exchange.OrderStateCanceled,
				DealVol:      0.5,
				DealAvgPrice: 50000,
			})
		}()
	}).AnyTimes()

	m.ws.EXPECT().RemoveOrderCallback(gomock.Any()).AnyTimes()

	// Because it's partially filled, Trap order should be fired (but for the filled amount).
	m.client.EXPECT().CreateOrder(gomock.Any(), gomock.Any()).Return("trap_1", nil).AnyTimes()
	m.subscriber.EXPECT().UnsubscribeTicker("BTC_USDT").Return(nil).AnyTimes()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	o.Run(ctx, time.Now())
}

func TestCycleOrchestrator_TrailingTrapRejection(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	o, m := setupOrchestrator(t, ctrl)

	m.tickerStore.EXPECT().GetTicker(gomock.Any(), "BTC_USDT").Return(&store.TickerData{
		Symbol:      "BTC_USDT",
		FundingRate: 0.005,
		LastPrice:   50000,
		BestBid:     49999,
		BestAsk:     50001,
	}, nil).AnyTimes()
	m.contractStore.EXPECT().GetContract(gomock.Any(), "BTC_USDT").Return(&store.ContractData{
		PriceUnit: 0.1, VolUnit: 1, MinVol: 1, ContractSize: 0.001,
	}, nil).AnyTimes()
	m.subscriber.EXPECT().SubscribeTicker("BTC_USDT").Return(nil).AnyTimes()
	m.priceStore.EXPECT().GetPrice(gomock.Any(), "BTC_USDT", gomock.Any()).Return(&store.PriceData{
		BestBid: 49999, BestAsk: 50001, LastPrice: 50000,
	}, nil).AnyTimes()
	m.clock.EXPECT().Until(gomock.Any()).Return(time.Duration(0)).AnyTimes()
	m.clock.EXPECT().LatencyMs().Return(int64(50)).AnyTimes()
	m.clock.EXPECT().Now().Return(time.Now()).AnyTimes()
	m.clock.EXPECT().GetServerTime().Return(time.Now().UnixMilli()).AnyTimes()
	m.clock.EXPECT().Offset().Return(int64(0)).AnyTimes()

	m.depthStore.EXPECT().GetDepth(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()

	m.client.EXPECT().CreateOrder(gomock.Any(), gomock.Any()).Return("ioc_1", nil).AnyTimes()
	m.ws.EXPECT().OnOrderUpdate(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(orderID string, duration time.Duration, callback func(exchange.WsOrderDeal)) {
		go func() {
			time.Sleep(10 * time.Millisecond)
			callback(exchange.WsOrderDeal{
				OrderID:      orderID,
				State:        exchange.OrderStateFilled,
				DealVol:      1.0,
				DealAvgPrice: 50000,
			})
		}()
	}).AnyTimes()

	m.ws.EXPECT().RemoveOrderCallback(gomock.Any()).AnyTimes()

	m.client.EXPECT().CreateOrder(gomock.Any(), gomock.Any()).Return("trap_1", nil).AnyTimes()

	// FAIL TRAILING STOP
	m.client.EXPECT().CreateTrackOrder(gomock.Any(), gomock.Any()).Return("", errors.New("API failure tracking")).AnyTimes()

	m.subscriber.EXPECT().UnsubscribeTicker("BTC_USDT").Return(nil).AnyTimes()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	o.Run(ctx, time.Now())
}

func TestCycleOrchestrator_OBTrapPath(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	o, m := setupOrchestrator(t, ctrl)

	m.tickerStore.EXPECT().GetTicker(gomock.Any(), "BTC_USDT").Return(&store.TickerData{
		Symbol:      "BTC_USDT",
		FundingRate: 0.005,
		LastPrice:   50000,
		BestBid:     49999,
		BestAsk:     50001,
	}, nil).AnyTimes()
	m.contractStore.EXPECT().GetContract(gomock.Any(), "BTC_USDT").Return(&store.ContractData{
		PriceUnit: 0.1, VolUnit: 1, MinVol: 1, ContractSize: 0.001,
	}, nil).AnyTimes()
	m.subscriber.EXPECT().SubscribeTicker("BTC_USDT").Return(nil).AnyTimes()
	m.priceStore.EXPECT().GetPrice(gomock.Any(), "BTC_USDT", gomock.Any()).Return(&store.PriceData{
		BestBid: 49999, BestAsk: 50001, LastPrice: 50000,
	}, nil).AnyTimes()
	m.clock.EXPECT().Until(gomock.Any()).Return(time.Duration(0)).AnyTimes()
	m.clock.EXPECT().LatencyMs().Return(int64(50)).AnyTimes()
	m.clock.EXPECT().Now().Return(time.Now()).AnyTimes()
	m.clock.EXPECT().GetServerTime().Return(time.Now().UnixMilli()).AnyTimes()
	m.clock.EXPECT().Offset().Return(int64(0)).AnyTimes()

	// Provide a valid OB to trigger fireOBTrap
	ob := &shared.OrderBook{
		Symbol: "BTC_USDT",
		Bids:   []shared.OrderBookEntry{{Price: 49000, Volume: 1000}},
	}
	m.depthStore.EXPECT().GetDepth(gomock.Any(), gomock.Any()).Return(ob, nil).AnyTimes()

	m.client.EXPECT().CreateOrder(gomock.Any(), gomock.Any()).Return("ioc_1", nil).AnyTimes()
	m.ws.EXPECT().OnOrderUpdate(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(orderID string, duration time.Duration, callback func(exchange.WsOrderDeal)) {
		go func() {
			time.Sleep(10 * time.Millisecond)
			callback(exchange.WsOrderDeal{
				OrderID:      orderID,
				State:        exchange.OrderStateFilled,
				DealVol:      1.0,
				DealAvgPrice: 50000,
			})
		}()
	}).AnyTimes()

	m.ws.EXPECT().RemoveOrderCallback(gomock.Any()).AnyTimes()

	m.client.EXPECT().CreateOrder(gomock.Any(), gomock.Any()).Return("trap_1", nil).AnyTimes()
	m.subscriber.EXPECT().UnsubscribeTicker("BTC_USDT").Return(nil).AnyTimes()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	o.Run(ctx, time.Now())
}
