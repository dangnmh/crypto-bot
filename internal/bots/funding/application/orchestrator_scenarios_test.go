package application_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"crypto-bot/internal/bots/funding/application"
	"crypto-bot/internal/bots/funding/application/events"
	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/bots/funding/domain"
	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/internal/testutil/mocks"
	"crypto-bot/pkg/types"

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
	priceUpdates  chan *store.PriceData
	recorder      *recordingCycleRecorder
}

type recordingCycleRecorder struct {
	mu      sync.Mutex
	records []domain.CycleRecord
}

func (r *recordingCycleRecorder) Record(_ context.Context, record domain.CycleRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, record)
	return nil
}

func (r *recordingCycleRecorder) Close() error { return nil }

func (r *recordingCycleRecorder) last() (domain.CycleRecord, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.records) == 0 {
		return domain.CycleRecord{}, false
	}
	return r.records[len(r.records)-1], true
}

func hasTimelineTopic(record domain.CycleRecord, topic string) bool {
	for _, entry := range record.Timeline {
		if entry.Topic == topic {
			return true
		}
	}
	return false
}

func assertAbortContext(t *testing.T, record domain.CycleRecord, flow, topic string) {
	t.Helper()
	if record.AbortFlow != flow || record.AbortTopic != topic {
		t.Fatalf("expected abort flow=%q topic=%q, got flow=%q topic=%q", flow, topic, record.AbortFlow, record.AbortTopic)
	}
}

func assertErrorContext(t *testing.T, record domain.CycleRecord, flow, topic string) {
	t.Helper()
	if record.ErrorFlow != flow || record.ErrorTopic != topic {
		t.Fatalf("expected error flow=%q topic=%q, got flow=%q topic=%q", flow, topic, record.ErrorFlow, record.ErrorTopic)
	}
}

func assertTimelineHasTopics(t *testing.T, record domain.CycleRecord, topics ...string) {
	t.Helper()
	for _, topic := range topics {
		if !hasTimelineTopic(record, topic) {
			t.Fatalf("expected timeline topic %q", topic)
		}
	}
}

func assertTimelineMissingTopics(t *testing.T, record domain.CycleRecord, topics ...string) {
	t.Helper()
	for _, topic := range topics {
		if hasTimelineTopic(record, topic) {
			t.Fatalf("unexpected timeline topic %q", topic)
		}
	}
}

func setupOrchestrator(t *testing.T, ctrl *gomock.Controller) (*application.CycleOrchestrator, orchestratorMocks) {
	cfg := config.SymbolConfig{
		Symbol:         "BTC_USDT",
		MinFundingRate: 0.001,
		MarginUSDT:     100,
		Leverage:       1,
		FundingReversion: domain.FundingReversionConfig{
			Trailing: domain.TrailingConfig{Enabled: true},
		},
		FundingTrap: domain.FundingTrapConfig{
			Enabled:   true,
			SizeRatio: 0.5,
			Trailing:  domain.TrailingConfig{Enabled: true},
		},
	}
	return setupOrchestratorWithConfig(t, ctrl, cfg)
}

func setupOrchestratorWithConfig(
	t *testing.T,
	ctrl *gomock.Controller,
	cfg config.SymbolConfig,
) (*application.CycleOrchestrator, orchestratorMocks) {
	if !cfg.FundingReversion.Enabled {
		cfg.FundingReversion.Enabled = true
	}
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
		priceUpdates:  make(chan *store.PriceData, 32),
		recorder:      &recordingCycleRecorder{},
	}

	global := &config.Config{System: &config.SystemConfig{Safety: config.SafetyConfig{}}}

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
		CycleRecorder: m.recorder,
	}

	m.subscriber.EXPECT().UnsubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil).AnyTimes()
	m.depthStore.EXPECT().GetDepth(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	m.priceStore.EXPECT().SubscribePrice(gomock.Any(), "BTC_USDT", gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ string, _ int) <-chan *store.PriceData {
			ch := make(chan *store.PriceData, 32)
			go func() {
				defer close(ch)
				for {
					select {
					case <-ctx.Done():
						return
					case pd := <-m.priceUpdates:
						select {
						case ch <- pd:
						case <-ctx.Done():
							return
						}
					}
				}
			}()
			return ch
		}).AnyTimes()
	m.clock.EXPECT().Sleep(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, d time.Duration) error {
		if d >= 5*time.Second {
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}).AnyTimes()

	return application.NewCycleOrchestrator(cfg, global, deps), m
}

func expectTrapThenReversionTimeoutSleep(m orchestratorMocks, trapTimeout, reversionTimeout time.Duration) {
	m.clock.EXPECT().Sleep(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, d time.Duration) error {
		if d == trapTimeout {
			return nil
		}
		if d == reversionTimeout {
			time.Sleep(reversionTimeout)
			return nil
		}
		return defaultMockSleep(ctx, d)
	}).AnyTimes()
}

func expectDelayedReversionTimeoutSleep(m orchestratorMocks, reversionTimeout time.Duration) {
	m.clock.EXPECT().Sleep(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, d time.Duration) error {
		if d == reversionTimeout {
			time.Sleep(reversionTimeout)
			return nil
		}
		return defaultMockSleep(ctx, d)
	}).AnyTimes()
}

