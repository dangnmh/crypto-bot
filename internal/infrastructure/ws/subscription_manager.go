package ws

import (
	"context"
	"errors"
	"sync"

	pkgws "crypto-bot/pkg/ws"
)

// SubscriptionManager provides reference-counted topic subscription management for a dedicated exchange WS pool.
type SubscriptionManager struct {
	mu          sync.RWMutex
	subscribers map[string]map[string]bool // topic -> set of flowIDs
	pool        *pkgws.Pool
}

// NewSubscriptionManager creates a new SubscriptionManager wrapping a dedicated exchange WS pool.
func NewSubscriptionManager(pool *pkgws.Pool) (*SubscriptionManager, error) {
	if pool == nil {
		return nil, errors.New("pool is required")
	}
	return &SubscriptionManager{
		subscribers: make(map[string]map[string]bool),
		pool:        pool,
	}, nil
}

// Subscribe tracks subscriber flowID for topic. Sends physical WS sub message to pool on first subscriber (0 -> 1).
func (m *SubscriptionManager) Subscribe(ctx context.Context, topic, flowID string, subMsg any) error {
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

	if !isFirstSubscriber {
		return nil
	}

	if err := m.pool.SubscribePublic(ctx, topic, subMsg); err != nil {
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

// Unsubscribe removes flowID from topic. Sends physical WS unsub message to pool when subscribers count reaches 0.
func (m *SubscriptionManager) Unsubscribe(ctx context.Context, topic, flowID string, unsubMsg any) error {
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

	if !isLastSubscriber {
		return nil
	}

	return m.pool.UnsubscribePublic(ctx, topic, unsubMsg)
}

// SubscriberCount returns the current number of active subscribers for a given topic.
func (m *SubscriptionManager) SubscriberCount(topic string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	flows, exists := m.subscribers[topic]
	if !exists {
		return 0
	}
	return len(flows)
}
