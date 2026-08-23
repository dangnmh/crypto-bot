package ordermanager_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	shared "crypto-bot/internal/domain"
	app "crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/notifier"
	"crypto-bot/internal/infrastructure/timesync"
	"crypto-bot/internal/trading/ordermanager"
	"crypto-bot/pkg/eventbus"
)

// Blackbox mock exchange client implementing exchange API interfaces.
type blackboxExchangeClient struct {
	exchange.UnimplementedClient
	mu                 sync.Mutex
	createOrderErr     error
	getOrderErr        error
	closeAllErr        error
	closePositionErr   error
	orderState         shared.OrderState
	dealVol            float64
	openPositions      []exchange.Position
	createOrderCalls   atomic.Int32
	closePositionCalls atomic.Int32
	closeAllCalls      atomic.Int32
}

func (m *blackboxExchangeClient) SwitchMarginMode(ctx context.Context, symbol string, mode shared.MarginMode, leverage int, side shared.Side) error {
	return nil
}

func (m *blackboxExchangeClient) SwitchPositionMode(ctx context.Context, symbol string, mode shared.PositionMode) error {
	return nil
}

func (m *blackboxExchangeClient) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	return nil
}

func (m *blackboxExchangeClient) SupportLeverageOnOrder() bool {
	return false
}

func (m *blackboxExchangeClient) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
	m.createOrderCalls.Add(1)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createOrderErr != nil {
		return exchange.CreateOrderResult{}, m.createOrderErr
	}
	orderID := fmt.Sprintf("ex-order-%s", req.ExternalOID)
	if req.ExternalOID == "" {
		orderID = "ex-order-default"
	}
	return exchange.CreateOrderResult{OrderID: orderID, TPSLSubmitted: false}, nil
}

func (m *blackboxExchangeClient) PlaceTPSL(ctx context.Context, req exchange.TPSLRequest) error {
	return nil
}

func (m *blackboxExchangeClient) CancelOrder(ctx context.Context, symbol, orderID string) error {
	return nil
}

func (m *blackboxExchangeClient) ClosePosition(ctx context.Context, symbol string, side shared.Side, volume float64, positionMode shared.PositionMode, leverage int) error {
	m.closePositionCalls.Add(1)
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closePositionErr
}

func (m *blackboxExchangeClient) CloseAllPositions(ctx context.Context, symbol string) error {
	m.closeAllCalls.Add(1)
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closeAllErr
}

func (m *blackboxExchangeClient) GetOrder(ctx context.Context, symbol, orderID string) (*exchange.OrderInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getOrderErr != nil {
		return nil, m.getOrderErr
	}
	st := m.orderState
	if st == 0 {
		st = exchange.OrderStateFilled
	}
	vol := m.dealVol
	if vol <= 0 {
		vol = 1.0
	}
	return &exchange.OrderInfo{
		OrderID:      orderID,
		Symbol:       symbol,
		State:        st,
		DealVol:      vol,
		DealAvgPrice: 50000.0,
	}, nil
}

func (m *blackboxExchangeClient) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.openPositions != nil {
		return m.openPositions, nil
	}
	return []exchange.Position{
		{Symbol: symbol, HoldVolContract: 1.0},
	}, nil
}

// Blackbox mock trade repository recording saved events.
type blackboxTradeRepo struct {
	mu          sync.Mutex
	savedEvents []ordermanager.OrderTradeRecordEvent
}

func (r *blackboxTradeRepo) Save(ctx context.Context, evt ordermanager.OrderTradeRecordEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.savedEvents = append(r.savedEvents, evt)
	return nil
}

func (r *blackboxTradeRepo) SavedEvents() []ordermanager.OrderTradeRecordEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	copied := make([]ordermanager.OrderTradeRecordEvent, len(r.savedEvents))
	copy(copied, r.savedEvents)
	return copied
}

// Blackbox mock Telegram notifier recording sent events.
type blackboxNotifier struct {
	mu         sync.Mutex
	sentEvents []notifier.Event
}

func (n *blackboxNotifier) Start(ctx context.Context) error {
	return nil
}