func defaultMockSleep(ctx context.Context, d time.Duration) error {
	if d < 5*time.Second {
		return nil
	}
	<-ctx.Done()
	return ctx.Err()
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

	// Scan event
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

	// Arm event
	m.subscriber.EXPECT().SubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil).AnyTimes()
	m.priceStore.EXPECT().GetPrice(gomock.Any(), "BTC_USDT", gomock.Any()).Return(&store.PriceData{
		BestBid: 49999, BestAsk: 50001, LastPrice: 50000,
	}, nil).AnyTimes()

	// Wait event
	m.clock.EXPECT().Until(gomock.Any()).Return(time.Duration(0)).AnyTimes()

	// Recheck event -> Sign flipped from +0.005 to -0.001
	m.tickerStore.EXPECT().GetTicker(gomock.Any(), "BTC_USDT").Return(&store.TickerData{
		Symbol:      "BTC_USDT",
		FundingRate: -0.001,
	}, nil).Times(1)

	// Abort will be triggered, ensure unsubscribe is called
	m.subscriber.EXPECT().UnsubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil).AnyTimes()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	o.Run(ctx, time.Now())
}

func TestCycleOrchestrator_ImbalanceFilterRejectsCandidate(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	cfg := config.SymbolConfig{
		Symbol:         "BTC_USDT",
		MinFundingRate: 0.001,
		MarginUSDT:     100,
		Leverage:       1,
		FundingReversion: domain.FundingReversionConfig{
			Enabled: true,
			ImbalanceFilter: domain.ImbalanceFilterConfig{
				Enabled:      true,
				NearPct:      0.001,
				MinLongRatio: 1.2,
			},
		},
	}
	o, m := setupOrchestratorWithConfig(t, ctrl, cfg)

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
	m.subscriber.EXPECT().SubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil).AnyTimes()
	m.subscriber.EXPECT().SubscribeDepth(gomock.Any(), "BTC_USDT", "").Return(nil).AnyTimes()
	m.subscriber.EXPECT().UnsubscribeDepth(gomock.Any(), "BTC_USDT", "").Return(nil).AnyTimes()
	m.priceStore.EXPECT().GetPrice(gomock.Any(), "BTC_USDT", gomock.Any()).Return(&store.PriceData{
		BestBid: 49999, BestAsk: 50001, LastPrice: 50000,
	}, nil).AnyTimes()
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	o.Run(ctx, time.Now())

	record, ok := m.recorder.last()
	if !ok {
		t.Fatal("expected cycle record")
	}
	if record.Outcome != domain.OutcomeAborted {
		t.Fatalf("expected imbalance abort, got %s", record.Outcome)
	}
	if !strings.Contains(record.AbortReason, "imbalance ratio") {
		t.Fatalf("expected imbalance abort reason, got %q", record.AbortReason)
	}
	if !record.Decision.ImbalanceFilterEnabled {
		t.Fatal("expected imbalance filter to be journaled as enabled")
	}
	if record.Decision.ImbalanceFilterPassed {
		t.Fatal("expected failed imbalance filter in journal")
	}
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
	m.subscriber.EXPECT().SubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil).AnyTimes()
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
	m.subscriber.EXPECT().UnsubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil).AnyTimes()

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
	m.subscriber.EXPECT().SubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil).AnyTimes()
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
	m.ws.EXPECT().OnOrderUpdate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, orderID string, duration time.Duration, callback func(exchange.WsOrderDeal)) {
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
	m.client.EXPECT().CreateTrackOrder(gomock.Any(), gomock.Any()).Return("track_1", nil).AnyTimes()
	m.subscriber.EXPECT().UnsubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil).AnyTimes()

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
	m.subscriber.EXPECT().SubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil).AnyTimes()
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
	m.ws.EXPECT().OnOrderUpdate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, orderID string, duration time.Duration, callback func(exchange.WsOrderDeal)) {
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
	m.client.EXPECT().
		ClosePosition(gomock.Any(), "BTC_USDT", shared.SideCloseLong, 1.0, gomock.Any()).
		Return(nil).AnyTimes()

	m.subscriber.EXPECT().UnsubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil).AnyTimes()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	o.Run(ctx, time.Now())
}

