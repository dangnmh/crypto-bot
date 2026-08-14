package ws

import (
	"context"
	"sync"
)

type ExchangeManagerAdapterSubscriber interface {
	Subscribe(ctx context.Context, flowID, topic string, subMsg any) error
	Unsubscribe(ctx context.Context, flowID, topic string, unsubMsg any) error

	SubscribeTicker(ctx context.Context, flowID, symbol string) error
	UnsubscribeTicker(ctx context.Context, flowID, symbol string) error

	SubscribePersonal(ctx context.Context, flowID string) error
	UnsubscribePersonal(ctx context.Context, flowID string) error
	SubscribePublic(ctx context.Context, topic string, subMsg any) error
	UnsubscribePublic(ctx context.Context, topic string, unsubMsg any) error
}

type ExchangeManagerAdapter interface {
	ExchangeManagerAdapterSubscriber
	ExchangeAdapterParser
}

type exchangeManagerAdapter struct {
	mu          sync.RWMutex
	subscribers map[string]map[string]bool // topic -> set of flowIDs
	ExchangeAdapter
}

func NewExchangeManagerAdapter(exchangeAdapter ExchangeAdapter) ExchangeManagerAdapter {
	return &exchangeManagerAdapter{
		ExchangeAdapter: exchangeAdapter,
		subscribers:     make(map[string]map[string]bool),
	}
}

// subscribeInternal handles reference counting for subscription (0 -> 1 triggers physical subFn).
func (m *exchangeManagerAdapter) subscribeInternal(ctx context.Context, flowID, topic string, subFn func(context.Context) error) error {
	if topic == "" || flowID == "" {
		return nil
	}

	m.mu.Lock()
	flows, exists := m.subscribers[topic]
	if !exists {
		flows = make(map[string]bool)
		m.subscribers[topic] = flows
	}
	isFirstSubscriber := len(flows) == 0
	flows[flowID] = true
	m.mu.Unlock()

	if !isFirstSubscriber || m.ExchangeAdapter == nil {
		return nil
	}

	if err := subFn(ctx); err != nil {
		m.mu.Lock()
		delete(flows, flowID)
		if len(flows) == 0 {
			delete(m.subscribers, topic)
		}
		m.mu.Unlock()
		return err
	}
	return nil
}

// unsubscribeInternal handles reference counting for unsubscription (1 -> 0 triggers physical unsubFn).
func (m *exchangeManagerAdapter) unsubscribeInternal(ctx context.Context, flowID, topic string, unsubFn func(context.Context) error) error {
	if topic == "" || flowID == "" {
		return nil
	}

	m.mu.Lock()
	flows, exists := m.subscribers[topic]
	if !exists {
		m.mu.Unlock()
		return nil
	}

	delete(flows, flowID)
	isLastSubscriber := len(flows) == 0
	if isLastSubscriber {
		delete(m.subscribers, topic)
	}
	m.mu.Unlock()

	if !isLastSubscriber || m.ExchangeAdapter == nil {
		return nil
	}

	return unsubFn(ctx)
}

func (a *exchangeManagerAdapter) Subscribe(ctx context.Context, flowID, topic string, subMsg any) error {
	return a.subscribeInternal(ctx, flowID, topic, func(c context.Context) error {
		return a.ExchangeAdapter.SubscribePublic(c, topic, subMsg)
	})
}

func (a *exchangeManagerAdapter) Unsubscribe(ctx context.Context, flowID, topic string, unsubMsg any) error {
	return a.unsubscribeInternal(ctx, flowID, topic, func(c context.Context) error {
		return a.ExchangeAdapter.UnsubscribePublic(c, topic, unsubMsg)
	})
}

func (a *exchangeManagerAdapter) SubscribeTicker(ctx context.Context, flowID, symbol string) error {
	return a.subscribeInternal(ctx, flowID, symbol, func(c context.Context) error {
		return a.ExchangeAdapter.SubscribeTicker(c, symbol)
	})
}

func (a *exchangeManagerAdapter) UnsubscribeTicker(ctx context.Context, flowID, symbol string) error {
	return a.unsubscribeInternal(ctx, flowID, symbol, func(c context.Context) error {
		return a.ExchangeAdapter.UnsubscribeTicker(c, symbol)
	})
}

func (a *exchangeManagerAdapter) SubscribePersonal(ctx context.Context, flowID string) error {
	return a.subscribeInternal(ctx, flowID, "personal", func(c context.Context) error {
		return a.ExchangeAdapter.SubscribePersonal(c)
	})
}

func (a *exchangeManagerAdapter) UnsubscribePersonal(ctx context.Context, flowID string) error {
	return a.unsubscribeInternal(ctx, flowID, "personal", func(c context.Context) error {
		return a.ExchangeAdapter.UnsubscribePersonal(c)
	})
}

func (a *exchangeManagerAdapter) SubscribePublic(ctx context.Context, topic string, subMsg any) error {
	return a.subscribeInternal(ctx, "default", topic, func(c context.Context) error {
		return a.ExchangeAdapter.SubscribePublic(c, topic, subMsg)
	})
}

func (a *exchangeManagerAdapter) UnsubscribePublic(ctx context.Context, topic string, unsubMsg any) error {
	return a.unsubscribeInternal(ctx, "default", topic, func(c context.Context) error {
		return a.ExchangeAdapter.UnsubscribePublic(c, topic, unsubMsg)
	})
}