func (n *blackboxNotifier) Stop(ctx context.Context) error {
	return nil
}

func (n *blackboxNotifier) Send(ctx context.Context, evt notifier.Event) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.sentEvents = append(n.sentEvents, evt)
	return nil
}

func (n *blackboxNotifier) SentEvents() []notifier.Event {
	n.mu.Lock()
	defer n.mu.Unlock()
	copied := make([]notifier.Event, len(n.sentEvents))
	copy(copied, n.sentEvents)
	return copied
}

func newTestTimeSync(client exchange.Client) *timesync.TimeSync {
	ts := timesync.New(client, slog.Default(), time.Second)
	ts.SetSleeper(func(ctx context.Context, d time.Duration) error {
		return nil
	})
	return ts
}

// Test 1: Full End-to-End Blackbox Order Lifecycle Pipeline.
func TestBlackBox_CompleteOrderLifecycle(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := &blackboxExchangeClient{
		orderState: exchange.OrderStateFilled,
		dealVol:    1.0,
	}
	bus := eventbus.New(slog.Default())
	clock := mockClock{}
	repo := &blackboxTradeRepo{}
	noti := &blackboxNotifier{}
	engine := &app.Engine{
		Bus: bus,
		Providers: map[string]*app.ExchangeProvider{
			"bybit": {
				Name:     "bybit",
				Client:   client,
				TimeSync: newTestTimeSync(client),
			},
			"mexc": {
				Name:     "mexc",
				Client:   client,
				TimeSync: newTestTimeSync(client),
			},
		},
	}
	mgr, err := ordermanager.NewOrderManager(context.Background(), engine, bus, repo, noti, slog.Default())
	if err != nil {
		t.Fatalf("failed to create order manager: %v", err)
	}

	if err := mgr.Init(ctx); err != nil {
		t.Fatalf("failed to init order manager: %v", err)
	}

	intent := ordermanager.OrderIntentEvent{
		ReqID:         "req-bb-001",
		ClientOrderID: "client-bb-001",
		Symbol:        "BTCUSDT",
		Exchange:      "bybit",
		StrategyType:  ordermanager.StrategyFundingReversion,
		Timestamp:     clock.Now(),
		Side:          shared.SideOpenLong,
		OrderType:     ordermanager.OrderTypeIOC,
		Price:         50000.0,
		Volume:        1.0,
		ContractSize:  1.0,
		MarginMode:    shared.MarginModeCross,
		PositionMode:  shared.PositionModeOneWay,
		Leverage:      10,
	}

	if err := mgr.Dispatch(ctx, intent); err != nil {
		t.Fatalf("failed to dispatch intent: %v", err)
	}

	// Wait briefly for order submission & outcome resolution to open position
	time.Sleep(50 * time.Millisecond)

	// Emit real-time WS position update closing position
	mgr.HandlePositionUpdate(ctx, "req-bb-001", exchange.PersonalPositionUpdate{
		Symbol:           "BTCUSDT",
		HoldVolContract:  0.0,
		CloseVolContract: 1.0,
		OpenAvgPrice:     50000.0,
		CloseAvgPrice:    51000.0,
		CloseProfitLoss:  1000.0,
		Fee:              20.0,
	})

	// Wait for eventbus async subscriber pipeline to complete
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		agg := mgr.GetAggregate("req-bb-001")
		if len(repo.SavedEvents()) > 0 && agg != nil && agg.State() == ordermanager.StateCompleted {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	assertCompleteOrderLifecycle(t, mgr, repo, noti)
}

func assertCompleteOrderLifecycle(t *testing.T, mgr *ordermanager.OrderManager, repo *blackboxTradeRepo, noti *blackboxNotifier) {
	t.Helper()
	// Verify DB Trade Persistence
	saved := repo.SavedEvents()
	if len(saved) == 0 {
		t.Fatalf("expected trade record to be saved to repository")
	}

	trade := saved[0]
	if trade.ReqID != "req-bb-001" {
		t.Errorf("expected ReqID req-bb-001, got %s", trade.ReqID)
	}
	if trade.ClientOrderID != "client-bb-001" {
		t.Errorf("expected ClientOrderID client-bb-001, got %s", trade.ClientOrderID)
	}
	if trade.MarketType != string(ordermanager.MarketTypeFuture) {
		t.Errorf("expected MarketType FUTURE, got %s", trade.MarketType)
	}
	exOrderID, found := mgr.GetExchangeOrderIDByReqID(trade.ReqID)

	if !found || exOrderID != "ex-order-client-bb-001" {
		t.Errorf("expected cached ExchangeOrderID ex-order-client-bb-001, got %s", exOrderID)
	}

	if trade.Outcome != string(ordermanager.OutcomeFilled) {
		t.Errorf("expected Outcome filled, got %s", trade.Outcome)
	}
	if trade.NetPnL != 980.0 {
		t.Errorf("expected NetPnL 980.0, got %.2f", trade.NetPnL)
	}

	// Verify Telegram Notification Sent
	notifications := noti.SentEvents()
	if len(notifications) == 0 {
		t.Errorf("expected Telegram notification events to be sent")
	}

	// Verify Aggregate State Invariants
	agg := mgr.GetAggregate("req-bb-001")
	if agg.State() != ordermanager.StateCompleted {
		t.Errorf("expected aggregate state StateCompleted, got %s", agg.State())
	}
}

func TestBlackBox_OutcomeWatcherFill(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := &blackboxExchangeClient{
		orderState: exchange.OrderStateFilled,
		dealVol:    2.0,
	}
	bus := eventbus.New(slog.Default())
	clock := mockClock{}
	repo := &blackboxTradeRepo{}
	noti := &blackboxNotifier{}
	engine := &app.Engine{
		Bus: bus,
		Providers: map[string]*app.ExchangeProvider{
			"mexc": {
				Name:     "mexc",
				Client:   client,
				TimeSync: newTestTimeSync(client),
			},
		},
	}
	mgr, err := ordermanager.NewOrderManager(context.Background(), engine, bus, repo, noti, slog.Default())
	if err != nil {
		t.Fatalf("failed to create order manager: %v", err)
	}
	if err := mgr.Init(context.Background()); err != nil {
		t.Fatalf("failed to init order manager: %v", err)
	}

	submittedEvt := ordermanager.OrderSubmittedEvent{
		ReqID:         "req-ws-001",
		ClientOrderID: "client-ws-001",
		Symbol:        "ETHUSDT",
		Exchange:      "mexc",
		StrategyType:  ordermanager.StrategyFundingReversion,
		Timestamp:     clock.Now(),
		Price:         3000.0,
		Volume:        2.0,
		SubmittedAt:   clock.Now(),
	}
	_ = mgr.GetAggregate(submittedEvt.GetReqID()).Record(submittedEvt)

	mgr.SetExchangeOrderIDByReqID(submittedEvt.GetReqID(), "ex-order-client-ws-001")

	resolved, err := mgr.HandleOutcomeWatcher(ctx, submittedEvt)
	if err != nil {
		t.Fatalf("HandleOutcomeWatcher failed: %v", err)
	}

	if resolved.Outcome != ordermanager.OutcomeFilled {
		t.Errorf("expected OutcomeFilled, got %s", resolved.Outcome)
	}
	if resolved.FilledVol != 2.0 {
		t.Errorf("expected FilledVol 2.0, got %.2f", resolved.FilledVol)
	}
}

// Test 3: Emergency Bailout Retry Loop & Side Mapping.
func TestBlackBox_EmergencyBailoutRetryLoop(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := &blackboxExchangeClient{
		closeAllErr: errors.New("close all positions failed"),
	}

	bus := eventbus.New(slog.Default())
	repo := &blackboxTradeRepo{}
	noti := &blackboxNotifier{}
	engine := &app.Engine{
		Bus: bus,
		Providers: map[string]*app.ExchangeProvider{
			"bybit": {
				Name:     "bybit",
				Client:   client,
				TimeSync: newTestTimeSync(client),
			},
			"mexc": {
				Name:     "mexc",
				Client:   client,
				TimeSync: newTestTimeSync(client),
			},
		},
	}
	mgr, err := ordermanager.NewOrderManager(ctx, engine, bus, repo, noti, slog.Default())
	if err != nil {
		t.Fatalf("failed to create order manager: %v", err)
	}

	bailoutEvt, err := mgr.HandleExecuteBailout(ctx, "req-bailout-bb-001", "bybit", "BTCUSDT", shared.SideOpenLong, 1.5, "timeout_expired")
	if err != nil {
		t.Fatalf("HandleExecuteBailout failed: %v", err)
	}

	if client.closeAllCalls.Load() != 1 {
		t.Errorf("expected 1 CloseAllPositions call, got %d", client.closeAllCalls.Load())
	}
	if client.closePositionCalls.Load() != 1 {
		t.Errorf("expected 1 ClosePosition retry call, got %d", client.closePositionCalls.Load())
	}
	if bailoutEvt.CloseRetryCount != 1 {
		t.Errorf("expected CloseRetryCount 1, got %d", bailoutEvt.CloseRetryCount)
	}
}

// Test 4: Custom Contract Size Notional USD Calculation.
func TestBlackBox_ContractSizeNotionalUSD(t *testing.T) {
	t.Parallel()

	agg := ordermanager.NewOrderExecutionAggregate("req-cs-001")

	intent := ordermanager.OrderIntentEvent{
		ReqID:        "req-cs-001",
		Symbol:       "BTCUSDT",
		StrategyType: ordermanager.StrategyFundingArbitrage,
		Side:         shared.SideOpenLong,
		OrderType:    ordermanager.OrderTypeLimit,
		Volume:       5.0,
		ContractSize: 10.0, // 10x Contract Multiplier
	}
	_ = agg.Record(intent)

	completed := ordermanager.OrderCompletedEvent{
		ReqID:            "req-cs-001",
		Symbol:           "BTCUSDT",
		StrategyType:     ordermanager.StrategyFundingArbitrage,
		Outcome:          ordermanager.OutcomeFilled,
		EntryPrice:       50000.0,
		ExitPrice:        52000.0,
		CloseVolContract: 5.0,
		ContractSize:     10.0,
		GrossProfit:      100000.0,
		NetProfit:        99000.0,
		CompletedAt:      time.Now(),
	}
	_ = agg.Record(completed)

	tradeRecord := agg.BuildTradeRecord()

	// Expected NotionalUSD = Volume (5.0) * ExitPrice (52000.0) * ContractSize (10.0) = 2,600,000.0
	expectedNotional := 5.0 * 52000.0 * 10.0
	if tradeRecord.NotionalUSD != expectedNotional {
		t.Errorf("expected NotionalUSD %.2f, got %.2f", expectedNotional, tradeRecord.NotionalUSD)
	}
	if tradeRecord.ContractSize != 10.0 {
		t.Errorf("expected ContractSize 10.0, got %.2f", tradeRecord.ContractSize)
	}
}

// Test 5: Timeout Guard Scheduling & Auto-Cancellation.
func TestBlackBox_TimeoutGuardCancellation(t *testing.T) {
	t.Parallel()

	client := &blackboxExchangeClient{}
	bus := eventbus.New(slog.Default())
	repo := &blackboxTradeRepo{}
	noti := &blackboxNotifier{}
	engine := &app.Engine{
		Bus: bus,
		Providers: map[string]*app.ExchangeProvider{
			"bybit": {
				Name:     "bybit",
				Client:   client,
				TimeSync: newTestTimeSync(client),
			},
			"mexc": {
				Name:     "mexc",
				Client:   client,
				TimeSync: newTestTimeSync(client),
			},
		},
	}
	mgr, err := ordermanager.NewOrderManager(context.Background(), engine, bus, repo, noti, slog.Default())
	if err != nil {
		t.Fatalf("failed to create order manager: %v", err)
	}

	agg := mgr.GetAggregate("req-tg-001")
	_ = agg.Record(ordermanager.OrderIntentEvent{
		ReqID:    "req-tg-001",
		Exchange: "bybit",
	})

	timerFired := false
	_, err = mgr.ScheduleTimeoutTimer("req-tg-001", "BTCUSDT", 5*time.Second, func() {
		timerFired = true
	})
	if err != nil {
		t.Fatalf("ScheduleTimeoutTimer failed: %v", err)
	}

	// Cancel timeout guard before expiration
	canceled := mgr.CancelTimeoutGuard("req-tg-001")
	if !canceled {
		t.Errorf("expected active timeout guard to be successfully canceled")
	}

	if timerFired {
		t.Errorf("timeout timer should not have fired after cancellation")
	}
}

// Test 6: Concurrent Multi-Order Intent Processing & Cache Isolation.
func TestBlackBox_ConcurrentRequestsThreadSafety(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := &blackboxExchangeClient{
		orderState: exchange.OrderStateFilled,
		dealVol:    1.0,
	}
	bus := eventbus.New(slog.Default())
	clock := mockClock{}
	repo := &blackboxTradeRepo{}
	noti := &blackboxNotifier{}
	engine := &app.Engine{
		Bus: bus,
		Providers: map[string]*app.ExchangeProvider{
			"bybit": {
				Name:     "bybit",
				Client:   client,
				TimeSync: newTestTimeSync(client),
			},
			"mexc": {
				Name:     "mexc",
				Client:   client,
				TimeSync: newTestTimeSync(client),
			},
		},
	}
	mgr, err := ordermanager.NewOrderManager(ctx, engine, bus, repo, noti, slog.Default())
	if err != nil {
		t.Fatalf("failed to create order manager: %v", err)
	}

	if err := mgr.Init(ctx); err != nil {
		t.Fatalf("failed to init order manager: %v", err)
	}

	const numOrders = 10
	var wg sync.WaitGroup

	for i := range numOrders {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			reqID := fmt.Sprintf("req-concurrent-%d", id)
			clientOID := fmt.Sprintf("client-concurrent-%d", id)

			intent := ordermanager.OrderIntentEvent{
				ReqID:         reqID,
				ClientOrderID: clientOID,
				Symbol:        "BTCUSDT",
				Exchange:      "bybit",
				StrategyType:  ordermanager.StrategyFundingArbitrage,
				Timestamp:     clock.Now(),
				Side:          shared.SideOpenLong,
				OrderType:     ordermanager.OrderTypeIOC,
				Price:         50000.0,
				Volume:        1.0,
				ContractSize:  1.0,
			}

			if err := mgr.Dispatch(ctx, intent); err != nil {
				t.Errorf("failed to dispatch intent %s: %v", reqID, err)
				return
			}

			time.Sleep(20 * time.Millisecond)

			mgr.HandlePositionUpdate(ctx, reqID, exchange.PersonalPositionUpdate{
				Symbol:           "BTCUSDT",
				HoldVolContract:  0.0,
				CloseVolContract: 1.0,
				OpenAvgPrice:     50000.0,
				CloseAvgPrice:    51000.0,
				CloseProfitLoss:  1000.0,
				Fee:              20.0,
			})
		}(i)
	}

	wg.Wait()

	// Wait for all 10 orders to complete
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(repo.SavedEvents()) >= numOrders {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if len(repo.SavedEvents()) != numOrders {
		t.Errorf("expected %d saved trade records, got %d", numOrders, len(repo.SavedEvents()))
	}
}

// Test 7: Order Canceled with No Fill completes immediately without awaiting position close.
func TestBlackBox_OrderCanceledNoFill_CompletesImmediately(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := &blackboxExchangeClient{
		orderState: exchange.OrderStateCanceled,
		dealVol:    0.0,
	}
	bus := eventbus.New(slog.Default())
	clock := mockClock{}
	repo := &blackboxTradeRepo{}
	noti := &blackboxNotifier{}
	engine := &app.Engine{
		Bus: bus,
		Providers: map[string]*app.ExchangeProvider{
			"bybit": {
				Name:     "bybit",
				Client:   client,
				TimeSync: newTestTimeSync(client),
			},
		},
	}
	mgr, err := ordermanager.NewOrderManager(ctx, engine, bus, repo, noti, slog.Default())
	if err != nil {
		t.Fatalf("failed to create order manager: %v", err)
	}
	if err := mgr.Init(ctx); err != nil {
		t.Fatalf("failed to init order manager: %v", err)
	}

	intent := ordermanager.OrderIntentEvent{
		ReqID:         "req-canceled-001",
		ClientOrderID: "client-canceled-001",
		Symbol:        "BTCUSDT",
		Exchange:      "bybit",
		StrategyType:  ordermanager.StrategyFundingReversion,
		Timestamp:     clock.Now(),
		Side:          shared.SideOpenLong,
		OrderType:     ordermanager.OrderTypeIOC,
		Price:         50000.0,
		Volume:        1.0,
		ContractSize:  1.0,
	}

	if err := mgr.Dispatch(ctx, intent); err != nil {
		t.Fatalf("failed to dispatch intent: %v", err)
	}

	// Wait for eventbus async pipeline to complete without position close update
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		agg := mgr.GetAggregate("req-canceled-001")
		if len(repo.SavedEvents()) > 0 && agg != nil && agg.State() == ordermanager.StateCompleted {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	saved := repo.SavedEvents()
	if len(saved) == 0 {
		t.Fatalf("expected trade record for canceled order to be saved immediately")
	}
	if saved[0].Outcome != string(ordermanager.OutcomeCanceledNoFill) {
		t.Errorf("expected outcome canceled_no_fill, got %s", saved[0].Outcome)
	}
}

// Test 8: Order Filled remains open until position close update arrives.
func TestBlackBox_OrderFilled_WaitsForPositionClose(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := &blackboxExchangeClient{
		orderState: exchange.OrderStateFilled,
		dealVol:    1.0,
	}
	bus := eventbus.New(slog.Default())
	clock := mockClock{}
	repo := &blackboxTradeRepo{}
	noti := &blackboxNotifier{}
	engine := &app.Engine{
		Bus: bus,
		Providers: map[string]*app.ExchangeProvider{
			"bybit": {
				Name:     "bybit",
				Client:   client,
				TimeSync: newTestTimeSync(client),
			},
		},
	}
	mgr, err := ordermanager.NewOrderManager(ctx, engine, bus, repo, noti, slog.Default())
	if err != nil {
		t.Fatalf("failed to create order manager: %v", err)
	}
	if err := mgr.Init(ctx); err != nil {
		t.Fatalf("failed to init order manager: %v", err)
	}

	intent := ordermanager.OrderIntentEvent{
		ReqID:         "req-filled-wait-001",
		ClientOrderID: "client-filled-wait-001",
		Symbol:        "BTCUSDT",
		Exchange:      "bybit",
		StrategyType:  ordermanager.StrategyFundingReversion,
		Timestamp:     clock.Now(),
		Side:          shared.SideOpenLong,
		OrderType:     ordermanager.OrderTypeIOC,
		Price:         50000.0,
		Volume:        1.0,
		ContractSize:  1.0,
	}

	if err := mgr.Dispatch(ctx, intent); err != nil {
		t.Fatalf("failed to dispatch intent: %v", err)
	}

	// Give time for order to fill and outcome_resolved to execute
	time.Sleep(100 * time.Millisecond)

	// Verify that trade is NOT completed yet
	if len(repo.SavedEvents()) > 0 {
		t.Fatalf("expected no saved trade record before position close update")
	}

	// Now emit position update closing position
	mgr.HandlePositionUpdate(ctx, "req-filled-wait-001", exchange.PersonalPositionUpdate{
		Symbol:           "BTCUSDT",
		HoldVolContract:  0.0,
		CloseVolContract: 1.0,
		OpenAvgPrice:     50000.0,
		CloseAvgPrice:    52000.0,
		CloseProfitLoss:  2000.0,
		Fee:              10.0,
	})

	// Wait for trade record to save
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if len(repo.SavedEvents()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	saved := repo.SavedEvents()
	if len(saved) == 0 {
		t.Fatalf("expected trade record after position close update")
	}
	if saved[0].ExitPrice != 52000.0 {
		t.Errorf("expected exit price 52000.0, got %.2f", saved[0].ExitPrice)
	}
}

// Test 10: Maker PostOnly order rests on book, then fills via position stream, completing on close.
func TestBlackBox_MakerPostOnly_RestingAndFillLifecycle(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	bus := eventbus.New(slog.Default())
	clock := mockClock{}
	repo := &blackboxTradeRepo{}
	noti := &blackboxNotifier{}

	client := &blackboxExchangeClient{
		orderState: shared.OrderStateNew,
		dealVol:    0.0,
	}

	engine := &app.Engine{
		Bus: bus,
		Providers: map[string]*app.ExchangeProvider{
			"mexc": {
				Name:     "mexc",
				Client:   client,
				TimeSync: newTestTimeSync(client),
			},
		},
	}

	mgr, err := ordermanager.NewOrderManager(ctx, engine, bus, repo, noti, slog.Default())
	if err != nil {
		t.Fatalf("failed to create order manager: %v", err)
	}
	if err := mgr.Init(ctx); err != nil {
		t.Fatalf("failed to init order manager: %v", err)
	}

	intent := ordermanager.OrderIntentEvent{
		ReqID:                "req-maker-resting-001",
		ClientOrderID:        "client-maker-resting-001",
		Symbol:               "BTCUSDT",
		Exchange:             "mexc",
		StrategyType:         ordermanager.StrategyDilution,
		Timestamp:            clock.Now(),
		Side:                 shared.SideOpenLong,
		OrderType:            ordermanager.OrderTypePostOnly,
		Price:                50000.0,
		Volume:               1.0,
		ContractSize:         1.0,
		PositionMode:         shared.PositionModeHedge,
		Leverage:             20,
		PositionCloseTimeout: 500 * time.Millisecond,
	}

	if err := mgr.Dispatch(ctx, intent); err != nil {
		t.Fatalf("failed to dispatch intent: %v", err)
	}

	// Give time for OutcomeWatcher to execute and classify as OutcomeResting
	time.Sleep(100 * time.Millisecond)

	// Ensure aggregate is in resting or submitted state, not prematurely completed
	if len(repo.SavedEvents()) > 0 {
		t.Fatalf("expected resting order not to be completed immediately")
	}

	// Now simulate stream fill from exchange
	mgr.HandlePositionUpdate(ctx, "req-maker-resting-001", exchange.PersonalPositionUpdate{
		Symbol:          "BTCUSDT",
		HoldVolContract: 1.0,
		OpenAvgPrice:    50000.0,
		HoldAvgPrice:    50000.0,
	})

	time.Sleep(50 * time.Millisecond)

	// Simulate position exit fill
	mgr.HandlePositionUpdate(ctx, "req-maker-resting-001", exchange.PersonalPositionUpdate{
		Symbol:           "BTCUSDT",
		HoldVolContract:  0.0,
		CloseVolContract: 1.0,
		OpenAvgPrice:     50000.0,
		CloseAvgPrice:    50100.0,
		CloseProfitLoss:  100.0,
		Fee:              0.0,
	})

	// Wait for trade record to save
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if len(repo.SavedEvents()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	saved := repo.SavedEvents()
	if len(saved) == 0 {
		t.Fatalf("expected trade record after position close update")
	}
	if saved[0].StrategyType != ordermanager.StrategyDilution {
		t.Errorf("expected strategy Dilution, got %s", saved[0].StrategyType)
	}
	if saved[0].ExitPrice != 50100.0 {
		t.Errorf("expected exit price 50100.0, got %.2f", saved[0].ExitPrice)
	}
}

func TestBlackBox_CloseOrderFilled_EmitsCompleted(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	bus := eventbus.New(slog.Default())
	repo := &blackboxTradeRepo{}
	noti := &blackboxNotifier{}

	client := &blackboxExchangeClient{
		orderState: exchange.OrderStateFilled,
		dealVol:    1.0,
	}

	engine := &app.Engine{
		Bus: bus,
		Providers: map[string]*app.ExchangeProvider{
			"toobit": {
				Name:     "toobit",
				Client:   client,
				TimeSync: newTestTimeSync(client),
			},
		},
	}

	mgr, err := ordermanager.NewOrderManager(ctx, engine, bus, repo, noti, nil)
	if err != nil {
		t.Fatalf("failed to create order manager: %v", err)
	}
	if err := mgr.Init(ctx); err != nil {
		t.Fatalf("failed to init order manager: %v", err)
	}

	intent := ordermanager.OrderIntentEvent{
		ReqID:                "req-close-fill-001",
		ClientOrderID:        "client-close-fill-001",
		Symbol:               "BTC-SWAP-USDT",
		Exchange:             "toobit",
		MarketType:           ordermanager.MarketTypeFuture,
		StrategyType:         ordermanager.StrategyDilution,
		Timestamp:            time.Now(),
		Side:                 shared.SideCloseLong,
		OrderType:            ordermanager.OrderTypePostOnly,
		Price:                63100.0,
		Volume:               1.0,
		MarginMode:           shared.MarginModeIsolated,
		PositionMode:         shared.PositionModeHedge,
		Leverage:             5,
		PositionCloseTimeout: 500 * time.Millisecond,
	}

	if err := mgr.Dispatch(ctx, intent); err != nil {
		t.Fatalf("failed to dispatch intent: %v", err)
	}

	// Wait for trade record to save upon close order fill
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if len(repo.SavedEvents()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	saved := repo.SavedEvents()
	if len(saved) == 0 {
		t.Fatalf("expected close order to emit completed and save trade record immediately on fill")
	}
	if saved[0].ReqID != "req-close-fill-001" {
		t.Errorf("expected ReqID req-close-fill-001, got %s", saved[0].ReqID)
	}
}

func TestBlackBox_Abort_EmitsCriticalNotification(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := &blackboxExchangeClient{
		createOrderErr: fmt.Errorf("insufficient balance for test"),
	}

	bus := eventbus.New(slog.Default())
	clock := mockClock{}
	repo := &blackboxTradeRepo{}
	noti := &blackboxNotifier{}
	engine := &app.Engine{
		Bus: bus,
		Providers: map[string]*app.ExchangeProvider{
			"toobit": {
				Name:     "toobit",
				Client:   client,
				TimeSync: newTestTimeSync(client),
			},
		},
	}
	defer func() { _ = engine.Shutdown(ctx) }()

	mgr, err := ordermanager.NewOrderManager(context.Background(), engine, bus, repo, noti, slog.Default())
	if err != nil {
		t.Fatalf("failed to create order manager: %v", err)
	}

	if err := mgr.Init(ctx); err != nil {
		t.Fatalf("failed to init order manager: %v", err)
	}

	intent := ordermanager.OrderIntentEvent{
		ReqID:         "req-abort-test-001",
		ClientOrderID: "client-abort-test-001",
		Symbol:        "BTC-SWAP-USDT",
		Exchange:      "toobit",
		MarketType:    ordermanager.MarketTypeFuture,
		StrategyType:  ordermanager.StrategyFundingReversion,
		Timestamp:     clock.Now(),
		Side:          shared.SideOpenLong,
		OrderType:     ordermanager.OrderTypeIOC,
		Price:         60000.0,
		Volume:        1.0,
		MarginMode:    shared.MarginModeCross,
		Leverage:      10,
	}

	// Dispatch should fail or abort
	_ = mgr.Dispatch(ctx, intent)

	// Wait for notification to be received
	deadline := time.Now().Add(3 * time.Second)
	var criticalNotif *notifier.Event
	for time.Now().Before(deadline) {
		for _, evt := range noti.SentEvents() {
			if evt.Level == notifier.LevelCritical {
				critCopy := evt
				criticalNotif = &critCopy
				break
			}
		}
		if criticalNotif != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if criticalNotif == nil {
		t.Fatalf("expected critical notification event for aborted order, got sent events: %+v", noti.SentEvents())
	}
	if criticalNotif.Level != notifier.LevelCritical {
		t.Errorf("expected level %s, got %s", notifier.LevelCritical, criticalNotif.Level)
	}
}
