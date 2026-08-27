package application_test

import (
	"context"
	"sync"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
)

type mockTopGainerFetcher struct {
	gainers []exchange.TopGainerResult
}

func (m *mockTopGainerFetcher) GetTopGainer(_ context.Context, _ exchange.TopGainerRequest) ([]exchange.TopGainerResult, error) {
	return m.gainers, nil
}

type mockDepthProvider struct{}

func (m *mockDepthProvider) GetDepth(_ context.Context, symbol string) (*domain.OrderBook, error) {
	return &domain.OrderBook{Symbol: symbol}, nil
}

type mockDepthSubscriber struct {
	mu                 sync.Mutex
	subscribed         map[string]bool
	unsubscribed       map[string]bool
	tradesSubscribed   map[string]bool
	tradesUnsubscribed map[string]bool
}

func newMockDepthSubscriber() *mockDepthSubscriber {
	return &mockDepthSubscriber{
		subscribed:         make(map[string]bool),
		unsubscribed:       make(map[string]bool),
		tradesSubscribed:   make(map[string]bool),
		tradesUnsubscribed: make(map[string]bool),
	}
}

func (m *mockDepthSubscriber) SubscribeDepth(_ context.Context, _, symbol string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subscribed[symbol] = true
	delete(m.unsubscribed, symbol)
	return nil
}

func (m *mockDepthSubscriber) UnsubscribeDepth(_ context.Context, _, symbol string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.unsubscribed[symbol] = true
	delete(m.subscribed, symbol)
	return nil
}

func (m *mockDepthSubscriber) SubscribeTrade(_ context.Context, _, symbol string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tradesSubscribed[symbol] = true
	delete(m.tradesUnsubscribed, symbol)
	return nil
}

func (m *mockDepthSubscriber) UnsubscribeTrade(_ context.Context, _, symbol string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tradesUnsubscribed[symbol] = true
	delete(m.tradesSubscribed, symbol)
	return nil
}

func (m *mockDepthSubscriber) IsSubscribed(symbol string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.subscribed[symbol]
}

func (m *mockDepthSubscriber) IsUnsubscribed(symbol string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.unsubscribed[symbol]
}

func (m *mockDepthSubscriber) IsTradeSubscribed(symbol string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tradesSubscribed[symbol]
}

func (m *mockDepthSubscriber) IsTradeUnsubscribed(symbol string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tradesUnsubscribed[symbol]
}
