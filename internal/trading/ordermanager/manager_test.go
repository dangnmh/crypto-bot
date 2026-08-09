package ordermanager_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/notifier"
	"crypto-bot/internal/trading/ordermanager"
	"crypto-bot/pkg/eventbus"
)

type mockExchangeClient struct {
	marginModeSwitched   bool
	positionModeSwitched bool
	leverageChanged      bool
	orderCreated         bool
	positionClosed       bool
	allClosed            bool
	tpslPlaced           bool
}

func (m *mockExchangeClient) SwitchMarginMode(ctx context.Context, symbol string, mode shared.MarginMode, leverage int, side shared.Side) error {
	m.marginModeSwitched = true
	return nil
}

func (m *mockExchangeClient) SwitchPositionMode(ctx context.Context, symbol string, mode shared.PositionMode) error {
	m.positionModeSwitched = true
	return nil
}

func (m *mockExchangeClient) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	m.leverageChanged = true
	return nil
}

func (m *mockExchangeClient) SupportLeverageOnOrder() bool {
	return false
}

func (m *mockExchangeClient) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
	m.orderCreated = true
	return exchange.CreateOrderResult{OrderID: "mock-order-123", TPSLSubmitted: false}, nil
}

func (m *mockExchangeClient) PlaceTPSL(ctx context.Context, req exchange.TPSLRequest) error {
	m.tpslPlaced = true
	return nil
}

func (m *mockExchangeClient) CancelOrder(ctx context.Context, symbol, orderID string) error {
	return nil
}

func (m *mockExchangeClient) ClosePosition(ctx context.Context, symbol string, side shared.Side, volume float64, positionMode shared.PositionMode, leverage int) error {
	m.positionClosed = true
	return nil
}

func (m *mockExchangeClient) CloseAllPositions(ctx context.Context, symbol string) error {
	m.allClosed = true
	return nil
}

func (m *mockExchangeClient) GetOrder(ctx context.Context, symbol, orderID string) (*exchange.OrderInfo, error) {
	return &exchange.OrderInfo{
		OrderID:      orderID,
		Symbol:       symbol,
		State:        exchange.OrderStateFilled,
		DealVol:      1.0,
		DealAvgPrice: 50000.0,
	}, nil
}

func (m *mockExchangeClient) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	return []exchange.Position{
		{Symbol: symbol, HoldVolContract: 1.0},
	}, nil
}

type mockClock struct{}

func (m mockClock) Now() time.Time                                   { return time.Now() }
func (m mockClock) LatencyMs() int64                                 { return 20 }
func (m mockClock) Until(t time.Time) time.Duration                  { return time.Duration(0) }
func (m mockClock) Sleep(ctx context.Context, d time.Duration) error { return nil }

type mockOrderWatcher struct{}

func (m *mockOrderWatcher) SubscribeOrderUpdates(ctx context.Context, symbol string) (<-chan ordermanager.OrderStreamUpdate, error) {
	ch := make(chan ordermanager.OrderStreamUpdate, 1)
	ch <- ordermanager.OrderStreamUpdate{
		Symbol:    symbol,
		OrderID:   "mock-order-123",
		Status:    "FILLED",
		FilledVol: 1.0,
		AvgPrice:  50000.0,
	}
	close(ch)
	return ch, nil
}

func TestNewOrderManager_NilValidation(t *testing.T) {
	t.Parallel()

	client := &mockExchangeClient{}
	bus := eventbus.New(slog.Default())
	clock := mockClock{}

	if _, err := ordermanager.NewOrderManager(nil, nil, bus, clock, nil); err == nil {
		t.Errorf("expected error when client is nil")
	}
	if _, err := ordermanager.NewOrderManager(client, nil, nil, clock, nil); err == nil {
		t.Errorf("expected error when bus is nil")
	}
	if _, err := ordermanager.NewOrderManager(client, nil, bus, nil, nil); err == nil {
		t.Errorf("expected error when clock is nil")
	}
	if mgr, err := ordermanager.NewOrderManager(client, &mockOrderWatcher{}, bus, clock, nil); err != nil || mgr == nil {
		t.Errorf("expected successful creation with non-nil dependencies, got err: %v", err)
	}
}

