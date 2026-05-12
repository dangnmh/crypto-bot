package timesync_test

import (
	"context"
	"errors"
	"testing"

	"crypto-bot/internal/infrastructure/timesync"
	"time"

	"crypto-bot/internal/infrastructure/exchange"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockClient implements exchange.Client for timesync tests.
type mockClient struct {
	serverTime int64
	latency    time.Duration
	err        error
}

func (m *mockClient) GetServerTime(_ context.Context) (int64, error) {
	if m.latency > 0 {
		time.Sleep(m.latency)
	}
	if m.err != nil {
		return 0, m.err
	}
	return m.serverTime, nil
}

// Stub remaining Client interface methods.
func (m *mockClient) GetTickers(_ context.Context, _ string) ([]exchange.Ticker, error) {
	return nil, nil
}
func (m *mockClient) GetContractDetails(_ context.Context) ([]exchange.ContractDetail, error) {
	return nil, nil
}
func (m *mockClient) GetFundingRate(_ context.Context, _ string) (*exchange.FundingRateDetail, error) {
	return nil, nil
}
func (m *mockClient) GetKlines(_ context.Context, _, _ string, _, _ int64) ([]exchange.Kline, error) {
	return nil, nil
}
func (m *mockClient) CreateOrder(_ context.Context, _ exchange.SubmitOrderRequest) (string, error) {
	return "", nil
}
func (m *mockClient) CreateTrackOrder(_ context.Context, _ exchange.SubmitTrackOrderRequest) (string, error) {
	return "", nil
}
func (m *mockClient) CancelOrder(_ context.Context, _, _ string) error      { return nil }
func (m *mockClient) CancelOrders(_ context.Context, _ []string) error      { return nil }
func (m *mockClient) CancelAllOpenOrders(_ context.Context, _ string) error { return nil }
func (m *mockClient) GetOrder(_ context.Context, _ string) (*exchange.OrderInfo, error) {
	return nil, nil
}
func (m *mockClient) GetOpenOrders(_ context.Context, _ string) ([]exchange.OrderInfo, error) {
	return nil, nil
}
func (m *mockClient) CloseAllPositions(_ context.Context, _ string) error { return nil }
func (m *mockClient) ChangeLeverage(_ context.Context, _ exchange.ChangeLeverageRequest) error {
	return nil
}
func (m *mockClient) GetAssets(_ context.Context) ([]exchange.AssetInfo, error) { return nil, nil }
func (m *mockClient) GetAssetByCurrency(_ context.Context, _ string) (*exchange.AssetInfo, error) {
	return nil, nil
}
func (m *mockClient) GetOpenPositions(_ context.Context, _ string) ([]exchange.Position, error) {
	return nil, nil
}
func (m *mockClient) GetDepthSnapshot(_ context.Context, _ string, _ int) (*exchange.OrderBook, error) {
	return nil, nil
}
func (m *mockClient) GetDepthCommits(_ context.Context, _ string, _ int) ([]exchange.DepthCommit, error) {
	return nil, nil
}
func (m *mockClient) WarmUp(_ context.Context, _ time.Duration) {}

func TestTimeSync_GetServerTime(t *testing.T) {
	t.Parallel()

	mc := &mockClient{serverTime: time.Now().UnixMilli() + 100}
	ts := timesync.New(mc, 10*time.Second)

	serverTime := ts.GetServerTime()
	localTime := time.Now().UnixMilli()

	// Before sync — offset is 0, so GetServerTime ≈ local time.
	assert.InDelta(t, localTime, serverTime, 50, "before sync: server time too far from local")
}

func TestTimeSync_SyncOnce(t *testing.T) {
	t.Parallel()

	mc := &mockClient{serverTime: time.Now().UnixMilli() + 500}
	ts := timesync.New(mc, 10*time.Second)

	ts.SyncOnceForTest(context.Background())

	offset := ts.Offset()
	assert.InDelta(t, 500, offset, 100)

	latency := ts.LatencyMs()
	assert.GreaterOrEqual(t, latency, int64(0))
}

func TestTimeSync_SyncOnce_Error(t *testing.T) {
	t.Parallel()

	mc := &mockClient{err: errors.New("network error")}
	ts := timesync.New(mc, 10*time.Second)

	ts.SyncOnceForTest(context.Background())
	assert.False(t, ts.IsHealthy(), "expected unhealthy after sync error")
}

func TestTimeSync_IsHealthy(t *testing.T) {
	t.Parallel()

	mc := &mockClient{serverTime: time.Now().UnixMilli()}
	ts := timesync.New(mc, 10*time.Second)

	assert.False(t, ts.IsHealthy(), "expected unhealthy before first sync")

	ts.SyncOnceForTest(context.Background())
	assert.True(t, ts.IsHealthy(), "expected healthy after sync")
}

func TestTimeSync_Until(t *testing.T) {
	t.Parallel()

	mc := &mockClient{serverTime: time.Now().UnixMilli()}
	ts := timesync.New(mc, 10*time.Second)
	ts.SyncOnceForTest(context.Background())

	target := time.Now().Add(5 * time.Second)
	d := ts.Until(target)

	assert.InDelta(t, float64(5*time.Second), float64(d), float64(500*time.Millisecond))
}

func TestTimeSync_Now(t *testing.T) {
	t.Parallel()

	mc := &mockClient{serverTime: time.Now().UnixMilli()}
	ts := timesync.New(mc, 10*time.Second)
	ts.SyncOnceForTest(context.Background())

	now := ts.Now()
	diff := time.Since(now)

	assert.InDelta(t, float64(0), float64(diff), float64(time.Second))
}

func TestTimeSync_MsUntilTarget(t *testing.T) {
	t.Parallel()

	mc := &mockClient{serverTime: time.Now().UnixMilli()}
	ts := timesync.New(mc, 10*time.Second)
	ts.SyncOnceForTest(context.Background())

	targetMs := time.Now().Add(3 * time.Second).UnixMilli()
	ms := ts.MsUntilTarget(targetMs)

	assert.InDelta(t, 3000, ms, 500)
}

func TestTimeSync_WaitReady(t *testing.T) {
	t.Parallel()

	mc := &mockClient{serverTime: time.Now().UnixMilli()}
	ts := timesync.New(mc, 10*time.Second)

	go func() {
		time.Sleep(50 * time.Millisecond)
		ts.SyncOnceForTest(context.Background())
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ts.WaitReady(ctx)
	require.NoError(t, ctx.Err(), "expected WaitReady to complete, but context timed out")
}
