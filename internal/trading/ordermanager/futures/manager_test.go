package futures_test

import (
	"context"
	"errors"
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
	"crypto-bot/internal/trading/ordermanager/futures"
	"crypto-bot/pkg/eventbus"

	"github.com/stretchr/testify/assert"
)

type mockExchangeClient struct {
	exchange.UnimplementedClient
	marginModeSwitched   bool
	positionModeSwitched bool
	leverageChanged      bool
	orderCreated         bool
	positionClosed       bool
	allClosed            bool
	tpslPlaced           bool
	cancelOrderCalled    bool
	cancelAllCalled      bool
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
	m.cancelOrderCalled = true
	return nil
}

func (m *mockExchangeClient) CancelAllOpenOrders(ctx context.Context, symbol string) error {
	m.cancelAllCalled = true
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

func (m mockClock) Now() time.Time                               { return time.Now() }
func (m mockClock) Until(t time.Time) time.Duration              { return time.Duration(0) }
func (m mockClock) GetServerTime() int64                         { return time.Now().UnixMilli() }
func (m mockClock) LatencyMs() int64                             { return 20 }
func (m mockClock) Offset() int64                                { return 0 }
func (m mockClock) IsHealthy() bool                              { return true }
func (m mockClock) MsUntilTarget(targetServerTimeMs int64) int64 { return 0 }
func (m mockClock) Sleep(ctx context.Context, d time.Duration) error {
	return nil
}

func TestNewOrderManager_NilValidation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	if _, err := futures.NewOrderManager(ctx, nil, nil, nil, nil, nil); err == nil {
		t.Errorf("expected error when bus is nil")
	}
	bus := eventbus.New(slog.Default())
	engine := &app.Engine{}
	repo := &mockTradeRepo{}
	noti := &mockNotifier{}
	if mgr, err := futures.NewOrderManager(ctx, engine, bus, repo, noti, nil); err != nil || mgr == nil {
		t.Errorf("expected successful creation with non-nil dependencies, got err: %v", err)
	}
}

