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
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/notifier"
	"crypto-bot/internal/trading/ordermanager"
	"crypto-bot/pkg/eventbus"
)

// Blackbox mock exchange client implementing exchange API interfaces.
type blackboxExchangeClient struct {
	mu                 sync.Mutex
	createOrderErr     error
	getOrderErr        error
	closeAllErr        error
	closePositionErr   error
	closedPnLErr       error
	orderState         shared.OrderState
	dealVol            float64
	closedPnLInfo      *exchange.ClosedPnLInfo
	openPositions      []exchange.Position
	createOrderCalls   atomic.Int32
	closePositionCalls atomic.Int32
	closeAllCalls      atomic.Int32
	getClosedPnLCalls  atomic.Int32
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

func (m *blackboxExchangeClient) GetOrderPNL(ctx context.Context, symbol, orderID string) (*exchange.ClosedPnLInfo, error) {
	m.getClosedPnLCalls.Add(1)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closedPnLErr != nil {
		return nil, m.closedPnLErr
	}
	if m.closedPnLInfo != nil {
		return m.closedPnLInfo, nil
	}
	return &exchange.ClosedPnLInfo{
		Exchange:           "BYBIT",
		Symbol:             symbol,
		ClosedSizeContract: new(1.0),
		ClosedSizeCoin:     new(1.0),
		EntryPrice:         50000.0,
		ExitPrice:          51000.0,
		GrossPnL:           1000.0,
		NetPnl:             980.0,
		PnLRate:            0.02,
		Fee:                20.0,
		FundingFee:         0.0,
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

// Blackbox mock WebSocket OrderWatcher.
type blackboxOrderWatcher struct {
	updateChan chan ordermanager.OrderStreamUpdate
}

func (w *blackboxOrderWatcher) SubscribeOrderUpdates(ctx context.Context, symbol string) (<-chan ordermanager.OrderStreamUpdate, error) {
	return w.updateChan, nil
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

	mgr, err := ordermanager.NewOrderManager(client, nil, bus, clock, slog.Default())
	if err != nil {
		t.Fatalf("failed to create order manager: %v", err)
	}
	mgr.SetRepository(repo)
	mgr.SetNotifier(noti)

	ordermanager.InitGlobalSubscriptions(ctx, mgr)

	intent := ordermanager.OrderIntentEvent{
		BaseExecutionEvent: ordermanager.BaseExecutionEvent{
			ReqID:         "req-bb-001",
			ClientOrderID: "client-bb-001",
			Symbol:        "BTCUSDT",
			Exchange:      "BYBIT",
			StrategyType:  ordermanager.StrategyFundingReversion,
			SendNotify:    true,
			Timestamp:     clock.Now(),
		},
		Side:         shared.SideOpenLong,
		OrderType:    ordermanager.OrderTypeIOC,
		Price:        50000.0,
		Volume:       1.0,
		ContractSize: 1.0,
		MarginMode:   shared.MarginModeCross,
		PositionMode: shared.PositionModeOneWay,
		Leverage:     10,
	}

	if err := mgr.Dispatch(ctx, intent); err != nil {
		t.Fatalf("failed to dispatch intent: %v", err)
	}

	// Wait for eventbus async subscriber pipeline to complete
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(repo.SavedEvents()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

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
	exOrderID, found := mgr.GetExchangeOrderIDByClientOrderID(trade.ClientOrderID)

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

// Test 2: WebSocket Stream Fast-Path Fill Watcher.
func TestBlackBox_WebSocketFastPathFill(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := &blackboxExchangeClient{
		orderState: exchange.OrderStateFilled,
		dealVol:    2.0,
	}
	bus := eventbus.New(slog.Default())
	clock := mockClock{}

	wsChan := make(chan ordermanager.OrderStreamUpdate, 1)
	watcher := &blackboxOrderWatcher{updateChan: wsChan}

	mgr, err := ordermanager.NewOrderManager(client, watcher, bus, clock, slog.Default())
	if err != nil {
		t.Fatalf("failed to create order manager with watcher: %v", err)
	}

	submittedEvt := ordermanager.OrderSubmittedEvent{
		BaseExecutionEvent: ordermanager.BaseExecutionEvent{
			ReqID:         "req-ws-001",
			ClientOrderID: "client-ws-001",
			Symbol:        "ETHUSDT",
			StrategyType:  ordermanager.StrategyFundingReversion,
			Timestamp:     clock.Now(),
		},
		Price:       3000.0,
		Volume:      2.0,
		SubmittedAt: clock.Now(),
	}

	mgr.SetExchangeOrderIDByClientOrderID("client-ws-001", "ex-order-client-ws-001")

	// Emit real-time WS fill update
	wsChan <- ordermanager.OrderStreamUpdate{
		Symbol:    "ETHUSDT",
		OrderID:   "ex-order-client-ws-001",
		Status:    "FILLED",
		FilledVol: 2.0,
		AvgPrice:  3000.0,
	}

	resolved, err := mgr.HandleOutcomeWatcher(ctx, submittedEvt)
	if err != nil {
		t.Fatalf("HandleOutcomeWatcher failed: %v", err)
	}

	if resolved.Outcome != ordermanager.OutcomeFilled {
		t.Errorf("expected OutcomeFilled via WS fast-path, got %s", resolved.Outcome)
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

	mgr, err := ordermanager.NewOrderManager(client, nil, eventbus.New(slog.Default()), mockClock{}, nil)
	if err != nil {
		t.Fatalf("failed to create order manager: %v", err)
	}

	bailoutEvt, err := mgr.HandleExecuteBailout(ctx, "BTCUSDT", shared.SideOpenLong, 1.5, "timeout_expired")
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
		BaseExecutionEvent: ordermanager.BaseExecutionEvent{
			ReqID:        "req-cs-001",
			Symbol:       "BTCUSDT",
			StrategyType: ordermanager.StrategyFundingArbitrage,
		},
		Side:         shared.SideOpenLong,
		OrderType:    ordermanager.OrderTypeLimit,
		Volume:       5.0,
		ContractSize: 10.0, // 10x Contract Multiplier
	}
	_ = agg.Record(intent)

	completed := ordermanager.OrderCompletedEvent{
		BaseExecutionEvent: ordermanager.BaseExecutionEvent{
			ReqID:        "req-cs-001",
			Symbol:       "BTCUSDT",
			StrategyType: ordermanager.StrategyFundingArbitrage,
		},
		Outcome:      string(ordermanager.OutcomeFilled),
		EntryPrice:   50000.0,
		ExitPrice:    52000.0,
		Volume:       5.0,
		ContractSize: 10.0,
		GrossProfit:  100000.0,
		NetProfit:    99000.0,
		CompletedAt:  time.Now(),
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
	mgr, err := ordermanager.NewOrderManager(client, nil, eventbus.New(slog.Default()), mockClock{}, nil)
	if err != nil {
		t.Fatalf("failed to create order manager: %v", err)
	}

	timerFired := false
	_ = mgr.HandleScheduleTimeout("req-tg-001", "BTCUSDT", 5*time.Second, func() {
		timerFired = true
	})

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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := &blackboxExchangeClient{
		orderState: exchange.OrderStateFilled,
		dealVol:    1.0,
	}
	bus := eventbus.New(slog.Default())
	clock := mockClock{}
	repo := &blackboxTradeRepo{}

	mgr, err := ordermanager.NewOrderManager(client, nil, bus, clock, slog.Default())
	if err != nil {
		t.Fatalf("failed to create order manager: %v", err)
	}
	mgr.SetRepository(repo)

	ordermanager.InitGlobalSubscriptions(ctx, mgr)

	const numOrders = 10
	var wg sync.WaitGroup

	for i := range numOrders {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			reqID := fmt.Sprintf("req-concurrent-%d", id)
			clientOID := fmt.Sprintf("client-concurrent-%d", id)

			intent := ordermanager.OrderIntentEvent{
				BaseExecutionEvent: ordermanager.BaseExecutionEvent{
					ReqID:         reqID,
					ClientOrderID: clientOID,
					Symbol:        "BTCUSDT",
					StrategyType:  ordermanager.StrategyGrid,
					Timestamp:     clock.Now(),
				},
				Side:         shared.SideOpenLong,
				OrderType:    ordermanager.OrderTypeLimit,
				Price:        50000.0,
				Volume:       1.0,
				ContractSize: 1.0,
			}

			if err := mgr.Dispatch(ctx, intent); err != nil {
				t.Errorf("failed to dispatch concurrent intent %d: %v", id, err)
			}
		}(i)
	}

	wg.Wait()

	// Wait for eventbus async subscriber pipeline to complete
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(repo.SavedEvents()) == numOrders {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	saved := repo.SavedEvents()
	if len(saved) != numOrders {
		t.Errorf("expected %d saved trade records, got %d", numOrders, len(saved))
	}
}
