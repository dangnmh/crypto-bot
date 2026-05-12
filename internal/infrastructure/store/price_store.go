package store

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// PriceStore manages real-time price updates from WebSocket streams.
type PriceStore struct {
	prices map[string]*PriceData
	mu     sync.RWMutex
	logger *slog.Logger
}

// NewPriceStore creates a new PriceStore.
func NewPriceStore() *PriceStore {
	return &PriceStore{
		prices: make(map[string]*PriceData),
		logger: slog.Default().With("component", "price_store"),
	}
}

// UpdatePrice writes a price update for a symbol (called by WS client).
func (s *PriceStore) UpdatePrice(symbol string, data *PriceData) {
	s.mu.Lock()
	s.prices[symbol] = data
	s.mu.Unlock()

	s.logger.Debug("store.UpdatePrice",
		"symbol", symbol,
		"lastPrice", data.LastPrice,
		"bid", data.BestBid,
		"ask", data.BestAsk,
	)
}

// GetPrice returns the latest price for a symbol.
// Returns error if data is stale (older than maxAge) or not found.
func (s *PriceStore) GetPrice(_ context.Context, symbol string, maxAge time.Duration) (*PriceData, error) {
	s.mu.RLock()
	pd, ok := s.prices[symbol]
	s.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("no price data for %s", symbol)
	}

	age := time.Since(pd.UpdatedAt)
	if age > maxAge {
		return pd, fmt.Errorf("price data stale for %s (age: %v)", symbol, age)
	}

	return pd, nil
}

// GetBestBidAsk returns the best bid and ask for a symbol.
func (s *PriceStore) GetBestBidAsk(_ context.Context, symbol string) (bid, ask float64, err error) {
	s.mu.RLock()
	pd, ok := s.prices[symbol]
	s.mu.RUnlock()

	if !ok {
		return 0, 0, fmt.Errorf("no price data for %s", symbol)
	}
	return pd.BestBid, pd.BestAsk, nil
}

// PriceAge returns the duration since the last price update.
func (s *PriceStore) PriceAge(symbol string) time.Duration {
	s.mu.RLock()
	pd, ok := s.prices[symbol]
	s.mu.RUnlock()

	if !ok {
		return time.Hour * 24 * 365
	}
	return time.Since(pd.UpdatedAt)
}