func TestOrderManager_MicroEventPipeline(t *testing.T) {
	t.Parallel()

	client := &mockExchangeClient{}
	bus := eventbus.New(slog.Default())
	ctx := context.Background()
	repo := &mockTradeRepo{}
	noti := &mockNotifier{}
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
	mgr, err := futures.NewOrderManager(ctx, engine, bus, repo, noti, nil)
	if err != nil {
		t.Fatalf("failed to create order manager: %v", err)
	}
	if err := mgr.Init(ctx); err != nil {
		t.Fatalf("failed to init order manager: %v", err)
	}

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

func createTestOrderIntent() futures.OrderIntentEvent {
	return futures.OrderIntentEvent{
		ReqID:                "req-001",
		ClientOrderID:        "client-oid-001",
		Symbol:               "BTCUSDT",
		Exchange:             "MEXC",
		StrategyType:         futures.StrategyFundingReversion,
		Timestamp:            time.Now(),
		Side:                 shared.SideOpenLong,
		OrderType:            futures.OrderTypePostOnly,
		Price:                50000.0,
		Volume:               1.0,
		MarginMode:           shared.MarginModeIsolated,
		PositionMode:         shared.PositionModeOneWay,
		Leverage:             10,
		TakeProfitPrice:      51000.0,
		StopLossPrice:        49000.0,
		PositionCloseTimeout: 200 * time.Millisecond,
	}
}

func runPreFlightToOrderExecution(t *testing.T, ctx context.Context, mgr *futures.OrderManager, client *mockExchangeClient, intent futures.OrderIntentEvent) futures.OrderSubmittedEvent {
	_ = mgr.GetAggregate(intent.GetReqID()).Record(intent)
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

	watchReady, err := mgr.HandlePositionWatchReady(ctx, fireWindow)
	if err != nil {
		t.Fatalf("HandlePositionWatchReady failed: %v", err)
	}

	submitted, err := mgr.HandleExecuteOrder(ctx, watchReady)
	if err != nil {
		t.Fatalf("HandleExecuteOrder failed: %v", err)
	}

	exOID, found := mgr.GetExchangeOrderIDByReqID(submitted.GetReqID())
	if !found || exOID != "mock-order-123" {
		t.Errorf("expected GetExchangeOrderIDByReqID to return mock-order-123, got %s (found: %v)", exOID, found)
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

func runScheduleTimeoutTest(t *testing.T, mgr *futures.OrderManager) futures.OrderTimeoutScheduledEvent {
	timeoutEvt, err := mgr.ScheduleTimeoutTimer("req-001", "BTCUSDT", 200*time.Millisecond, nil)
	if err != nil {
		t.Fatalf("ScheduleTimeoutTimer failed: %v", err)
	}
	if timeoutEvt.ReqID != "req-001" {
		t.Errorf("expected timeout event req_id req-001")
	}
	if !mgr.CancelTimeoutGuard("req-001") {
		t.Errorf("expected active timeout guard to be canceled")
	}
	return timeoutEvt
}

func runOutcomeWatcherTest(t *testing.T, ctx context.Context, mgr *futures.OrderManager, submitted futures.OrderSubmittedEvent) {
	resolved, err := mgr.HandleOutcomeWatcher(ctx, submitted)
	if err != nil {
		t.Fatalf("HandleOutcomeWatcher failed: %v", err)
	}
	if resolved.Outcome != futures.OutcomeFilled || resolved.FilledVol != 1.0 {
		t.Errorf("expected OutcomeFilled with 1.0 vol, got %+v", resolved)
	}
}

func runTimeoutCheckTest(t *testing.T, ctx context.Context, mgr *futures.OrderManager, timeoutEvt futures.OrderTimeoutScheduledEvent) {
	timeoutExpired, err := mgr.HandleTimeoutCheck(ctx, timeoutEvt)
	if err != nil {
		t.Fatalf("HandleTimeoutCheck failed: %v", err)
	}
	if timeoutExpired == nil || timeoutExpired.HoldVol != 1.0 {
		t.Errorf("expected timeout expired event with holdVol 1.0")
	}
}

func runBailoutTest(t *testing.T, ctx context.Context, mgr *futures.OrderManager, client *mockExchangeClient) {
	bailout, err := mgr.HandleExecuteBailout(ctx, "req-bailout-001", "mexc", "BTCUSDT", shared.SideCloseLong, 1.0, "timeout")
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

func runEnrichAndCompleteTest(t *testing.T, ctx context.Context, mgr *futures.OrderManager) {
	completed, err := mgr.HandleEnrichAndComplete(ctx, "mexc", "req-001", "client-oid-001", "BTCUSDT", futures.StrategyFundingReversion, "filled", "normal")
	if err != nil {
		t.Fatalf("HandleEnrichAndComplete failed: %v", err)
	}
	if completed.GetReqID() != "req-001" || completed.Outcome != "filled" || completed.StrategyType != futures.StrategyFundingReversion {
		t.Errorf("unexpected completed event properties: %+v", completed)
	}
}

func TestOrderSubmittedEvent_GetNotifyMessage(t *testing.T) {
	t.Parallel()

	evt := futures.OrderSubmittedEvent{
		ReqID:         "12082026170000PROMTOOBITFUTURES",
		ClientOrderID: "12082026170000PROMTOOBITFUTURES",
		Symbol:        "PROM-SWAP-USDT",
		Exchange:      "toobit_futures",
		StrategyType:  futures.StrategyFundingReversion,
		Side:          shared.SideOpenLong,
		Leverage:      20,
		MarginUSDT:    30.0,
		FundingRate:   -0.021,
		Vol24hUSDT:    1200000.0,
		ContractSize:  1.0,
		Extra: map[string]any{
			"margin_usdt":  30.0,
			"funding_rate": -0.021,
			"vol_usdt_24h": 1200000.0,
		},
		OrderID: "2279963257363092992",
		Price:   3.032,
		Volume:  4002.638522,
	}

	msg := evt.GetNotifyMessage()
	assert.Contains(t, msg, "🟡 [FUNDING_REVERSION] [toobit_futures] [SUBMITTED]")
	assert.Contains(t, msg, "• Symbol: PROM-SWAP-USDT | Side: Long")
	assert.Contains(t, msg, "• Margin: 30.00 USDT | Leverage: 20x")
	assert.Contains(t, msg, "• Price: 3.032000 | Size: 12,136.00 USDT")
	assert.Contains(t, msg, "• FR: -2.1% | Vol24h: $1.2m")
	assert.Contains(t, msg, "• Order ID: 2279963257363092992")
	assert.Contains(t, msg, "• Client ID: 12082026170000PROMTOOBITFUTURES")
	assert.Contains(t, msg, "• Req ID: 12082026170000PROMTOOBITFUTURES")
}

type mockNotifier struct {
	mu        sync.Mutex
	sentCount int
}

func (m *mockNotifier) Send(ctx context.Context, evt notifier.Event) error {
	m.mu.Lock()
	m.sentCount++
	m.mu.Unlock()
	return nil
}

func (m *mockNotifier) Start(ctx context.Context) error { return nil }
func (m *mockNotifier) Stop(ctx context.Context) error  { return nil }

type mockTradeRepo struct {
	saved bool
}

func (m *mockTradeRepo) Save(ctx context.Context, evt futures.OrderTradeRecordEvent) error {
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
			repo := &mockTradeRepo{}
			noti := &mockNotifier{}
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
			mgr, err := futures.NewOrderManager(context.Background(), engine, bus, repo, noti, nil)
			if err != nil {
				t.Fatalf("failed to create order manager: %v", err)
			}
			if err := mgr.Init(context.Background()); err != nil {
				t.Fatalf("failed to init order manager: %v", err)
			}

			ctx := context.Background()
			intent := futures.OrderIntentEvent{
				ReqID:        tt.reqID,
				Symbol:       tt.symbol,
				Exchange:     "MEXC",
				StrategyType: futures.StrategyFundingReversion,
				Timestamp:    time.Now(),
				Side:         tt.side,
				OrderType:    futures.OrderTypeIOC,
				Price:        tt.price,
				Volume:       tt.vol,
			}

			if err := mgr.Dispatch(ctx, intent); err != nil {
				t.Fatalf("Dispatch failed: %v", err)
			}

			agg := mgr.GetAggregate(tt.reqID)
			if agg.State() != futures.StateInit {
				t.Errorf("expected aggregate state StateInit, got %s", agg.State())
			}
		})
	}
}

func TestOrderAbortedEvent_Notification(t *testing.T) {
	t.Parallel()

	evt := futures.OrderAbortedEvent{
		ReqID:         "req-abort-123",
		ClientOrderID: "client-abort-123",
		Symbol:        "BTCUSDT",
		Exchange:      "BINANCE",
		StrategyType:  futures.StrategyFundingReversion,
		OrderID:       "ex-order-999",
		Reason:        "submit_error",
		Error:         "insufficient margin",
		AbortedAt:     time.Now(),
	}

	assert.Equal(t, futures.TopicOrderAborted, evt.GetTopic())
	assert.True(t, evt.ShouldNotify())
	assert.Equal(t, notifier.LevelCritical, evt.GetNotiLevel())

	msg := evt.GetNotifyMessage()
	assert.Contains(t, msg, "🔴 [FUNDING_REVERSION] [BINANCE] [ABORTED]")
	assert.Contains(t, msg, "• Symbol: BTCUSDT")
	assert.Contains(t, msg, "• Reason: submit_error")
	assert.Contains(t, msg, "• Error: insufficient margin")
	assert.Contains(t, msg, "• Order ID: ex-order-999")
	assert.Contains(t, msg, "• Client ID: client-abort-123")
	assert.Contains(t, msg, "• Req ID: req-abort-123")
}

func TestOrderCompletedEvent_Notification_Canceled(t *testing.T) {
	t.Parallel()

	completedNormal := futures.OrderCompletedEvent{Outcome: futures.OutcomeFilled}
	assert.True(t, completedNormal.ShouldNotify())

	completedCanceled := futures.OrderCompletedEvent{
		ReqID:         "req-canceled-123",
		ClientOrderID: "client-canceled-123",
		Symbol:        "BTCUSDT",
		Exchange:      "BINANCE",
		StrategyType:  futures.StrategyFundingReversion,
		OrderID:       "ex-order-123",
		Outcome:       futures.OutcomeCanceledNoFill,
		Reason:        "user canceled",
	}
	assert.True(t, completedCanceled.ShouldNotify())
	assert.Equal(t, notifier.LevelCritical, completedCanceled.GetNotiLevel())
	msg := completedCanceled.GetNotifyMessage()
	assert.Contains(t, msg, "🔵 [FUNDING_REVERSION] [BINANCE] [CANCELED_NO_FILL]")
	assert.Contains(t, msg, "• Symbol: BTCUSDT")
	assert.Contains(t, msg, "• Outcome: canceled_no_fill")
}

func TestOrderCompletedEvent_Notification_Filled(t *testing.T) {
	t.Parallel()

	completedFilled := futures.OrderCompletedEvent{
		ReqID:          "req-filled-123",
		ClientOrderID:  "client-filled-123",
		Symbol:         "LAB-SWAP-USDT",
		Exchange:       "toobit_futures",
		StrategyType:   futures.StrategyObfuscator,
		OrderID:        "2282128112719258880",
		Outcome:        futures.OutcomeFilled,
		Side:           shared.SideOpenLong,
		EntryPrice:     0.013190,
		ExitPrice:      0.013180,
		VolumeUSDT:     10.82,
		NetProfit:      -0.0190,
		PnLPct:         -0.08,
		Fee:            0.0108,
		HoldDurationMs: 35000,
	}
	assert.True(t, completedFilled.ShouldNotify())
	assert.Equal(t, notifier.LevelCritical, completedFilled.GetNotiLevel())
	msg := completedFilled.GetNotifyMessage()
	assert.Contains(t, msg, "🔴 [OBFUSCATOR] [toobit_futures] [COMPLETED]")
	assert.Contains(t, msg, "• Symbol: LAB-SWAP-USDT")
	assert.Contains(t, msg, "PnL: -$0.0190 (-0.08%) [35s] | Side: Long")
	assert.Contains(t, msg, "• Price: 0.013190 ➔ 0.013180 (-0.08%) | Size: 10.82 USDT")
	assert.Contains(t, msg, "• Order ID: 2282128112719258880")
	assert.Contains(t, msg, "• Client ID: client-filled-123")
	assert.Contains(t, msg, "• Req ID: req-filled-123")
}

type mockErrExchangeClient struct {
	mockExchangeClient
	createErr error
}

func (m *mockErrExchangeClient) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
	return exchange.CreateOrderResult{}, m.createErr
}