func TestCycleOrchestrator_CriticalCloseFailureAfterTrailingRejection(t *testing.T) {
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
	m.subscriber.EXPECT().SubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil).AnyTimes()
	m.priceStore.EXPECT().GetPrice(gomock.Any(), "BTC_USDT", gomock.Any()).Return(&store.PriceData{
		BestBid: 49999, BestAsk: 50001, LastPrice: 50000,
	}, nil).AnyTimes()
	m.clock.EXPECT().Until(gomock.Any()).Return(time.Duration(0)).AnyTimes()
	m.clock.EXPECT().LatencyMs().Return(int64(50)).AnyTimes()
	m.clock.EXPECT().Now().Return(time.Now()).AnyTimes()
	m.clock.EXPECT().GetServerTime().Return(time.Now().UnixMilli()).AnyTimes()
	m.clock.EXPECT().Offset().Return(int64(0)).AnyTimes()

	m.client.EXPECT().CreateOrder(gomock.Any(), gomock.Any()).Return("ioc_1", nil).AnyTimes()
	m.ws.EXPECT().OnOrderUpdate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, orderID string, duration time.Duration, callback func(exchange.WsOrderDeal)) {
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

	m.client.EXPECT().CreateTrackOrder(gomock.Any(), gomock.Any()).Return("", errors.New("API failure tracking")).AnyTimes()
	m.client.EXPECT().
		ClosePosition(gomock.Any(), "BTC_USDT", shared.SideCloseLong, 1.0, gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ string, _ shared.Side, _ float64, _ int) error {
			if ctx.Err() != nil {
				t.Fatalf("exact close used a cancelled context: %v", ctx.Err())
			}
			return errors.New("exact close rejected")
		}).AnyTimes()
	m.client.EXPECT().CloseAllPositions(gomock.Any(), "BTC_USDT").DoAndReturn(func(ctx context.Context, _ string) error {
		if ctx.Err() != nil {
			t.Fatalf("fallback close used a cancelled context: %v", ctx.Err())
		}
		return errors.New("close rejected")
	}).AnyTimes()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	o.Run(ctx, time.Now())

	record, ok := m.recorder.last()
	if !ok {
		t.Fatal("expected cycle record")
	}
	if record.Outcome != domain.OutcomeAborted {
		t.Fatalf("expected aborted outcome, got %s", record.Outcome)
	}
	if !strings.Contains(record.AbortReason, "critical_close_failed") {
		t.Fatalf("expected critical close abort reason, got %q", record.AbortReason)
	}
	assertAbortContext(t, record, events.FlowReversion, events.TopicReversionAbort)
	assertErrorContext(t, record, events.FlowReversion, events.TopicReversionError)
	assertTimelineHasTopics(t, record, events.TopicReversionAbort, events.TopicReversionError)
	assertTimelineMissingTopics(t, record, events.TopicReversionPositionClosed)
}

func TestCycleOrchestrator_TrapTrailingCloseFailureUsesTrapAbortTopic(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	cfg := config.SymbolConfig{
		Symbol:         "BTC_USDT",
		MinFundingRate: 0.001,
		MarginUSDT:     100,
		Leverage:       1,
		FundingReversion: domain.FundingReversionConfig{
			Trailing: domain.TrailingConfig{Enabled: false},
		},
		FundingTrap: domain.FundingTrapConfig{
			Enabled:   true,
			DepthPct:  0.01,
			SizeRatio: 0.5,
			Trailing:  domain.TrailingConfig{Enabled: true},
		},
	}
	o, m := setupOrchestratorWithConfig(t, ctrl, cfg)

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
	m.subscriber.EXPECT().SubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil).AnyTimes()
	m.priceStore.EXPECT().GetPrice(gomock.Any(), "BTC_USDT", gomock.Any()).Return(&store.PriceData{
		BestBid: 49999, BestAsk: 50001, LastPrice: 50000,
	}, nil).AnyTimes()
	m.clock.EXPECT().Until(gomock.Any()).Return(time.Duration(0)).AnyTimes()
	m.clock.EXPECT().LatencyMs().Return(int64(50)).AnyTimes()
	m.clock.EXPECT().Now().Return(time.Now()).AnyTimes()
	m.clock.EXPECT().GetServerTime().Return(time.Now().UnixMilli()).AnyTimes()
	m.clock.EXPECT().Offset().Return(int64(0)).AnyTimes()

	m.client.EXPECT().CreateOrder(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, req exchange.SubmitOrderRequest) (string, error) {
		if strings.HasPrefix(req.ExternalOID, "trp_") {
			return "trap_1", nil
		}
		return "ioc_1", nil
	}).AnyTimes()
	m.ws.EXPECT().OnOrderUpdate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, orderID string, _ time.Duration, callback func(exchange.WsOrderDeal)) {
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
	m.client.EXPECT().CreateTrackOrder(gomock.Any(), gomock.Any()).Return("", errors.New("API failure tracking"))
	m.client.EXPECT().
		ClosePosition(gomock.Any(), "BTC_USDT", shared.SideCloseShort, 1.0, gomock.Any()).
		Return(errors.New("exact close rejected"))
	m.client.EXPECT().CloseAllPositions(gomock.Any(), "BTC_USDT").Return(errors.New("close rejected"))

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	o.Run(ctx, time.Now())

	record, ok := m.recorder.last()
	if !ok {
		t.Fatal("expected cycle record")
	}
	if record.Outcome != domain.OutcomeAborted {
		t.Fatalf("expected aborted outcome, got %s", record.Outcome)
	}
	if !strings.Contains(record.AbortReason, "critical_close_failed") {
		t.Fatalf("expected critical close abort reason, got %q", record.AbortReason)
	}
	if !hasTimelineTopic(record, events.TopicTrapAbort) || !hasTimelineTopic(record, events.TopicTrapError) {
		t.Fatal("expected trap abort and trap error topics")
	}
	if hasTimelineTopic(record, events.TopicReversionAbort) {
		t.Fatal("trap trailing close failure must not publish reversion abort")
	}
}

