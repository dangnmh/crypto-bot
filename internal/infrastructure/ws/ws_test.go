package ws_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/internal/infrastructure/ws"
	pkgws "crypto-bot/pkg/ws"
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

func TestExchangeManagerAdapter_ReferenceCounting(t *testing.T) {
	t.Parallel()

	adapter := &fakeAdapter{}
	mgr := ws.NewExchangeManagerAdapter(adapter)
	ctx := context.Background()

	// Empty flowID or topic is a no-op
	if err := mgr.Subscribe(ctx, "", "topic1", nil); err != nil {
		t.Fatalf("expected nil error for empty flowID, got %v", err)
	}
	if err := mgr.Subscribe(ctx, "flow1", "", nil); err != nil {
		t.Fatalf("expected nil error for empty topic, got %v", err)
	}

	// flow1 subscribes -> 0 -> 1 subscriber, adapter called
	if err := mgr.Subscribe(ctx, "flow1", "topic1", nil); err != nil {
		t.Fatalf("Subscribe flow1 failed: %v", err)
	}
	if adapter.subCount != 1 {
		t.Errorf("expected adapter.subCount 1, got %d", adapter.subCount)
	}

	// flow2 subscribes -> 1 -> 2 subscribers, adapter NOT called again
	if err := mgr.Subscribe(ctx, "flow2", "topic1", nil); err != nil {
		t.Fatalf("Subscribe flow2 failed: %v", err)
	}
	if adapter.subCount != 1 {
		t.Errorf("expected adapter.subCount 1, got %d", adapter.subCount)
	}

	// flow1 unsubscribes -> 2 -> 1 subscriber, adapter unsub NOT called yet
	if err := mgr.Unsubscribe(ctx, "flow1", "topic1", nil); err != nil {
		t.Fatalf("Unsubscribe flow1 failed: %v", err)
	}
	if adapter.unsubCount != 0 {
		t.Errorf("expected adapter.unsubCount 0, got %d", adapter.unsubCount)
	}

	// flow2 unsubscribes -> 1 -> 0 subscriber, adapter unsub called once
	if err := mgr.Unsubscribe(ctx, "flow2", "topic1", nil); err != nil {
		t.Fatalf("Unsubscribe flow2 failed: %v", err)
	}
	if adapter.unsubCount != 1 {
		t.Errorf("expected adapter.unsubCount 1, got %d", adapter.unsubCount)
	}
}

func TestExchangeManagerAdapter_SubscribeTicker(t *testing.T) {
	t.Parallel()

	adapter := &fakeAdapter{}
	mgr := ws.NewExchangeManagerAdapter(adapter)
	ctx := context.Background()

	if err := mgr.SubscribeTicker(ctx, "flowA", "BTCUSDT"); err != nil {
		t.Fatalf("SubscribeTicker flowA failed: %v", err)
	}
	if adapter.subCount != 1 {
		t.Errorf("expected subCount 1, got %d", adapter.subCount)
	}

	if err := mgr.SubscribeTicker(ctx, "flowB", "BTCUSDT"); err != nil {
		t.Fatalf("SubscribeTicker flowB failed: %v", err)
	}
	if adapter.subCount != 1 {
		t.Errorf("expected subCount remaining 1, got %d", adapter.subCount)
	}

	if err := mgr.UnsubscribeTicker(ctx, "flowA", "BTCUSDT"); err != nil {
		t.Fatalf("UnsubscribeTicker flowA failed: %v", err)
	}
	if adapter.unsubCount != 0 {
		t.Errorf("expected unsubCount 0, got %d", adapter.unsubCount)
	}

	if err := mgr.UnsubscribeTicker(ctx, "flowB", "BTCUSDT"); err != nil {
		t.Fatalf("UnsubscribeTicker flowB failed: %v", err)
	}
	if adapter.unsubCount != 1 {
		t.Errorf("expected unsubCount 1, got %d", adapter.unsubCount)
	}
}

func TestExchangeManagerAdapter_SubscribePersonal(t *testing.T) {
	t.Parallel()

	adapter := &fakeAdapter{}
	mgr := ws.NewExchangeManagerAdapter(adapter)
	ctx := context.Background()

	if err := mgr.SubscribePersonal(ctx, "flow1"); err != nil {
		t.Fatalf("SubscribePersonal flow1 failed: %v", err)
	}
	if adapter.subCount != 1 {
		t.Errorf("expected subCount 1, got %d", adapter.subCount)
	}

	if err := mgr.SubscribePersonal(ctx, "flow2"); err != nil {
		t.Fatalf("SubscribePersonal flow2 failed: %v", err)
	}
	if adapter.subCount != 1 {
		t.Errorf("expected subCount remaining 1, got %d", adapter.subCount)
	}

	if err := mgr.UnsubscribePersonal(ctx, "flow1"); err != nil {
		t.Fatalf("UnsubscribePersonal flow1 failed: %v", err)
	}
	if adapter.unsubCount != 0 {
		t.Errorf("expected unsubCount 0, got %d", adapter.unsubCount)
	}

	if err := mgr.UnsubscribePersonal(ctx, "flow2"); err != nil {
		t.Fatalf("UnsubscribePersonal flow2 failed: %v", err)
	}
	if adapter.unsubCount != 1 {
		t.Errorf("expected unsubCount 1, got %d", adapter.unsubCount)
	}
}

func TestExchangeManagerAdapter_SubscribePublic(t *testing.T) {
	t.Parallel()

	adapter := &fakeAdapter{}
	mgr := ws.NewExchangeManagerAdapter(adapter)
	ctx := context.Background()

	if err := mgr.SubscribePublic(ctx, "depth:BTCUSDT", nil); err != nil {
		t.Fatalf("SubscribePublic failed: %v", err)
	}
	if adapter.subCount != 1 {
		t.Errorf("expected subCount 1, got %d", adapter.subCount)
	}

	if err := mgr.UnsubscribePublic(ctx, "depth:BTCUSDT", nil); err != nil {
		t.Fatalf("UnsubscribePublic failed: %v", err)
	}
	if adapter.unsubCount != 1 {
		t.Errorf("expected unsubCount 1, got %d", adapter.unsubCount)
	}
}

func TestExchangeManagerAdapter_SubscribeErrorRollback(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("network error")
	adapter := &fakeAdapter{failSubscribe: expectedErr}
	mgr := ws.NewExchangeManagerAdapter(adapter)
	ctx := context.Background()

	err := mgr.Subscribe(ctx, "flow1", "topic1", nil)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected error %v, got %v", expectedErr, err)
	}

	// Secondary subscribe after failed initial attempt should try physical subscribe again
	adapter.mu.Lock()
	adapter.failSubscribe = nil
	adapter.mu.Unlock()

	if err := mgr.Subscribe(ctx, "flow2", "topic1", nil); err != nil {
		t.Fatalf("Subscribe after rollback failed: %v", err)
	}
	if adapter.subCount != 1 {
		t.Errorf("expected subCount 1 after retry, got %d", adapter.subCount)
	}
}