func TestOrderManager_SubmitError_PublishesAbort(t *testing.T) {
	t.Parallel()

	client := &mockErrExchangeClient{createErr: assert.AnError}
	bus := eventbus.New(slog.Default())
	repo := &mockTradeRepo{}
	noti := &mockNotifier{}
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
	mgr, err := futures.NewOrderManager(context.Background(), engine, bus, repo, noti, nil)
	assert.NoError(t, err)
	assert.NoError(t, mgr.Init(context.Background()))

	ctx := context.Background()
	reqID := "req-submit-err-001"
	watchReady := futures.OrderPositionWatchReadyEvent{
		ReqID:        reqID,
		Symbol:       "BTCUSDT",
		Exchange:     "mexc",
		StrategyType: futures.StrategyFundingReversion,
		Timestamp:    time.Now(),
		Side:         shared.SideOpenLong,
		OrderType:    futures.OrderTypeIOC,
		Price:        50000.0,
		Volume:       1.0,
	}

	_, submitErr := mgr.HandleExecuteOrder(ctx, watchReady)
	assert.Error(t, submitErr)
}

func TestHandlePositionUpdate_IgnoresZeroVolumeBeforeFill(t *testing.T) {
	t.Parallel()

	client := &mockExchangeClient{}
	bus := eventbus.New(slog.Default())
	repo := &mockTradeRepo{}
	noti := &mockNotifier{}
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
	mgr, err := futures.NewOrderManager(context.Background(), engine, bus, repo, noti, nil)
	assert.NoError(t, err)
	assert.NoError(t, mgr.Init(context.Background()))

	ctx := context.Background()
	reqID := "req-pos-zero-001"

	agg := mgr.GetAggregate(reqID)
	assert.NoError(t, agg.Record(futures.OrderPositionWatchReadyEvent{
		ReqID:        reqID,
		Symbol:       "BTCUSDT",
		Exchange:     "mexc",
		StrategyType: futures.StrategyFundingReversion,
		Side:         shared.SideOpenLong,
		Price:        50000.0,
		Volume:       1.0,
	}))

	posZero := exchange.PersonalPositionUpdate{
		Symbol:          "BTCUSDT",
		HoldVolContract: 0,
		HoldVolCoin:     0,
	}

	// Zero-volume position snapshot before fill should be ignored
	mgr.HandlePositionUpdate(ctx, reqID, posZero)
	assert.False(t, agg.HasFilled())
	assert.NotEqual(t, futures.StatePositionClosed, agg.State())

	// Position filled update
	posFilled := exchange.PersonalPositionUpdate{
		Symbol:          "BTCUSDT",
		HoldVolContract: 1.0,
		OpenAvgPrice:    50000.0,
	}
	mgr.HandlePositionUpdate(ctx, reqID, posFilled)
	assert.True(t, agg.HasFilled())

	// Once filled, position update with 0 volume triggers closed state
	mgr.HandlePositionUpdate(ctx, reqID, posZero)
	assert.Equal(t, futures.StatePositionClosed, agg.State())
}