func TestCycleOrchestrator_TrapPositionClosedDoesNotTerminateReversionCycle(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	cfg := config.SymbolConfig{
		Symbol:         "BTC_USDT",
		MinFundingRate: 0.001,
		MarginUSDT:     100,
		Leverage:       1,
		FundingReversion: domain.FundingReversionConfig{
			Trailing: domain.TrailingConfig{Enabled: false},
		},
		FundingTrap: domain.FundingTrapConfig{
			Enabled:   true,
			DepthPct:  0.01,
			SizeRatio: 0.5,
			Trailing:  domain.TrailingConfig{Enabled: true},
		},
	}
	o, m := setupOrchestratorWithConfig(t, ctrl, cfg)

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
	m.subscriber.EXPECT().SubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil).AnyTimes()
	m.priceStore.EXPECT().GetPrice(gomock.Any(), "BTC_USDT", gomock.Any()).Return(&store.PriceData{
		BestBid: 49999, BestAsk: 50001, LastPrice: 50000,
	}, nil).AnyTimes()
	m.clock.EXPECT().Until(gomock.Any()).Return(time.Duration(0)).AnyTimes()
	m.clock.EXPECT().LatencyMs().Return(int64(50)).AnyTimes()
	m.clock.EXPECT().Now().Return(time.Now()).AnyTimes()
	m.clock.EXPECT().GetServerTime().Return(time.Now().UnixMilli()).AnyTimes()
	m.clock.EXPECT().Offset().Return(int64(0)).AnyTimes()

	m.client.EXPECT().CreateOrder(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, req exchange.SubmitOrderRequest) (string, error) {
		if strings.HasPrefix(req.ExternalOID, "trp_") {
			return "trap_1", nil
		}
		return "ioc_1", nil
	}).AnyTimes()
	m.ws.EXPECT().OnOrderUpdate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, orderID string, _ time.Duration, callback func(exchange.WsOrderDeal)) {
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
	m.client.EXPECT().CreateTrackOrder(gomock.Any(), gomock.Any()).Return("", errors.New("API failure tracking"))
	m.client.EXPECT().
		ClosePosition(gomock.Any(), "BTC_USDT", shared.SideCloseShort, 1.0, gomock.Any()).
		Return(nil)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	o.Run(ctx, time.Now())

	record, ok := m.recorder.last()
	if !ok {
		t.Fatal("expected cycle record")
	}
	if !hasTimelineTopic(record, events.TopicTrapPositionClosed) {
		t.Fatal("expected trap position_closed topic")
	}
	if record.Trap.Outcome != domain.TrapOutcomeClosed {
		t.Fatalf("expected closed trap outcome, got %q", record.Trap.Outcome)
	}
	if record.Cleanup.TerminalTopic == events.TopicTrapPositionClosed {
		t.Fatalf("trap position_closed must not cleanup the whole cycle: %+v", record.Cleanup)
	}
	if hasTimelineTopic(record, events.TopicReversionAbort) || hasTimelineTopic(record, events.TopicTrapAbort) {
		t.Fatal("successful fallback close must not publish abort")
	}
}

func TestCycleOrchestrator_CriticalCloseFailureAfterTimeout(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	cfg := config.SymbolConfig{
		Symbol:         "BTC_USDT",
		MinFundingRate: 0.001,
		FundingReversion: domain.FundingReversionConfig{
			PostSettleTimeout: types.Duration(1 * time.Millisecond),
			Trailing:          domain.TrailingConfig{Enabled: false},
		},
		FundingTrap: domain.FundingTrapConfig{
			Enabled: false,
		},
	}
	o, m := setupOrchestratorWithConfig(t, ctrl, cfg)

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
	m.subscriber.EXPECT().SubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil).AnyTimes()
	m.priceStore.EXPECT().GetPrice(gomock.Any(), "BTC_USDT", gomock.Any()).Return(&store.PriceData{
		BestBid: 49999, BestAsk: 50001, LastPrice: 50000,
	}, nil).AnyTimes()
	m.clock.EXPECT().Until(gomock.Any()).Return(time.Duration(0)).AnyTimes()
	m.clock.EXPECT().LatencyMs().Return(int64(50)).AnyTimes()
	m.clock.EXPECT().Now().Return(time.Now()).AnyTimes()
	m.clock.EXPECT().GetServerTime().Return(time.Now().UnixMilli()).AnyTimes()
	m.clock.EXPECT().Offset().Return(int64(0)).AnyTimes()

	m.client.EXPECT().CreateOrder(gomock.Any(), gomock.Any()).Return("ioc_1", nil).AnyTimes()
	m.ws.EXPECT().OnOrderUpdate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, orderID string, duration time.Duration, callback func(exchange.WsOrderDeal)) {
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
	m.client.EXPECT().CloseAllPositions(gomock.Any(), "BTC_USDT").DoAndReturn(func(ctx context.Context, _ string) error {
		if ctx.Err() != nil {
			t.Fatalf("timeout close used a cancelled context: %v", ctx.Err())
		}
		return errors.New("close rejected")
	}).AnyTimes()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	o.Run(ctx, time.Now())

	record, ok := m.recorder.last()
	if !ok {
		t.Fatal("expected cycle record")
	}
	if record.Outcome != domain.OutcomeAborted {
		t.Fatalf("expected aborted outcome, got %s", record.Outcome)
	}
	if !strings.Contains(record.AbortReason, "critical_timeout_close_failed") {
		t.Fatalf("expected critical timeout close abort reason, got %q", record.AbortReason)
	}
	assertAbortContext(t, record, events.FlowReversion, events.TopicReversionAbort)
	assertErrorContext(t, record, events.FlowReversion, events.TopicReversionError)
	if record.Timeout.Flow != events.FlowReversion || !record.Timeout.Triggered {
		t.Fatalf("expected triggered reversion timeout snapshot, got %+v", record.Timeout)
	}
	if !record.Timeout.ForceCloseAttempted || record.Timeout.ForceCloseSucceeded {
		t.Fatalf("expected failed timeout force-close snapshot, got %+v", record.Timeout)
	}
	assertTimelineHasTopics(t, record, events.TopicReversionAbort, events.TopicReversionError)
	assertTimelineMissingTopics(t, record, events.TopicReversionTimeout)
}

