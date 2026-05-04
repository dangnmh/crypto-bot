package store

import (
	"fmt"
	"log/slog"
	"sync"

	"crypto-bot/internal/infrastructure/exchange"
)

// DepthStore manages OrderBook depth data (Level 2/3).
type DepthStore struct {
	depth  map[string]*exchange.OrderBook
	mu     sync.RWMutex
	logger *slog.Logger
}

// NewDepthStore creates a new DepthStore.
func NewDepthStore() *DepthStore {
	return &DepthStore{
		depth:  make(map[string]*exchange.OrderBook),
		logger: slog.Default().With("component", "depth_store"),
	}
}

// UpdateDepth completely overwrites the OrderBook for a symbol.
func (s *DepthStore) UpdateDepth(symbol string, ob *exchange.OrderBook) {
	s.mu.Lock()
	s.depth[symbol] = ob
	s.mu.Unlock()
}

// GetDepth returns the latest OrderBook for a symbol.
func (s *DepthStore) GetDepth(symbol string) (*exchange.OrderBook, error) {
	s.mu.RLock()
	ob, ok := s.depth[symbol]
	s.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("no depth data for %s", symbol)
	}

	return ob, nil
}