func TestOrderManager_HandleEnrichAndComplete_Sleep(t *testing.T) {
	t.Parallel()

	// 1. Verify HandleEnrichAndComplete executes clock.Sleep with 30s on TimeSync
	var sleepCalls []time.Duration
	client := &mockExchangeClient{}
	ts := timesync.New(client, slog.Default(), time.Second)
	ts.SetSleeper(func(ctx context.Context, d time.Duration) error {
		sleepCalls = append(sleepCalls, d)
		return nil
	})

	bus := eventbus.New(slog.Default())
	repo := &mockTradeRepo{}
	noti := &mockNotifier{}
	engine := &app.Engine{
		Bus: bus,
		Providers: map[string]*app.ExchangeProvider{
			"mexc": {
				Name:     "mexc",
				Client:   client,
				TimeSync: ts,
			},
		},
	}
	mgr, err := futures.NewOrderManager(context.Background(), engine, bus, repo, noti, nil)
	assert.NoError(t, err)
	assert.NoError(t, mgr.Init(context.Background()))

	ctx := context.Background()
	completed, err := mgr.HandleEnrichAndComplete(ctx, "mexc", "req-sleep-001", "client-sleep-001", "BTCUSDT", futures.StrategyFundingReversion, futures.OutcomeFilled, "normal")
	assert.NoError(t, err)
	assert.Equal(t, "req-sleep-001", completed.GetReqID())
	assert.Len(t, sleepCalls, 1)
	assert.Equal(t, 30*time.Second, sleepCalls[0])

	// 2. Verify HandleEnrichAndComplete handles sleep error/cancellation gracefully
	var errSleepCalls []time.Duration
	tsErr := timesync.New(client, slog.Default(), time.Second)
	tsErr.SetSleeper(func(ctx context.Context, d time.Duration) error {
		errSleepCalls = append(errSleepCalls, d)
		return errors.New("context deadline exceeded")
	})

	engineErr := &app.Engine{
		Bus: bus,
		Providers: map[string]*app.ExchangeProvider{
			"mexc": {
				Name:     "mexc",
				Client:   client,
				TimeSync: tsErr,
			},
		},
	}
	mgrErr, err := futures.NewOrderManager(context.Background(), engineErr, bus, repo, noti, nil)
	assert.NoError(t, err)
	assert.NoError(t, mgrErr.Init(context.Background()))

	completedErr, err := mgrErr.HandleEnrichAndComplete(ctx, "mexc", "req-sleep-002", "client-sleep-002", "BTCUSDT", futures.StrategyFundingReversion, futures.OutcomeFilled, "normal")
	assert.NoError(t, err)
	assert.Equal(t, "req-sleep-002", completedErr.GetReqID())
	assert.Len(t, errSleepCalls, 1)
	assert.Equal(t, 30*time.Second, errSleepCalls[0])
}