func TestOrderManager_MicroEventPipeline(t *testing.T) {
	t.Parallel()

	client := &mockExchangeClient{}
	bus := eventbus.New(slog.Default())
	watcher := &mockOrderWatcher{}
	mgr, err := ordermanager.NewOrderManager(client, watcher, bus, mockClock{}, nil)
	if err != nil {
		t.Fatalf("failed to create order manager: %v", err)
	}

	ctx := context.Background()
	intent := createTestOrderIntent()

	// Steps 1 - 4
	submitted := runPreFlightToOrderExecution(t, ctx, mgr, client, intent)

	// Step 5
	timeoutEvt := runScheduleTimeoutTest(t, mgr)

	// Step 6
	runOutcomeWatcherTest(t, ctx, mgr, submitted)

	// Step 7
	runTimeoutCheckTest(t, ctx, mgr, timeoutEvt)

	// Step 8
	runBailoutTest(t, ctx, mgr, client)

	// Step 9
	runEnrichAndCompleteTest(t, ctx, mgr)
}

func createTestOrderIntent() ordermanager.OrderIntentEvent {
	return ordermanager.OrderIntentEvent{
		BaseExecutionEvent: ordermanager.BaseExecutionEvent{
			ReqID:         "req-001",
			ClientOrderID: "client-oid-001",
			Symbol:        "BTCUSDT",
			Exchange:      "MEXC",
			StrategyType:  ordermanager.StrategyFundingReversion,
			Timestamp:     time.Now(),
		},
		Side:            shared.SideOpenLong,
		OrderType:       ordermanager.OrderTypePostOnly,
		Price:           50000.0,
		Volume:          1.0,
		MarginMode:      shared.MarginModeIsolated,
		PositionMode:    shared.PositionModeOneWay,
		Leverage:        10,
		TakeProfitPrice: 51000.0,
		StopLossPrice:   49000.0,
		TimeoutDuration: 200 * time.Millisecond,
	}
}

func runPreFlightToOrderExecution(t *testing.T, ctx context.Context, mgr *ordermanager.OrderManager, client *mockExchangeClient, intent ordermanager.OrderIntentEvent) ordermanager.OrderSubmittedEvent {
	preflight, err := mgr.HandlePreFlight(ctx, intent)
	if err != nil {
		t.Fatalf("HandlePreFlight failed: %v", err)
	}
	if !client.marginModeSwitched || !client.positionModeSwitched || !client.leverageChanged {
		t.Errorf("expected pre-flight exchange setup methods to be called")
	}

	fireWindow, err := mgr.HandleFireTiming(ctx, preflight)
	if err != nil {
		t.Fatalf("HandleFireTiming failed: %v", err)
	}

	submitted, err := mgr.HandleExecuteOrder(ctx, fireWindow)
	if err != nil {
		t.Fatalf("HandleExecuteOrder failed: %v", err)
	}

	exOID, found := mgr.GetExchangeOrderIDByClientOrderID(submitted.GetClientOrderID())
	if !found || exOID != "mock-order-123" {
		t.Errorf("expected GetExchangeOrderIDByClientOrderID to return mock-order-123, got %s (found: %v)", exOID, found)
	}

	tpslEvt, err := mgr.HandleTPSLContingency(ctx, submitted, intent)
	if err != nil {
		t.Fatalf("HandleTPSLContingency failed: %v", err)
	}
	if tpslEvt == nil || !client.tpslPlaced {
		t.Errorf("expected TPSL contingency order to be placed")
	}

	return submitted
}

func runScheduleTimeoutTest(t *testing.T, mgr *ordermanager.OrderManager) ordermanager.OrderTimeoutScheduledEvent {
	timeoutEvt := mgr.HandleScheduleTimeout("req-001", "BTCUSDT", 200*time.Millisecond, nil)
	if timeoutEvt.ReqID != "req-001" {
		t.Errorf("expected timeout event req_id req-001")
	}
	if !mgr.CancelTimeoutGuard("req-001") {
		t.Errorf("expected active timeout guard to be canceled")
	}
	return timeoutEvt
}

func runOutcomeWatcherTest(t *testing.T, ctx context.Context, mgr *ordermanager.OrderManager, submitted ordermanager.OrderSubmittedEvent) {
	resolved, err := mgr.HandleOutcomeWatcher(ctx, submitted)
	if err != nil {
		t.Fatalf("HandleOutcomeWatcher failed: %v", err)
	}
	if resolved.Outcome != ordermanager.OutcomeFilled || resolved.FilledVol != 1.0 {
		t.Errorf("expected OutcomeFilled with 1.0 vol, got %+v", resolved)
	}
}

