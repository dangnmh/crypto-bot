package ws_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/internal/infrastructure/ws"
	pkgws "crypto-bot/pkg/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAdapter struct {
	mu            sync.Mutex
	subCount      int
	unsubCount    int
	subTopics     []string
	unsubTopics   []string
	failSubscribe error
}

func (f *fakeAdapter) SubscribeTicker(ctx context.Context, sym string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failSubscribe != nil {
		return f.failSubscribe
	}
	f.subCount++
	f.subTopics = append(f.subTopics, "ticker:"+sym)
	return nil
}

func (f *fakeAdapter) UnsubscribeTicker(ctx context.Context, sym string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unsubCount++
	f.unsubTopics = append(f.unsubTopics, "ticker:"+sym)
	return nil
}

func (f *fakeAdapter) SubscribePersonal(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failSubscribe != nil {
		return f.failSubscribe
	}
	f.subCount++
	f.subTopics = append(f.subTopics, "personal")
	return nil
}

func (f *fakeAdapter) UnsubscribePersonal(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unsubCount++
	f.unsubTopics = append(f.unsubTopics, "personal")
	return nil
}

func (f *fakeAdapter) SetPool(*pkgws.Pool)                            {}
func (f *fakeAdapter) GetPingConfig() (any, time.Duration)            { return nil, 0 }
func (f *fakeAdapter) GetAuthHook(string, string) func(*pkgws.Client) { return nil }
func (f *fakeAdapter) GetChannelExtractor() func([]byte) string       { return nil }
func (f *fakeAdapter) ParseTicker([]byte) (string, *store.PriceData, error) {
	return "", nil, nil
}
func (f *fakeAdapter) ParsePosition([]byte) (*exchange.PersonalPositionUpdate, error) {
	return nil, nil
}
func (f *fakeAdapter) ParseDepth([]byte) (string, *domain.OrderBook, error) {
	return "BTCUSDT", &domain.OrderBook{Symbol: "BTCUSDT"}, nil
}
func (f *fakeAdapter) SubscribeDepth(ctx context.Context, sym string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failSubscribe != nil {
		return f.failSubscribe
	}
	f.subCount++
	f.subTopics = append(f.subTopics, "depth:"+sym)
	return nil
}

func (f *fakeAdapter) UnsubscribeDepth(ctx context.Context, sym string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unsubCount++
	f.unsubTopics = append(f.unsubTopics, "depth:"+sym)
	return nil
}

func (f *fakeAdapter) SubscribePublic(ctx context.Context, topic string, msg any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failSubscribe != nil {
		return f.failSubscribe
	}
	f.subCount++
	f.subTopics = append(f.subTopics, topic)
	return nil
}

func (f *fakeAdapter) UnsubscribePublic(ctx context.Context, topic string, msg any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unsubCount++
	f.unsubTopics = append(f.unsubTopics, topic)
	return nil
}

func verifyDualSubscriptionLifecycle(
	t *testing.T,
	subFunc func(flow string) error,
	unsubFunc func(flow string) error,
	adapter *fakeAdapter,
) {
	t.Helper()

	require.NoError(t, subFunc("flow1"))
	assert.Equal(t, 1, adapter.subCount)

	// Second subscriber for same resource does not increment physical subscribe
	require.NoError(t, subFunc("flow2"))
	assert.Equal(t, 1, adapter.subCount)

	// First unsubscribe decrements refcount without dropping physical subscription
	require.NoError(t, unsubFunc("flow1"))
	assert.Equal(t, 0, adapter.unsubCount)

	// Second unsubscribe drops refcount to 0, triggering physical unsubscribe
	require.NoError(t, unsubFunc("flow2"))
	assert.Equal(t, 1, adapter.unsubCount)
}

func TestExchangeManagerAdapter_ReferenceCounting(t *testing.T) {
	t.Parallel()

	adapter := &fakeAdapter{}
	mgr := ws.NewExchangeManagerAdapter(adapter)
	ctx := context.Background()

	// Empty flowID or topic is a no-op
	require.NoError(t, mgr.Subscribe(ctx, "", "topic1", nil))
	require.NoError(t, mgr.Subscribe(ctx, "flow1", "", nil))

	verifyDualSubscriptionLifecycle(t,
		func(flow string) error { return mgr.Subscribe(ctx, flow, "topic1", nil) },
		func(flow string) error { return mgr.Unsubscribe(ctx, flow, "topic1", nil) },
		adapter,
	)
}