func TestOrderManager_RegisterOnCompletedCallback_MultipleCallbacks(t *testing.T) {
	t.Parallel()

	client := &mockExchangeClient{}
	bus := eventbus.New(slog.Default())
	repo := &mockTradeRepo{}
	noti := &mockNotifier{}
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
	mgr, err := futures.NewOrderManager(context.Background(), engine, bus, repo, noti, nil)
	assert.NoError(t, err)
	assert.NoError(t, mgr.Init(context.Background()))

	var cb1Called, cb2Called atomic.Bool
	mgr.RegisterOnCompletedCallback(func(ctx context.Context, evt futures.OrderCompletedEvent) {
		if evt.GetReqID() == "req-multicb-001" {
			cb1Called.Store(true)
		}
	})
	mgr.RegisterOnCompletedCallback(func(ctx context.Context, evt futures.OrderCompletedEvent) {
		if evt.GetReqID() == "req-multicb-001" {
			cb2Called.Store(true)
		}
	})
	// Nil callback should be ignored safely
	mgr.RegisterOnCompletedCallback(nil)

	ctx := context.Background()
	_, err = mgr.HandleEnrichAndComplete(ctx, "mexc", "req-multicb-001", "client-multicb-001", "BTCUSDT", futures.StrategyFundingReversion, futures.OutcomeFilled, "normal")
	assert.NoError(t, err)

	// Trigger completed event through eventbus
	err = bus.Publish(futures.TopicOrderCompleted, futures.OrderCompletedEvent{
		ReqID: "req-multicb-001",
	})
	assert.NoError(t, err)

	assert.Eventually(t, func() bool {
		return cb1Called.Load() && cb2Called.Load()
	}, 1*time.Second, 10*time.Millisecond)
}

func TestOrderCompletedEvent_PropagatesRefID(t *testing.T) {
	t.Parallel()

	client := &mockExchangeClient{}
	bus := eventbus.New(slog.Default())
	repo := &mockTradeRepo{}
	noti := &mockNotifier{}
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
	mgr, err := futures.NewOrderManager(context.Background(), engine, bus, repo, noti, nil)
	assert.NoError(t, err)
	assert.NoError(t, mgr.Init(context.Background()))

	intent := futures.OrderIntentEvent{
		ReqID: "req-ref-001",
		RefID: "orig-trade-999",
	}
	assert.NoError(t, mgr.GetAggregate(intent.GetReqID()).Record(intent))

	completed, err := mgr.HandleEnrichAndComplete(
		context.Background(),
		"mexc",
		"req-ref-001",
		"client-ref-001",
		"BTCUSDT",
		futures.StrategyObfuscator,
		futures.OutcomeFilled,
		"normal",
	)
	assert.NoError(t, err)
	assert.Equal(t, "orig-trade-999", completed.RefID)
	assert.Equal(t, "req-ref-001", completed.ReqID)
	assert.Equal(t, futures.StrategyObfuscator, completed.StrategyType)
}

func TestHandlePreFlight_CloseOrder_SkipsModeSwitches(t *testing.T) {
	t.Parallel()

	client := &mockExchangeClient{}
	bus := eventbus.New(slog.Default())
	repo := &mockTradeRepo{}
	noti := &mockNotifier{}
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
	mgr, err := futures.NewOrderManager(context.Background(), engine, bus, repo, noti, nil)
	assert.NoError(t, err)
	assert.NoError(t, mgr.Init(context.Background()))

	intent := futures.OrderIntentEvent{
		ReqID:        "req-close-001",
		Symbol:       "BTCUSDT",
		Exchange:     "MEXC",
		StrategyType: futures.StrategyDilution,
		Timestamp:    time.Now(),
		Side:         shared.SideCloseLong,
		OrderType:    futures.OrderTypePostOnly,
		Price:        50000.0,
		Volume:       1.0,
		MarginMode:   shared.MarginModeIsolated,
		PositionMode: shared.PositionModeHedge,
		Leverage:     10,
	}

	preflight, err := mgr.HandlePreFlight(context.Background(), intent)
	assert.NoError(t, err)
	assert.Equal(t, 10, preflight.AdjustedLeverage)
	// Must NOT call switch margin mode, switch position mode, or change leverage for closing orders
	assert.False(t, client.marginModeSwitched, "SwitchMarginMode should be skipped for close orders")
	assert.False(t, client.positionModeSwitched, "SwitchPositionMode should be skipped for close orders")
	assert.False(t, client.leverageChanged, "ChangeLeverage should be skipped for close orders")
}

func TestOrderManager_CancelOrder(t *testing.T) {
	t.Parallel()
	bus := eventbus.New(slog.Default())
	client := &mockExchangeClient{}
	repo := &mockTradeRepo{}
	noti := &mockNotifier{}

	engine := &app.Engine{
		Providers: map[string]*app.ExchangeProvider{
			"mexc": {
				Client:   client,
				TimeSync: newTestTimeSync(client),
			},
		},
	}
	mgr, err := futures.NewOrderManager(context.Background(), engine, bus, repo, noti, nil)
	assert.NoError(t, err)
	assert.NoError(t, mgr.Init(context.Background()))

	agg := mgr.GetAggregate("req-cancel-001")
	err = agg.Record(futures.OrderSubmittedEvent{
		ReqID:        "req-cancel-001",
		Symbol:       "BTCUSDT",
		Exchange:     "MEXC",
		StrategyType: futures.StrategyDilution,
		OrderType:    futures.OrderTypePostOnly,
		OrderID:      "order-to-cancel-999",
	})
	assert.NoError(t, err)

	assert.True(t, mgr.HasActiveOrder("req-cancel-001"))
	activeOrders := mgr.GetActiveOrders("MEXC", "BTCUSDT")
	assert.Len(t, activeOrders, 1)

	err = mgr.CancelOrder(context.Background(), "req-cancel-001")
	assert.NoError(t, err)
	assert.True(t, client.cancelOrderCalled)
	assert.False(t, mgr.HasActiveOrder("req-cancel-001"))
	assert.Equal(t, futures.StateCanceled, agg.State())
}

