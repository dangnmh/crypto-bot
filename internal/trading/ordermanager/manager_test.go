package ordermanager_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	shared "crypto-bot/internal/domain"
	app "crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/notifier"
	"crypto-bot/internal/infrastructure/timesync"
	"crypto-bot/internal/trading/ordermanager"
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

	if _, err := ordermanager.NewOrderManager(ctx, nil, nil, nil, nil, nil); err == nil {
		t.Errorf("expected error when bus is nil")
	}
	bus := eventbus.New(slog.Default())
	engine := &app.Engine{}
	repo := &mockTradeRepo{}
	noti := &mockNotifier{}
	if mgr, err := ordermanager.NewOrderManager(ctx, engine, bus, repo, noti, nil); err != nil || mgr == nil {
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
	mgr, err := ordermanager.NewOrderManager(ctx, engine, bus, repo, noti, nil)
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

func runScheduleTimeoutTest(t *testing.T, mgr *ordermanager.OrderManager) ordermanager.OrderTimeoutScheduledEvent {
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

func runEnrichAndCompleteTest(t *testing.T, ctx context.Context, mgr *ordermanager.OrderManager) {
	completed, err := mgr.HandleEnrichAndComplete(ctx, "mexc", "req-001", "client-oid-001", "BTCUSDT", ordermanager.StrategyFundingReversion, "filled", "normal")
	if err != nil {
		t.Fatalf("HandleEnrichAndComplete failed: %v", err)
	}
	if completed.GetReqID() != "req-001" || completed.Outcome != "filled" || completed.StrategyType != ordermanager.StrategyFundingReversion {
		t.Errorf("unexpected completed event properties: %+v", completed)
	}
}

func TestOrderSubmittedEvent_GetNotifyMessage(t *testing.T) {
	t.Parallel()

	evt := ordermanager.OrderSubmittedEvent{
		OrderPositionWatchReadyEvent: ordermanager.OrderPositionWatchReadyEvent{
			OrderFireWindowReachedEvent: ordermanager.OrderFireWindowReachedEvent{
				OrderPreFlightCompletedEvent: ordermanager.OrderPreFlightCompletedEvent{
					OrderIntentEvent: ordermanager.OrderIntentEvent{
						BaseExecutionEvent: ordermanager.BaseExecutionEvent{
							ReqID:         "12082026170000PROMTOOBITFUTURES",
							ClientOrderID: "12082026170000PROMTOOBITFUTURES",
							Symbol:        "PROM-SWAP-USDT",
							Exchange:      "toobit_futures",
							StrategyType:  ordermanager.StrategyFundingReversion,
						},
						Leverage:     20,
						MarginUSDT:   30.0,
						FundingRate:  -0.021,
						Vol24hUSDT:   1200000.0,
						ContractSize: 1.0,
						Extra: map[string]any{
							"margin_usdt":  30.0,
							"funding_rate": -0.021,
							"vol_usdt_24h": 1200000.0,
						},
					},
				},
			},
		},
		OrderID: "2279963257363092992",
		Price:   3.032,
		Volume:  4002.638522,
	}

	msg := evt.GetNotifyMessage()
	assert.Contains(t, msg, "🟡 [FUNDING_REVERSION] [toobit_futures]")
	assert.Contains(t, msg, "• Symbol: PROM-SWAP-USDT")
	assert.Contains(t, msg, "• MarginUSD : $30.00 | Leverage: 20x | TotalUSD : $12,136.00")
	assert.Contains(t, msg, "• Price: 3.032000 | Size: 12,136.00 USDT")
	assert.Contains(t, msg, "• Vol24hUSD : $1.2M | FundingRate : -2.1%")
	assert.Contains(t, msg, "• Order ID: 2279963257363092992")
	assert.Contains(t, msg, "• Client ID: 12082026170000PROMTOOBITFUTURES")
	assert.Contains(t, msg, "• Req ID: 12082026170000PROMTOOBITFUTURES")
}

type mockNotifier struct {
	sentCount int
}

func (m *mockNotifier) Send(ctx context.Context, evt notifier.Event) error {
	m.sentCount++
	return nil
}

func (m *mockNotifier) SendRawMsg(ctx context.Context, msg string) error {
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
			mgr, err := ordermanager.NewOrderManager(context.Background(), engine, bus, repo, noti, nil)
			if err != nil {
				t.Fatalf("failed to create order manager: %v", err)
			}
			if err := mgr.Init(context.Background()); err != nil {
				t.Fatalf("failed to init order manager: %v", err)
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

func TestOrderAbortedEvent_Notification(t *testing.T) {
	t.Parallel()

	evt := ordermanager.OrderAbortedEvent{
		BaseExecutionEvent: ordermanager.BaseExecutionEvent{
			ReqID:         "req-abort-123",
			ClientOrderID: "client-abort-123",
			Symbol:        "BTCUSDT",
			Exchange:      "BINANCE",
			StrategyType:  ordermanager.StrategyFundingReversion,
		},
		OrderID:   "ex-order-999",
		Reason:    "submit_error",
		Error:     "insufficient margin",
		AbortedAt: time.Now(),
	}

	assert.Equal(t, ordermanager.TopicOrderAborted, evt.GetTopic())
	assert.True(t, evt.ShouldNotify())

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

	completedNormal := ordermanager.OrderCompletedEvent{Outcome: ordermanager.OutcomeFilled}
	assert.True(t, completedNormal.ShouldNotify())

	completedCanceled := ordermanager.OrderCompletedEvent{
		BaseExecutionEvent: ordermanager.BaseExecutionEvent{
			ReqID:         "req-canceled-123",
			ClientOrderID: "client-canceled-123",
			Symbol:        "BTCUSDT",
			Exchange:      "BINANCE",
			StrategyType:  ordermanager.StrategyFundingReversion,
		},
		OrderID: "ex-order-123",
		Outcome: ordermanager.OutcomeCanceledNoFill,
		Reason:  "user canceled",
	}
	assert.True(t, completedCanceled.ShouldNotify())
	msg := completedCanceled.GetNotifyMessage()
	assert.Contains(t, msg, "🔵 [FUNDING_REVERSION] [BINANCE] [CANCELED_NO_FILL]")
	assert.Contains(t, msg, "• Symbol: BTCUSDT")
	assert.Contains(t, msg, "• Outcome: canceled_no_fill")
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
	mgr, err := ordermanager.NewOrderManager(context.Background(), engine, bus, repo, noti, nil)
	assert.NoError(t, err)
	assert.NoError(t, mgr.Init(context.Background()))

	ctx := context.Background()
	reqID := "req-submit-err-001"
	watchReady := ordermanager.OrderPositionWatchReadyEvent{
		OrderFireWindowReachedEvent: ordermanager.OrderFireWindowReachedEvent{
			OrderPreFlightCompletedEvent: ordermanager.OrderPreFlightCompletedEvent{
				OrderIntentEvent: ordermanager.OrderIntentEvent{
					BaseExecutionEvent: ordermanager.BaseExecutionEvent{
						ReqID:        reqID,
						Symbol:       "BTCUSDT",
						Exchange:     "mexc",
						StrategyType: ordermanager.StrategyFundingReversion,
						Timestamp:    time.Now(),
					},
					Side:      shared.SideOpenLong,
					OrderType: ordermanager.OrderTypeIOC,
					Price:     50000.0,
					Volume:    1.0,
				},
			},
		},
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
	mgr, err := ordermanager.NewOrderManager(context.Background(), engine, bus, repo, noti, nil)
	assert.NoError(t, err)
	assert.NoError(t, mgr.Init(context.Background()))

	ctx := context.Background()
	reqID := "req-pos-zero-001"

	agg := mgr.GetAggregate(reqID)
	assert.NoError(t, agg.Record(ordermanager.OrderPositionWatchReadyEvent{
		OrderFireWindowReachedEvent: ordermanager.OrderFireWindowReachedEvent{
			OrderPreFlightCompletedEvent: ordermanager.OrderPreFlightCompletedEvent{
				OrderIntentEvent: ordermanager.OrderIntentEvent{
					BaseExecutionEvent: ordermanager.BaseExecutionEvent{
						ReqID:        reqID,
						Symbol:       "BTCUSDT",
						Exchange:     "mexc",
						StrategyType: ordermanager.StrategyFundingReversion,
					},
					Side:   shared.SideOpenLong,
					Price:  50000.0,
					Volume: 1.0,
				},
			},
		},
	}))

	posZero := exchange.PersonalPositionUpdate{
		Symbol:          "BTCUSDT",
		HoldVolContract: 0,
		HoldVolCoin:     0,
	}

	// Zero-volume position snapshot before fill should be ignored
	mgr.HandlePositionUpdate(ctx, reqID, posZero)
	assert.False(t, agg.HasFilled())
	assert.NotEqual(t, ordermanager.StatePositionClosed, agg.State())

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
	assert.Equal(t, ordermanager.StatePositionClosed, agg.State())
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
	mgr, err := ordermanager.NewOrderManager(context.Background(), engine, bus, repo, noti, nil)
	assert.NoError(t, err)
	assert.NoError(t, mgr.Init(context.Background()))

	ctx := context.Background()
	completed, err := mgr.HandleEnrichAndComplete(ctx, "mexc", "req-sleep-001", "client-sleep-001", "BTCUSDT", ordermanager.StrategyFundingReversion, ordermanager.OutcomeFilled, "normal")
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
	mgrErr, err := ordermanager.NewOrderManager(context.Background(), engineErr, bus, repo, noti, nil)
	assert.NoError(t, err)
	assert.NoError(t, mgrErr.Init(context.Background()))

	completedErr, err := mgrErr.HandleEnrichAndComplete(ctx, "mexc", "req-sleep-002", "client-sleep-002", "BTCUSDT", ordermanager.StrategyFundingReversion, ordermanager.OutcomeFilled, "normal")
	assert.NoError(t, err)
	assert.Equal(t, "req-sleep-002", completedErr.GetReqID())
	assert.Len(t, errSleepCalls, 1)
	assert.Equal(t, 30*time.Second, errSleepCalls[0])
}
