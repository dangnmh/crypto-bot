package timesync_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/timesync"
	"crypto-bot/internal/testutil/mocks"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestTimeSync_StartAndAccessors(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	client.EXPECT().GetServerTime(gomock.Any()).DoAndReturn(func(context.Context) (int64, error) {
		return time.Now().Add(25 * time.Millisecond).UnixMilli(), nil
	}).AnyTimes()

	ts := timesync.New(client, slog.Default(), 10*time.Millisecond)
	ctx := t.Context()

	go ts.Start(ctx)
	assert.NoError(t, ts.WaitReady(ctx))

	assert.NotZero(t, ts.GetServerTime())
	assert.NotZero(t, ts.Now())
	assert.LessOrEqual(t, ts.LatencyMs(), int64(100))
	assert.True(t, ts.IsHealthy())
	assert.NotZero(t, ts.Offset())
	assert.Less(t, ts.MsUntilTarget(time.Now().Add(time.Second).UnixMilli()), int64(time.Second*2/time.Millisecond))
	assert.Greater(t, ts.Until(time.Now().Add(50*time.Millisecond)), time.Duration(0))
}

func TestTimeSync_WaitReadyAndSleepCancel(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	ts := timesync.New(mocks.NewMockClient(ctrl), slog.Default(), time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	assert.ErrorIs(t, ts.WaitReady(ctx), context.Canceled)
	assert.ErrorIs(t, ts.Sleep(ctx, time.Second), context.Canceled)
	assert.ErrorIs(t, ts.PrecisionSleepUntil(ctx, time.Now().Add(time.Second)), context.Canceled)
}

func TestTimeSync_PrecisionSleepUntil(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	ts := timesync.New(mocks.NewMockClient(ctrl), slog.Default(), time.Second)

	// Test immediate past target
	assert.NoError(t, ts.PrecisionSleepUntil(t.Context(), time.Now().Add(-time.Second)))

	// Test short duration spin-wait
	start := time.Now()
	target := start.Add(5 * time.Millisecond)
	assert.NoError(t, ts.PrecisionSleepUntil(t.Context(), target))
	assert.True(t, time.Now().After(target) || time.Now().Equal(target))
}

func TestTimeSync_RollingMinRTT(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)

	callCount := 0
	delays := []time.Duration{
		40 * time.Millisecond,
		15 * time.Millisecond, // minimum
		60 * time.Millisecond,
	}

	client.EXPECT().GetServerTime(gomock.Any()).DoAndReturn(func(context.Context) (int64, error) {
		d := delays[callCount%len(delays)]
		callCount++
		time.Sleep(d)
		return time.Now().UnixMilli(), nil
	}).Times(3)

	ts := timesync.New(client, slog.Default(), time.Minute)
	ctx := t.Context()

	// 1st sync (40ms)
	ts.SyncNow(ctx)
	assert.GreaterOrEqual(t, ts.LatencyMs(), int64(35))
	assert.Equal(t, ts.LatencyMs(), ts.LastLatencyMs())

	// 2nd sync (15ms -> new minimum)
	ts.SyncNow(ctx)
	assert.GreaterOrEqual(t, ts.MinRTTMs(), int64(14))
	assert.LessOrEqual(t, ts.MinRTTMs(), int64(30))

	// 3rd sync (60ms -> spike, minRTT should stay around ~15ms, median should be ~40ms)
	ts.SyncNow(ctx)
	assert.GreaterOrEqual(t, ts.LastLatencyMs(), int64(55))
	assert.LessOrEqual(t, ts.MinRTTMs(), int64(30))
	assert.GreaterOrEqual(t, ts.MedianRTTMs(), int64(35))
	assert.LessOrEqual(t, ts.MedianRTTMs(), int64(45))
}

type mockTradeModeClient struct {
	*mocks.MockClient
	tradeMode exchange.TradeMode
	wsExec    exchange.WSTradeExecutor
}

func (m *mockTradeModeClient) SetTradeMode(mode exchange.TradeMode) { m.tradeMode = mode }
func (m *mockTradeModeClient) TradeMode() exchange.TradeMode        { return m.tradeMode }
func (m *mockTradeModeClient) SetWSTradeExecutor(e exchange.WSTradeExecutor) {
	m.wsExec = e
}
func (m *mockTradeModeClient) WSTradeExecutor() exchange.WSTradeExecutor { return m.wsExec }