func TestOrderManager_CancelOpenOrders(t *testing.T) {
	t.Parallel()
	bus := eventbus.New(slog.Default())
	client := &mockExchangeClient{}
	repo := &mockTradeRepo{}
	noti := &mockNotifier{}

	engine := &app.Engine{
		Providers: map[string]*app.ExchangeProvider{
			"mexc": {
				Client:   client,
				TimeSync: newTestTimeSync(client),
			},
		},
	}
	mgr, err := futures.NewOrderManager(context.Background(), engine, bus, repo, noti, nil)
	assert.NoError(t, err)
	assert.NoError(t, mgr.Init(context.Background()))

	err = mgr.CancelOpenOrders(context.Background(), "MEXC", "BTCUSDT")
	assert.NoError(t, err)
	assert.True(t, client.cancelAllCalled)
}

func TestOrderManager_RestingTimeoutAutoCancel(t *testing.T) {
	t.Parallel()
	bus := eventbus.New(slog.Default())
	client := &mockExchangeClient{}
	repo := &mockTradeRepo{}
	noti := &mockNotifier{}

	engine := &app.Engine{
		Providers: map[string]*app.ExchangeProvider{
			"mexc": {
				Client:   client,
				TimeSync: newTestTimeSync(client),
			},
		},
	}
	mgr, err := futures.NewOrderManager(context.Background(), engine, bus, repo, noti, nil)
	assert.NoError(t, err)
	assert.NoError(t, mgr.Init(context.Background()))

	submittedEvt := futures.OrderSubmittedEvent{
		ReqID:                 "req-resting-timeout-001",
		Symbol:                "BTCUSDT",
		Exchange:              "mexc",
		StrategyType:          futures.StrategyDilution,
		OrderType:             futures.OrderTypePostOnly,
		UnfilledCancelTimeout: 20 * time.Millisecond,
		OrderID:               "order-resting-123",
	}

	agg := mgr.GetAggregate("req-resting-timeout-001")
	assert.NoError(t, agg.Record(submittedEvt))

	err = mgr.HandleScheduleUnfilledCancelTimeout(context.Background(), submittedEvt)
	assert.NoError(t, err)

	// Wait for resting timeout to fire
	time.Sleep(50 * time.Millisecond)

	assert.True(t, client.cancelOrderCalled, "Resting timeout expiration must call CancelOrder on exchange")
	assert.Equal(t, futures.StateCompleted, agg.State())
}

func TestOrderManager_SkipPreFlight(t *testing.T) {
	t.Parallel()

	client := &mockExchangeClient{}
	bus := eventbus.New(slog.Default())
	ctx := context.Background()
	repo := &mockTradeRepo{}
	noti := &mockNotifier{}
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
	mgr, err := futures.NewOrderManager(ctx, engine, bus, repo, noti, nil)
	assert.NoError(t, err)
	assert.NoError(t, mgr.Init(ctx))

	intent := futures.OrderIntentEvent{
		ReqID:         "req-skip-pf-001",
		Symbol:        "BTCUSDT",
		Exchange:      "mexc",
		StrategyType:  futures.StrategyFundingReversion,
		Timestamp:     time.Now(),
		Side:          shared.SideOpenLong,
		OrderType:     futures.OrderTypeIOC,
		Price:         50000.0,
		Volume:        1.0,
		MarginMode:    shared.MarginModeIsolated,
		PositionMode:  shared.PositionModeOneWay,
		Leverage:      10,
		SkipPreFlight: true,
	}

	preflight, err := mgr.HandlePreFlight(ctx, intent)
	assert.NoError(t, err)
	assert.Equal(t, 10, preflight.AdjustedLeverage)
	assert.False(t, client.marginModeSwitched, "expected margin mode switch to be skipped")
	assert.False(t, client.positionModeSwitched, "expected position mode switch to be skipped")
	assert.False(t, client.leverageChanged, "expected leverage change to be skipped")
}

func TestIsConsecutiveDrop(t *testing.T) {
	t.Parallel()

	// Not enough elements
	assert.False(t, futures.IsConsecutiveDrop([]float64{10.0}, 2))
	assert.False(t, futures.IsConsecutiveDrop([]float64{10.0, 9.0}, 2))

	// 2 consecutive drops
	assert.True(t, futures.IsConsecutiveDrop([]float64{10.0, 9.0, 8.0}, 2))
	assert.True(t, futures.IsConsecutiveDrop([]float64{5.0, 10.0, 9.0, 8.0}, 2))

	// Only 1 drop followed by a bounce
	assert.False(t, futures.IsConsecutiveDrop([]float64{10.0, 9.0, 9.5}, 2))
	// Flat tick
	assert.False(t, futures.IsConsecutiveDrop([]float64{10.0, 9.0, 9.0}, 2))
	// Up tick
	assert.False(t, futures.IsConsecutiveDrop([]float64{10.0, 11.0, 12.0}, 2))
}

