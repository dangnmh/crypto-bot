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
	prices      map[string]*PriceData
	subscribers map[string]map[chan *PriceData]struct{}
	mu          sync.RWMutex
	logger      *slog.Logger
}

// NewPriceStore creates a new PriceStore.
func NewPriceStore() *PriceStore {
	return &PriceStore{
		prices:      make(map[string]*PriceData),
		subscribers: make(map[string]map[chan *PriceData]struct{}),
		logger:      slog.Default().With("component", "price_store"),
	}
}

// UpdatePrice writes a price update for a symbol (called by WS client).
func (s *PriceStore) UpdatePrice(symbol string, data *PriceData) {
	if data == nil {
		return
	}
	if data.Symbol == "" {
		data.Symbol = symbol
	}
	if data.UpdatedAt.IsZero() {
		data.UpdatedAt = time.Now()
	}

	s.mu.Lock()
	snapshot := *data
	s.prices[symbol] = &snapshot
	subs := make([]chan *PriceData, 0, len(s.subscribers[symbol]))
	for ch := range s.subscribers[symbol] {
		subs = append(subs, ch)
	}
	s.mu.Unlock()

	for _, ch := range subs {
		update := snapshot
		select {
		case ch <- &update:
		default:
		}
	}

	s.logger.Debug("store.UpdatePrice",
		slog.String("symbol", symbol),
		slog.Float64("lastPrice", data.LastPrice),
		slog.Float64("bid", data.BestBid),
		slog.Float64("ask", data.BestAsk),
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
	snapshot := *pd
	if age > maxAge {
		return &snapshot, fmt.Errorf("price data stale for %s (age: %v)", symbol, age)
	}

	return &snapshot, nil
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

// SubscribePrice returns a non-blocking stream of future price updates for one symbol.
// The channel is closed when ctx is cancelled.
func (s *PriceStore) SubscribePrice(ctx context.Context, symbol string) <-chan *PriceData {
	ch := make(chan *PriceData, 1)

	s.mu.Lock()
	if s.subscribers[symbol] == nil {
		s.subscribers[symbol] = make(map[chan *PriceData]struct{})
	}
	s.subscribers[symbol][ch] = struct{}{}
	s.mu.Unlock()

	go func() {
		<-ctx.Done()
		s.mu.Lock()
		if subs := s.subscribers[symbol]; subs != nil {
			delete(subs, ch)
			if len(subs) == 0 {
				delete(s.subscribers, symbol)
			}
		}
		s.mu.Unlock()
		close(ch)
	}()

	return ch
}