func TestCycleOrchestrator_CancelsUnfilledTrapOrderAfterTimeout(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t, gomock.WithOverridableExpectations())
	cfg := config.SymbolConfig{
		Symbol:         "BTC_USDT",
		MinFundingRate: 0.001,
		MarginUSDT:     100,
		Leverage:       1,
		FundingReversion: domain.FundingReversionConfig{
			PostSettleTimeout: types.Duration(20 * time.Millisecond),
			Trailing:          domain.TrailingConfig{Enabled: true},
		},
		FundingTrap: domain.FundingTrapConfig{
			Enabled:           true,
			DepthPct:          0.01,
			SizeRatio:         0.5,
			PostSettleTimeout: types.Duration(1 * time.Millisecond),
			Trailing:          domain.TrailingConfig{Enabled: true},
		},
	}
	o, m := setupOrchestratorWithConfig(t, ctrl, cfg)

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
	m.subscriber.EXPECT().SubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil).AnyTimes()
	m.priceStore.EXPECT().GetPrice(gomock.Any(), "BTC_USDT", gomock.Any()).Return(&store.PriceData{
		BestBid: 49999, BestAsk: 50001, LastPrice: 50000,
	}, nil).AnyTimes()
	m.clock.EXPECT().Until(gomock.Any()).Return(time.Duration(0)).AnyTimes()
	m.clock.EXPECT().LatencyMs().Return(int64(50)).AnyTimes()
	m.clock.EXPECT().Now().Return(time.Now()).AnyTimes()
	m.clock.EXPECT().GetServerTime().Return(time.Now().UnixMilli()).AnyTimes()
	m.clock.EXPECT().Offset().Return(int64(0)).AnyTimes()
	expectTrapThenReversionTimeoutSleep(
		m,
		time.Duration(cfg.FundingTrap.PostSettleTimeout),
		time.Duration(cfg.FundingReversion.PostSettleTimeout),
	)

	m.client.EXPECT().CreateOrder(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, req exchange.SubmitOrderRequest) (string, error) {
		if strings.HasPrefix(req.ExternalOID, "trp_") {
			return "trap_1", nil
		}
		return "ioc_1", nil
	}).AnyTimes()
	m.ws.EXPECT().OnOrderUpdate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, orderID string, duration time.Duration, callback func(exchange.WsOrderDeal)) {
		if orderID != "ioc_1" {
			return
		}
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
	m.client.EXPECT().CreateTrackOrder(gomock.Any(), gomock.Any()).Return("track_1", nil).AnyTimes()
	m.client.EXPECT().CancelOrder(gomock.Any(), "BTC_USDT", "trap_1").Return(nil)
	m.client.EXPECT().CloseAllPositions(gomock.Any(), "BTC_USDT").Return(nil)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	o.Run(ctx, time.Now())

	record, ok := m.recorder.last()
	if !ok {
		t.Fatal("expected cycle record")
	}

	if !hasTimelineTopic(record, events.TopicTrapTimeout) {
		t.Fatal("expected trap timeout after canceling unfilled trap order")
	}
	if !hasTimelineTopic(record, events.TopicReversionTimeout) {
		t.Fatal("expected reversion timeout to terminate the cycle")
	}
	if record.Outcome != domain.OutcomeTimeout {
		t.Fatalf("expected reversion timeout to terminate cycle, got %s", record.Outcome)
	}
	if record.Exit.Reason != "timeout" {
		t.Fatalf("expected timeout exit reason, got %q", record.Exit.Reason)
	}
	if record.Trap.Filled {
		t.Fatal("trap should remain unfilled")
	}
	if record.Trap.Outcome != domain.TrapOutcomeTimeout {
		t.Fatalf("expected trap timeout outcome, got %q", record.Trap.Outcome)
	}
	if record.Timeout.Flow != events.FlowReversion || !record.Timeout.Triggered {
		t.Fatalf("expected triggered reversion timeout snapshot, got %+v", record.Timeout)
	}
	if record.Cleanup.TerminalTopic != events.TopicReversionTimeout || record.Cleanup.TerminalFlow != events.FlowReversion {
		t.Fatalf("expected reversion timeout cleanup, got %+v", record.Cleanup)
	}
}