func TestHandlePositionUpdate_PnLTrailingStop_ImmediateExit(t *testing.T) {
	t.Parallel()

	client := &mockExchangeClient{}
	bus := eventbus.New(slog.Default())
	repo := &mockTradeRepo{}
	noti := &mockNotifier{}
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
	mgr, err := futures.NewOrderManager(context.Background(), engine, bus, repo, noti, nil)
	assert.NoError(t, err)
	assert.NoError(t, mgr.Init(context.Background()))

	ctx := context.Background()
	reqID := "req-pnl-trailing-001"

	agg := mgr.GetAggregate(reqID)
	assert.NoError(t, agg.Record(futures.OrderIntentEvent{
		ReqID:                   reqID,
		Symbol:                  "BTCUSDT",
		Exchange:                "mexc",
		StrategyType:            futures.StrategyFundingReversion,
		Side:                    shared.SideOpenLong,
		OrderType:               futures.OrderTypeIOC,
		Price:                   50000.0,
		Volume:                  1.0,
		EnablePnLTrailing:       true,
		PnLTrailingDropPct:      10.0, // 10% pullback from peak
		PnLTrailingConfirmTicks: 2,
	}))

	// Step 1: Initial fill
	mgr.HandlePositionUpdate(ctx, reqID, exchange.PersonalPositionUpdate{
		Symbol:          "BTCUSDT",
		HoldVolContract: 1.0,
		OpenAvgPrice:    50000.0,
	})

	assert.True(t, agg.HasFilled())

	// Step 1b: Trade price 50010.0 -> PnL = 10.0
	mgr.HandleTradeUpdate(ctx, reqID, "BTCUSDT", []shared.PublicTrade{
		{Symbol: "BTCUSDT", Price: 50010.0},
	})
	tracker := mgr.GetPnLTracker(reqID)
	assert.NotNil(t, tracker)
	assert.Equal(t, 10.0, tracker.MaxPnL)
	assert.Equal(t, []float64{10.0}, tracker.History)
	assert.False(t, client.allClosed)

	// Step 2: Growth to Price 50015.0 -> PnL = 15.0
	mgr.HandleTradeUpdate(ctx, reqID, "BTCUSDT", []shared.PublicTrade{
		{Symbol: "BTCUSDT", Price: 50015.0},
	})
	tracker = mgr.GetPnLTracker(reqID)
	assert.Equal(t, 15.0, tracker.MaxPnL)
	assert.Equal(t, []float64{10.0, 15.0}, tracker.History)
	assert.False(t, client.allClosed)

	// Step 3: 1st tick down to Price 50014.5 -> PnL = 14.5 (drop is 0.5, 3.3% < 10% threshold; only 1 down-tick)
	mgr.HandleTradeUpdate(ctx, reqID, "BTCUSDT", []shared.PublicTrade{
		{Symbol: "BTCUSDT", Price: 50014.5},
	})
	tracker = mgr.GetPnLTracker(reqID)
	assert.Equal(t, 15.0, tracker.MaxPnL)
	assert.Equal(t, []float64{10.0, 15.0, 14.5}, tracker.History)
	assert.False(t, client.allClosed, "should not exit on 3.3% drop and only 1 down-tick")

	// Step 4: 2nd tick down to Price 50013.5 -> PnL = 13.5 (drop is 1.5, exactly 10% pullback from 15.0 peak and 2 consecutive drops) -> triggers immediate bailout exit!
	mgr.HandleTradeUpdate(ctx, reqID, "BTCUSDT", []shared.PublicTrade{
		{Symbol: "BTCUSDT", Price: 50013.5},
	})
	assert.True(t, client.allClosed, "expected CloseAllPositions to be called immediately on 10% pullback with 2 down-ticks")
	assert.Equal(t, futures.StateBailout, agg.State())

	// Step 5: Position closed confirmation (holdVol = 0)
	mgr.HandlePositionUpdate(ctx, reqID, exchange.PersonalPositionUpdate{
		Symbol:          "BTCUSDT",
		HoldVolContract: 0.0,
		HoldVolCoin:     0.0,
		CloseProfitLoss: 14.0,
	})
	assert.Nil(t, mgr.GetPnLTracker(reqID), "tracker should be cleaned up on close")
	assert.Equal(t, futures.StatePositionClosed, agg.State())
}

func TestHandlePositionUpdate_PnLTrailing_DisabledByDefault(t *testing.T) {
	t.Parallel()

	client := &mockExchangeClient{}
	bus := eventbus.New(slog.Default())
	repo := &mockTradeRepo{}
	noti := &mockNotifier{}
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
	mgr, err := futures.NewOrderManager(context.Background(), engine, bus, repo, noti, nil)
	assert.NoError(t, err)
	assert.NoError(t, mgr.Init(context.Background()))

	ctx := context.Background()
	reqID := "req-pnl-disabled-001"

	agg := mgr.GetAggregate(reqID)
	// EnablePnLTrailing is false by default
	assert.NoError(t, agg.Record(futures.OrderIntentEvent{
		ReqID:        reqID,
		Symbol:       "BTCUSDT",
		Exchange:     "mexc",
		StrategyType: futures.StrategyFundingReversion,
		Side:         shared.SideOpenLong,
		OrderType:    futures.OrderTypeIOC,
		Price:        50000.0,
		Volume:       1.0,
	}))

	mgr.HandlePositionUpdate(ctx, reqID, exchange.PersonalPositionUpdate{
		Symbol:          "BTCUSDT",
		HoldVolContract: 1.0,
		OpenAvgPrice:    50000.0,
	})
	// Drop
	mgr.HandleTradeUpdate(ctx, reqID, "BTCUSDT", []shared.PublicTrade{{Symbol: "BTCUSDT", Price: 50005.0}})
	mgr.HandleTradeUpdate(ctx, reqID, "BTCUSDT", []shared.PublicTrade{{Symbol: "BTCUSDT", Price: 50002.0}})

	assert.False(t, client.allClosed, "should not trigger close when trailing is disabled")
	assert.Nil(t, mgr.GetPnLTracker(reqID))
}