type mockWSExec struct {
	latency int64
	pinged  bool
}

func (m *mockWSExec) Start(ctx context.Context) {}
func (m *mockWSExec) IsReady() bool             { return true }
func (m *mockWSExec) Close()                    {}
func (m *mockWSExec) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
	return exchange.CreateOrderResult{}, nil
}
func (m *mockWSExec) CancelOrder(ctx context.Context, symbol, orderID string) error {
	return nil
}
func (m *mockWSExec) PrepareOrder(ctx context.Context, req exchange.SubmitOrderRequest) (func(context.Context) (exchange.CreateOrderResult, error), error) {
	return nil, nil
}
func (m *mockWSExec) LatencyMs() int64       { return m.latency }
func (m *mockWSExec) LastLatencyMs() int64   { return m.latency }
func (m *mockWSExec) MinLatencyMs() int64    { return m.latency }
func (m *mockWSExec) MedianLatencyMs() int64 { return m.latency }
func (m *mockWSExec) Ping(ctx context.Context) error {
	m.pinged = true
	return nil
}

func TestTimeSync_TradeMode_LatencyRouting(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	baseMock := mocks.NewMockClient(ctrl)
	baseMock.EXPECT().GetServerTime(gomock.Any()).DoAndReturn(func(context.Context) (int64, error) {
		time.Sleep(50 * time.Millisecond) // HTTP RTT ~50ms
		return time.Now().UnixMilli(), nil
	}).AnyTimes()

	wsExec := &mockWSExec{latency: 8} // WS RTT = 8ms
	tmClient := &mockTradeModeClient{
		MockClient: baseMock,
		tradeMode:  exchange.TradeModeHTTP,
		wsExec:     wsExec,
	}

	ts := timesync.New(tmClient, slog.Default(), time.Minute)
	ctx := t.Context()
	ts.SyncNow(ctx)

	// In HTTP mode: returns HTTP latency (~50ms)
	assert.GreaterOrEqual(t, ts.LatencyMs(), int64(40))
	assert.GreaterOrEqual(t, ts.HTTPLatencyMs(), int64(40))
	assert.GreaterOrEqual(t, ts.HTTPMedianRTTMs(), int64(40))
	assert.GreaterOrEqual(t, ts.HTTPMinRTTMs(), int64(40))
	assert.GreaterOrEqual(t, ts.MinRTTMs(), int64(40))
	assert.GreaterOrEqual(t, ts.MedianRTTMs(), int64(40))
	assert.GreaterOrEqual(t, ts.LastHTTPLatencyMs(), int64(40))
	assert.Equal(t, int64(8), ts.WSLatencyMs())
	assert.Equal(t, int64(8), ts.WSLastLatencyMs())
	assert.Equal(t, int64(8), ts.WSMinRTTMs())
	assert.Equal(t, int64(8), ts.WSMedianRTTMs())
	assert.GreaterOrEqual(t, ts.LatencyForMode(exchange.TradeModeHTTP), int64(40))
	assert.Equal(t, int64(8), ts.LatencyForMode(exchange.TradeModeWS))

	// Switch to WS mode: returns WS executor latency (8ms)
	tmClient.SetTradeMode(exchange.TradeModeWS)
	assert.Equal(t, int64(8), ts.LatencyMs())
	assert.Equal(t, int64(8), ts.MinRTTMs())
	assert.Equal(t, int64(8), ts.MedianRTTMs())

	// If WS executor reports -1 (unmeasured), fallback to HTTP latency
	wsExec.latency = -1
	assert.GreaterOrEqual(t, ts.LatencyMs(), int64(40))
	assert.GreaterOrEqual(t, ts.LatencyForMode(exchange.TradeModeWS), int64(40))
	wsExec.latency = 8

	// SyncNow triggers ping on WS executor
	assert.False(t, wsExec.pinged)
	ts.SyncNow(ctx)
	assert.True(t, wsExec.pinged)
}