func TestCycleOrchestrator_ReversionTimeoutCancelsOpenTrapBeforeCleanup(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t, gomock.WithOverridableExpectations())
	cfg := config.SymbolConfig{
		Symbol:         "BTC_USDT",
		MinFundingRate: 0.001,
		MarginUSDT:     100,
		Leverage:       1,
		FundingReversion: domain.FundingReversionConfig{
			PostSettleTimeout: types.Duration(30 * time.Millisecond),
			Trailing:          domain.TrailingConfig{Enabled: true},
		},
		FundingTrap: domain.FundingTrapConfig{
			Enabled:   true,
			DepthPct:  0.01,
			SizeRatio: 0.5,
			Trailing:  domain.TrailingConfig{Enabled: true},
		},
	}
	o, m := setupOrchestratorWithConfig(t, ctrl, cfg)

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
	m.subscriber.EXPECT().SubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil).AnyTimes()
	m.priceStore.EXPECT().GetPrice(gomock.Any(), "BTC_USDT", gomock.Any()).Return(&store.PriceData{
		BestBid: 49999, BestAsk: 50001, LastPrice: 50000,
	}, nil).AnyTimes()
	m.clock.EXPECT().Until(gomock.Any()).Return(time.Duration(0)).AnyTimes()
	m.clock.EXPECT().LatencyMs().Return(int64(50)).AnyTimes()
	m.clock.EXPECT().Now().Return(time.Now()).AnyTimes()
	m.clock.EXPECT().GetServerTime().Return(time.Now().UnixMilli()).AnyTimes()
	m.clock.EXPECT().Offset().Return(int64(0)).AnyTimes()
	expectDelayedReversionTimeoutSleep(m, time.Duration(cfg.FundingReversion.PostSettleTimeout))

	m.client.EXPECT().CreateOrder(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, req exchange.SubmitOrderRequest) (string, error) {
		if strings.HasPrefix(req.ExternalOID, "trp_") {
			return "trap_1", nil
		}
		return "ioc_1", nil
	}).AnyTimes()
	m.ws.EXPECT().OnOrderUpdate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, orderID string, _ time.Duration, callback func(exchange.WsOrderDeal)) {
		if orderID != "ioc_1" {
			return
		}
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
	m.client.EXPECT().CreateTrackOrder(gomock.Any(), gomock.Any()).Return("track_1", nil).AnyTimes()
	m.client.EXPECT().CloseAllPositions(gomock.Any(), "BTC_USDT").Return(nil)
	m.client.EXPECT().CancelOrder(gomock.Any(), "BTC_USDT", "trap_1").Return(nil)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	o.Run(ctx, time.Now())

	record, ok := m.recorder.last()
	if !ok {
		t.Fatal("expected cycle record")
	}
	if record.Outcome != domain.OutcomeTimeout {
		t.Fatalf("expected reversion timeout outcome, got %s", record.Outcome)
	}
	if record.Trap.Outcome != domain.TrapOutcomeTimeout {
		t.Fatalf("expected cleanup to mark trap timeout, got %q", record.Trap.Outcome)
	}
	assertTimelineHasTopics(t, record, events.TopicReversionTimeout, events.TopicTrapTimeout)
	if record.Cleanup.TerminalTopic != events.TopicReversionTimeout || record.Cleanup.TerminalFlow != events.FlowReversion {
		t.Fatalf("expected reversion cleanup after canceling trap order, got %+v", record.Cleanup)
	}
}

func TestCycleOrchestrator_TrapCancelFailureUsesTrapAbortTopic(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	cfg := config.SymbolConfig{
		Symbol:         "BTC_USDT",
		MinFundingRate: 0.001,
		MarginUSDT:     100,
		Leverage:       1,
		FundingReversion: domain.FundingReversionConfig{
			Trailing: domain.TrailingConfig{Enabled: true},
		},
		FundingTrap: domain.FundingTrapConfig{
			Enabled:           true,
			DepthPct:          0.01,
			SizeRatio:         0.5,
			PostSettleTimeout: types.Duration(1 * time.Millisecond),
			Trailing:          domain.TrailingConfig{Enabled: true},
		},
	}
	o, m := setupOrchestratorWithConfig(t, ctrl, cfg)

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
	m.subscriber.EXPECT().SubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil).AnyTimes()
	m.priceStore.EXPECT().GetPrice(gomock.Any(), "BTC_USDT", gomock.Any()).Return(&store.PriceData{
		BestBid: 49999, BestAsk: 50001, LastPrice: 50000,
	}, nil).AnyTimes()
	m.clock.EXPECT().Until(gomock.Any()).Return(time.Duration(0)).AnyTimes()
	m.clock.EXPECT().LatencyMs().Return(int64(50)).AnyTimes()
	m.clock.EXPECT().Now().Return(time.Now()).AnyTimes()
	m.clock.EXPECT().GetServerTime().Return(time.Now().UnixMilli()).AnyTimes()
	m.clock.EXPECT().Offset().Return(int64(0)).AnyTimes()

	m.client.EXPECT().CreateOrder(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, req exchange.SubmitOrderRequest) (string, error) {
		if strings.HasPrefix(req.ExternalOID, "trp_") {
			return "trap_1", nil
		}
		return "ioc_1", nil
	}).AnyTimes()
	m.ws.EXPECT().OnOrderUpdate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, orderID string, _ time.Duration, callback func(exchange.WsOrderDeal)) {
		if orderID != "ioc_1" {
			return
		}
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
	m.client.EXPECT().CreateTrackOrder(gomock.Any(), gomock.Any()).Return("track_1", nil).AnyTimes()
	m.client.EXPECT().CancelOrder(gomock.Any(), "BTC_USDT", "trap_1").Return(errors.New("cancel rejected"))
	m.client.EXPECT().CancelAllOpenOrders(gomock.Any(), "BTC_USDT").Return(errors.New("cancel all rejected"))

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	o.Run(ctx, time.Now())

	record, ok := m.recorder.last()
	if !ok {
		t.Fatal("expected cycle record")
	}
	if record.Outcome != domain.OutcomeAborted {
		t.Fatalf("expected aborted outcome, got %s", record.Outcome)
	}
	if !strings.Contains(record.AbortReason, "critical_trap_cancel_failed") {
		t.Fatalf("expected critical trap cancel abort reason, got %q", record.AbortReason)
	}
	assertAbortContext(t, record, events.FlowTrap, events.TopicTrapAbort)
	assertErrorContext(t, record, events.FlowTrap, events.TopicTrapError)
	if record.Timeout.Flow != events.FlowTrap || !record.Timeout.Triggered || record.Timeout.Error == "" {
		t.Fatalf("expected failed trap timeout snapshot, got %+v", record.Timeout)
	}
	assertTimelineHasTopics(t, record, events.TopicTrapAbort, events.TopicTrapError)
	assertTimelineMissingTopics(t, record, events.TopicReversionAbort)
}