func TestHandleTradeUpdate_PnLTrailingStop_ViaDealStream(t *testing.T) {
	t.Parallel()

	logger := slog.Default()
	bus := eventbus.New(logger)
	defer func() { _ = bus.Close() }()
	repo := &mockTradeRepo{}
	client := &mockExchangeClient{}
	noti := &mockNotifier{}
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
	mgr, err := futures.NewOrderManager(context.Background(), engine, bus, repo, noti, nil)
	assert.NoError(t, err)
	assert.NoError(t, mgr.Init(context.Background()))

	ctx := context.Background()
	reqID := "req-deal-stream-001"

	agg := mgr.GetAggregate(reqID)
	assert.NoError(t, agg.Record(futures.OrderIntentEvent{
		ReqID:                   reqID,
		Symbol:                  "BTCUSDT",
		Exchange:                "mexc",
		StrategyType:            futures.StrategyFundingReversion,
		Side:                    shared.SideOpenShort, // Short position
		OrderType:               futures.OrderTypeIOC,
		Price:                   50000.0,
		Volume:                  1.0,
		ContractSize:            1.0,
		EnablePnLTrailing:       true,
		PnLTrailingDropPct:      10.0, // 10% pullback
		PnLTrailingConfirmTicks: 2,
	}))

	// Mark order as filled at 50,000.0
	assert.NoError(t, agg.Record(futures.OrderFilledEvent{
		ReqID:           reqID,
		Symbol:          "BTCUSDT",
		Exchange:        "mexc",
		Side:            shared.SideOpenShort,
		FillPrice:       50000.0,
		FillVolCoin:     1.0,
		FillVolContract: 1.0,
	}))
	assert.True(t, agg.HasFilled())

	// Deal 1: Market trade at 49,990.0 (profit +10 for short)
	mgr.HandleTradeUpdate(ctx, reqID, "BTCUSDT", []shared.PublicTrade{
		{Symbol: "BTCUSDT", Price: 49990.0, Volume: 0.5},
	})
	tracker := mgr.GetPnLTracker(reqID)
	assert.NotNil(t, tracker)
	assert.Equal(t, 10.0, tracker.MaxPnL)
	assert.Equal(t, []float64{10.0}, tracker.History)
	assert.False(t, client.allClosed)

	// Deal 2: Market trade drops to 49,980.0 (profit increases to +20)
	mgr.HandleTradeUpdate(ctx, reqID, "BTCUSDT", []shared.PublicTrade{
		{Symbol: "BTCUSDT", Price: 49980.0, Volume: 1.0},
	})
	tracker = mgr.GetPnLTracker(reqID)
	assert.Equal(t, 20.0, tracker.MaxPnL)
	assert.Equal(t, []float64{10.0, 20.0}, tracker.History)
	assert.False(t, client.allClosed)

	// Deal 3: Price bounces to 49,981.0 (PnL drops to 19.0; 5% drop < 10% threshold; 1 down-tick)
	mgr.HandleTradeUpdate(ctx, reqID, "BTCUSDT", []shared.PublicTrade{
		{Symbol: "BTCUSDT", Price: 49981.0, Volume: 0.2},
	})
	tracker = mgr.GetPnLTracker(reqID)
	assert.Equal(t, 20.0, tracker.MaxPnL)
	assert.Equal(t, []float64{10.0, 20.0, 19.0}, tracker.History)
	assert.False(t, client.allClosed, "should not exit on 5% drop with only 1 down-tick")

	// Deal 4: Price bounces further to 49,983.0 (PnL drops to 17.0; 15% drop >= 10% threshold AND 2 consecutive down-ticks)
	mgr.HandleTradeUpdate(ctx, reqID, "BTCUSDT", []shared.PublicTrade{
		{Symbol: "BTCUSDT", Price: 49983.0, Volume: 0.3},
	})
	assert.True(t, client.allClosed, "expected immediate bailout exit via deal stream trailing stop")
	assert.Equal(t, futures.StateBailout, agg.State())

	// Deal 5: Position closed confirmation
	mgr.HandlePositionUpdate(ctx, reqID, exchange.PersonalPositionUpdate{
		Symbol:          "BTCUSDT",
		HoldVolContract: 0,
		HoldVolCoin:     0,
		CloseProfitLoss: 17.0,
	})
	assert.Nil(t, mgr.GetPnLTracker(reqID), "tracker should be cleaned up on position close")
}
