package store

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"crypto-bot/internal/domain"
)

// DepthStore manages OrderBook depth data (Level 2/3).
type DepthStore struct {
	depth  map[string]*domain.OrderBook
	mu     sync.RWMutex
	logger *slog.Logger
}

// NewDepthStore creates a new DepthStore.
func NewDepthStore() *DepthStore {
	return &DepthStore{
		depth:  make(map[string]*domain.OrderBook),
		logger: slog.Default().With("component", "depth_store"),
	}
}

// UpdateDepth completely overwrites the OrderBook for a symbol.
func (s *DepthStore) UpdateDepth(symbol string, ob *domain.OrderBook) {
	s.mu.Lock()
	s.depth[symbol] = ob
	s.mu.Unlock()
}

// DeleteDepth removes the OrderBook data for a symbol.
func (s *DepthStore) DeleteDepth(symbol string) {
	s.mu.Lock()
	delete(s.depth, symbol)
	s.mu.Unlock()
}

// GetDepth returns the latest OrderBook for a symbol.
func (s *DepthStore) GetDepth(_ context.Context, symbol string) (*domain.OrderBook, error) {
	s.mu.RLock()
	ob, ok := s.depth[symbol]
	s.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("no depth data for %s", symbol)
	}

	return ob, nil
}
