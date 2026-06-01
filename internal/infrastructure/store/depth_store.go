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
func NewDepthStore(log *slog.Logger) *DepthStore {
	return &DepthStore{
		depth:  make(map[string]*domain.OrderBook),
		logger: log.With("component", "depth_store"),
	}
}

// UpdateDepth completely overwrites the OrderBook for a symbol.
func (s *DepthStore) UpdateDepth(symbol string, ob *domain.OrderBook) {
	if ob == nil {
		return
	}
	snapshot := cloneOrderBook(ob)
	s.mu.Lock()
	s.depth[symbol] = snapshot
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

	return cloneOrderBook(ob), nil
}

func cloneOrderBook(ob *domain.OrderBook) *domain.OrderBook {
	if ob == nil {
		return nil
	}
	out := &domain.OrderBook{
		Symbol:  ob.Symbol,
		Version: ob.Version,
		Asks:    make([]domain.OrderBookEntry, len(ob.Asks)),
		Bids:    make([]domain.OrderBookEntry, len(ob.Bids)),
	}
	copy(out.Asks, ob.Asks)
	copy(out.Bids, ob.Bids)
	return out
}