func TestExchangeManagerAdapter_SubscribeTicker(t *testing.T) {
	t.Parallel()

	adapter := &fakeAdapter{}
	mgr := ws.NewExchangeManagerAdapter(adapter)
	ctx := context.Background()

	verifyDualSubscriptionLifecycle(t,
		func(flow string) error { return mgr.SubscribeTicker(ctx, flow, "BTCUSDT") },
		func(flow string) error { return mgr.UnsubscribeTicker(ctx, flow, "BTCUSDT") },
		adapter,
	)
}

func TestExchangeManagerAdapter_SubscribePersonal(t *testing.T) {
	t.Parallel()

	adapter := &fakeAdapter{}
	mgr := ws.NewExchangeManagerAdapter(adapter)
	ctx := context.Background()

	verifyDualSubscriptionLifecycle(t,
		func(flow string) error { return mgr.SubscribePersonal(ctx, flow) },
		func(flow string) error { return mgr.UnsubscribePersonal(ctx, flow) },
		adapter,
	)
}

func TestExchangeManagerAdapter_SubscribeDepth(t *testing.T) {
	t.Parallel()

	adapter := &fakeAdapter{}
	mgr := ws.NewExchangeManagerAdapter(adapter)
	ctx := context.Background()

	verifyDualSubscriptionLifecycle(t,
		func(flow string) error { return mgr.SubscribeDepth(ctx, flow, "BTCUSDT") },
		func(flow string) error { return mgr.UnsubscribeDepth(ctx, flow, "BTCUSDT") },
		adapter,
	)
}

func TestExchangeManagerAdapter_SubscribePublic(t *testing.T) {
	t.Parallel()

	adapter := &fakeAdapter{}
	mgr := ws.NewExchangeManagerAdapter(adapter)
	ctx := context.Background()

	require.NoError(t, mgr.SubscribePublic(ctx, "depth:BTCUSDT", nil))
	assert.Equal(t, 1, adapter.subCount)

	require.NoError(t, mgr.UnsubscribePublic(ctx, "depth:BTCUSDT", nil))
	assert.Equal(t, 1, adapter.unsubCount)
}

func TestExchangeManagerAdapter_SubscribeErrorRollback(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("network error")
	adapter := &fakeAdapter{failSubscribe: expectedErr}
	mgr := ws.NewExchangeManagerAdapter(adapter)
	ctx := context.Background()

	err := mgr.Subscribe(ctx, "flow1", "topic1", nil)
	require.ErrorIs(t, err, expectedErr)

	// Secondary subscribe after failed initial attempt should try physical subscribe again
	adapter.mu.Lock()
	adapter.failSubscribe = nil
	adapter.mu.Unlock()

	require.NoError(t, mgr.Subscribe(ctx, "flow2", "topic1", nil))
	assert.Equal(t, 1, adapter.subCount)
}

func (f *fakeAdapter) SubscribeTrade(ctx context.Context, sym string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failSubscribe != nil {
		return f.failSubscribe
	}
	f.subCount++
	f.subTopics = append(f.subTopics, "trade:"+sym)
	return nil
}

func (f *fakeAdapter) UnsubscribeTrade(ctx context.Context, sym string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unsubCount++
	f.unsubTopics = append(f.unsubTopics, "trade:"+sym)
	return nil
}

func (f *fakeAdapter) ParseTrade([]byte) (string, []domain.PublicTrade, error) {
	return "BTCUSDT", []domain.PublicTrade{{Symbol: "BTCUSDT", Price: 60000.0, Volume: 1.5}}, nil
}

func TestExchangeManagerAdapter_ParseDepth(t *testing.T) {
	t.Parallel()

	adapter := &fakeAdapter{}
	mgr := ws.NewExchangeManagerAdapter(adapter)

	sym, ob, err := mgr.ParseDepth([]byte(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", sym)
	require.NotNil(t, ob)
	assert.Equal(t, "BTCUSDT", ob.Symbol)
}

func TestExchangeManagerAdapter_ParseTrade(t *testing.T) {
	t.Parallel()

	adapter := &fakeAdapter{}
	mgr := ws.NewExchangeManagerAdapter(adapter)

	sym, trades, err := mgr.ParseTrade([]byte(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", sym)
	require.Len(t, trades, 1)
	assert.Equal(t, 60000.0, trades[0].Price)
}

func TestExchangeManagerAdapter_SubscribeTrade(t *testing.T) {
	t.Parallel()

	adapter := &fakeAdapter{}
	mgr := ws.NewExchangeManagerAdapter(adapter)

	ctx := context.Background()
	verifyDualSubscriptionLifecycle(
		t,
		func(flow string) error { return mgr.SubscribeTrade(ctx, flow, "BTCUSDT") },
		func(flow string) error { return mgr.UnsubscribeTrade(ctx, flow, "BTCUSDT") },
		adapter,
	)
}