func runTimeoutCheckTest(t *testing.T, ctx context.Context, mgr *ordermanager.OrderManager, timeoutEvt ordermanager.OrderTimeoutScheduledEvent) {
	timeoutExpired, err := mgr.HandleTimeoutCheck(ctx, timeoutEvt)
	if err != nil {
		t.Fatalf("HandleTimeoutCheck failed: %v", err)
	}
	if timeoutExpired == nil || timeoutExpired.HoldVol != 1.0 {
		t.Errorf("expected timeout expired event with holdVol 1.0")
	}
}

func runBailoutTest(t *testing.T, ctx context.Context, mgr *ordermanager.OrderManager, client *mockExchangeClient) {
	bailout, err := mgr.HandleExecuteBailout(ctx, "BTCUSDT", shared.SideCloseLong, 1.0, "timeout")
	if err != nil {
		t.Fatalf("HandleExecuteBailout failed: %v", err)
	}
	if !client.allClosed {
		t.Errorf("expected CloseAllPositions to be called")
	}
	if bailout.Symbol != "BTCUSDT" {
		t.Errorf("unexpected bailout symbol: %s", bailout.Symbol)
	}
}

func runEnrichAndCompleteTest(t *testing.T, ctx context.Context, mgr *ordermanager.OrderManager) {
	repo := &mockTradeRepo{}
	noti := &mockNotifier{}
	mgr.SetRepository(repo)
	mgr.SetNotifier(noti)

	completed := mgr.HandleEnrichAndComplete(ctx, "req-001", "client-oid-001", "BTCUSDT", ordermanager.StrategyFundingReversion, "filled", "normal")
	if completed.GetReqID() != "req-001" || completed.Outcome != "filled" || completed.StrategyType != ordermanager.StrategyFundingReversion {
		t.Errorf("unexpected completed event properties: %+v", completed)
	}

	if !completed.ShouldNotify() {
		t.Errorf("expected completed event to have ShouldNotify() == true")
	}
}

type mockNotifier struct {
	sentCount int
}

func (m *mockNotifier) Send(ctx context.Context, evt notifier.Event) error {
	m.sentCount++
	return nil
}

func (m *mockNotifier) Start(ctx context.Context) error { return nil }
func (m *mockNotifier) Stop(ctx context.Context) error  { return nil }

type mockTradeRepo struct {
	saved bool
}

func (m *mockTradeRepo) Save(ctx context.Context, evt ordermanager.OrderTradeRecordEvent) error {
	m.saved = true
	return nil
}

func TestOrderManager_Dispatch_EDD(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		reqID  string
		symbol string
		side   shared.Side
		price  float64
		vol    float64
	}{
		{
			name:   "watermill_dispatch_long",
			reqID:  "req-watermill-001",
			symbol: "BTCUSDT",
			side:   shared.SideOpenLong,
			price:  50000.0,
			vol:    1.0,
		},
		{
			name:   "edd_dispatch_short",
			reqID:  "req-edd-001",
			symbol: "ETHUSDT",
			side:   shared.SideOpenShort,
			price:  3000.0,
			vol:    5.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := &mockExchangeClient{}
			bus := eventbus.New(slog.Default())
			mgr, err := ordermanager.NewOrderManager(client, nil, bus, mockClock{}, nil)
			if err != nil {
				t.Fatalf("failed to create order manager: %v", err)
			}

			ctx := context.Background()
			intent := ordermanager.OrderIntentEvent{
				BaseExecutionEvent: ordermanager.BaseExecutionEvent{
					ReqID:        tt.reqID,
					Symbol:       tt.symbol,
					Exchange:     "MEXC",
					StrategyType: ordermanager.StrategyFundingReversion,
					Timestamp:    time.Now(),
				},
				Side:      tt.side,
				OrderType: ordermanager.OrderTypeIOC,
				Price:     tt.price,
				Volume:    tt.vol,
			}

			if err := mgr.Dispatch(ctx, intent); err != nil {
				t.Fatalf("Dispatch failed: %v", err)
			}

			agg := mgr.GetAggregate(tt.reqID)
			if agg.State() != ordermanager.StateInit {
				t.Errorf("expected aggregate state StateInit, got %s", agg.State())
			}
		})
	}
}