func TestCycleOrchestrator_OBTrapUsesOriginalReversionSideForWallVerification(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t, gomock.WithOverridableExpectations())
	cfg := config.SymbolConfig{
		Symbol:         "BTC_USDT",
		MinFundingRate: 0.001,
		MarginUSDT:     100,
		Leverage:       1,
		FundingReversion: domain.FundingReversionConfig{
			Trailing: domain.TrailingConfig{Enabled: false},
		},
		FundingTrap: domain.FundingTrapConfig{
			Enabled:         true,
			DepthPct:        0.01,
			SizeRatio:       0.5,
			TrapAfterSettle: types.Duration(time.Millisecond),
			Trailing:        domain.TrailingConfig{Enabled: true},
		},
	}
	o, m := setupOrchestratorWithConfig(t, ctrl, cfg)

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
	m.subscriber.EXPECT().SubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil).AnyTimes()
	m.priceStore.EXPECT().GetPrice(gomock.Any(), "BTC_USDT", gomock.Any()).Return(&store.PriceData{
		BestBid: 49999, BestAsk: 50001, LastPrice: 50000,
	}, nil).AnyTimes()
	m.clock.EXPECT().Until(gomock.Any()).Return(time.Duration(0)).AnyTimes()
	m.clock.EXPECT().LatencyMs().Return(int64(50)).AnyTimes()
	m.clock.EXPECT().Now().Return(time.Now()).AnyTimes()
	m.clock.EXPECT().GetServerTime().Return(time.Now().UnixMilli()).AnyTimes()
	m.clock.EXPECT().Offset().Return(int64(0)).AnyTimes()
	m.depthStore.EXPECT().GetDepth(gomock.Any(), "BTC_USDT").Return(&shared.OrderBook{
		Symbol: "BTC_USDT",
		Asks: []shared.OrderBookEntry{
			{Price: 50500, Volume: 1},
			{Price: 50800, Volume: 1},
			{Price: 51000, Volume: 1000},
			{Price: 51200, Volume: 1},
		},
	}, nil).AnyTimes()

	var trapMu sync.Mutex
	trapOrderSeen := false
	trapOrderProblem := ""
	m.client.EXPECT().CreateOrder(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, req exchange.SubmitOrderRequest) (string, error) {
		if strings.HasPrefix(req.ExternalOID, "trp_ob_") {
			trapMu.Lock()
			defer trapMu.Unlock()
			trapOrderSeen = true
			if req.Side != int(shared.SideOpenShort) {
				trapOrderProblem = "OB trap used wrong side"
			}
			if req.Price < 50999.89 || req.Price > 50999.91 {
				trapOrderProblem = "OB trap used wrong price"
			}
			return "trap_1", nil
		}
		return "ioc_1", nil
	}).AnyTimes()
	m.ws.EXPECT().OnOrderUpdate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	m.ws.EXPECT().RemoveOrderCallback(gomock.Any()).AnyTimes()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	o.Run(ctx, time.Now())

	trapMu.Lock()
	seen := trapOrderSeen
	problem := trapOrderProblem
	trapMu.Unlock()
	if problem != "" {
		t.Fatal(problem)
	}
	if !seen {
		t.Fatal("expected OB trap order to be placed from ask wall")
	}
	record, ok := m.recorder.last()
	if !ok {
		t.Fatal("expected cycle record")
	}
	if record.Trap.Outcome != domain.TrapOutcomePlaced {
		t.Fatalf("trap outcome = %q, want %q", record.Trap.Outcome, domain.TrapOutcomePlaced)
	}
	if record.Trap.Source != "ob_monitor" {
		t.Fatalf("trap source = %q, want ob_monitor", record.Trap.Source)
	}
}

func TestCycleOrchestrator_JournalsTrapSkipBeforePlacement(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	cfg := config.SymbolConfig{
		Symbol:         "BTC_USDT",
		MinFundingRate: 0.001,
		MarginUSDT:     100,
		Leverage:       1,
		FundingReversion: domain.FundingReversionConfig{
			Trailing: domain.TrailingConfig{Enabled: false},
		},
		FundingTrap: domain.FundingTrapConfig{
			Enabled:         true,
			DepthPct:        -0.01,
			SizeRatio:       0.5,
			TrapAfterSettle: types.Duration(time.Millisecond),
			Trailing:        domain.TrailingConfig{Enabled: true},
		},
	}
	o, m := setupOrchestratorWithConfig(t, ctrl, cfg)

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
	m.subscriber.EXPECT().SubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil).AnyTimes()
	m.priceStore.EXPECT().GetPrice(gomock.Any(), "BTC_USDT", gomock.Any()).Return(&store.PriceData{
		BestBid: 49999, BestAsk: 50001, LastPrice: 50000,
	}, nil).AnyTimes()
	m.clock.EXPECT().Until(gomock.Any()).Return(time.Duration(0)).AnyTimes()
	m.clock.EXPECT().LatencyMs().Return(int64(50)).AnyTimes()
	m.clock.EXPECT().Now().Return(time.Now()).AnyTimes()
	m.clock.EXPECT().GetServerTime().Return(time.Now().UnixMilli()).AnyTimes()
	m.clock.EXPECT().Offset().Return(int64(0)).AnyTimes()
	var orderMu sync.Mutex
	unexpectedTrapOrder := false
	m.client.EXPECT().CreateOrder(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, req exchange.SubmitOrderRequest) (string, error) {
		if strings.HasPrefix(req.ExternalOID, "trp_") {
			orderMu.Lock()
			unexpectedTrapOrder = true
			orderMu.Unlock()
		}
		return "ioc_1", nil
	}).AnyTimes()
	m.ws.EXPECT().OnOrderUpdate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	m.ws.EXPECT().RemoveOrderCallback(gomock.Any()).AnyTimes()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	o.Run(ctx, time.Now())

	orderMu.Lock()
	trapOrderPlaced := unexpectedTrapOrder
	orderMu.Unlock()
	if trapOrderPlaced {
		t.Fatal("trap order should not be placed when static trap price is invalid")
	}

	record, ok := m.recorder.last()
	if !ok {
		t.Fatal("expected cycle record")
	}
	if record.Trap.Outcome != domain.TrapOutcomeSkipped {
		t.Fatalf("trap outcome = %q, want %q", record.Trap.Outcome, domain.TrapOutcomeSkipped)
	}
	if record.Trap.SkipReason != domain.TrapSkipReasonInvalidPrice {
		t.Fatalf("trap skip reason = %q, want %q", record.Trap.SkipReason, domain.TrapSkipReasonInvalidPrice)
	}
	assertTimelineHasTopics(t, record, events.TopicTrapSkipped)
	assertTimelineMissingTopics(t, record, events.TopicTrapOrderPlaced)
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
	m.subscriber.EXPECT().SubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil).AnyTimes()
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
	m.ws.EXPECT().OnOrderUpdate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, orderID string, duration time.Duration, callback func(exchange.WsOrderDeal)) {
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
	m.client.EXPECT().CreateTrackOrder(gomock.Any(), gomock.Any()).Return("track_1", nil).AnyTimes()
	m.subscriber.EXPECT().UnsubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil).AnyTimes()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	o.Run(ctx, time.Now())
}

func TestCycleOrchestrator_EventDrivenExcursionAndFlowTopics(t *testing.T) {
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
	m.subscriber.EXPECT().SubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil).AnyTimes()
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
	m.ws.EXPECT().OnOrderUpdate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, orderID string, _ time.Duration, callback func(exchange.WsOrderDeal)) {
		go func() {
			time.Sleep(10 * time.Millisecond)
			callback(exchange.WsOrderDeal{
				OrderID:      orderID,
				State:        exchange.OrderStateFilled,
				DealVol:      1.0,
				DealAvgPrice: 50000,
			})
			time.Sleep(10 * time.Millisecond)
			m.priceUpdates <- &store.PriceData{Symbol: "BTC_USDT", LastPrice: 51000, UpdatedAt: time.Now()}
			m.priceUpdates <- &store.PriceData{Symbol: "BTC_USDT", LastPrice: 49000, UpdatedAt: time.Now()}
		}()
	}).AnyTimes()
	m.ws.EXPECT().RemoveOrderCallback(gomock.Any()).AnyTimes()
	m.client.EXPECT().CreateTrackOrder(gomock.Any(), gomock.Any()).Return("track_1", nil).AnyTimes()

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	o.Run(ctx, time.Now())

	record, ok := m.recorder.last()
	if !ok {
		t.Fatal("expected cycle record")
	}
	if record.SchemaVersion != 2 {
		t.Fatalf("schema_version = %d, want 2", record.SchemaVersion)
	}
	if record.IOC.Flow != events.FlowReversion {
		t.Fatalf("ioc flow = %q, want %q", record.IOC.Flow, events.FlowReversion)
	}
	if record.Excursion.MFEPct < 1.9 || record.Excursion.MAEPct < 1.9 {
		t.Fatalf("expected MFE/MAE from price stream, got mfe=%v mae=%v", record.Excursion.MFEPct, record.Excursion.MAEPct)
	}
	for _, entry := range record.Timeline {
		if strings.HasPrefix(entry.Topic, "cycle.") {
			t.Fatalf("unexpected generic cycle topic in timeline: %s", entry.Topic)
		}
	}
}
