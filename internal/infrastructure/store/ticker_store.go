package store

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/ticker"
)

// TickerStore manages ticker snapshot data via REST synchronization.
type TickerStore struct {
	tickers         map[string]*TickerData
	mu              sync.RWMutex
	logger          *slog.Logger
	tickerReadyOnce sync.Once
	readyWG         *sync.WaitGroup
}

// NewTickerStore creates a new TickerStore.
func NewTickerStore(wg *sync.WaitGroup) *TickerStore {
	if wg == nil {
		wg = &sync.WaitGroup{}
	}
	wg.Add(1)
	return &TickerStore{
		tickers: make(map[string]*TickerData),
		logger:  slog.Default().With("component", "ticker_store"),
		readyWG: wg,
	}
}

// StartTickerSync periodically fetches all tickers and updates the store.
func (s *TickerStore) StartTickerSync(ctx context.Context, client exchange.Client, interval time.Duration) {
	s.logger.Debug("🔄 Starting ticker sync", "interval", interval)

	defer s.logger.Debug("🔄 Ticker sync stopped")
	ticker.RunImmediate(ctx, interval, func() bool {
		s.syncTickers(ctx, client)
		return true
	})
}

func (s *TickerStore) syncTickers(ctx context.Context, client exchange.Client) {
	tickers, err := client.GetTickers(ctx, "")
	if err != nil {
		s.logger.Error("🔴 Ticker sync failed", "error", err)
		return
	}

	s.mu.Lock()
	for i := range tickers {
		s.tickers[tickers[i].Symbol] = TickerDataFromExchange(&tickers[i])
	}
	s.mu.Unlock()

	s.logger.Debug("store.SyncTickers", "count", len(tickers))
	s.tickerReadyOnce.Do(func() { s.readyWG.Done() })
}

// GetTicker returns the latest ticker data for a symbol.
func (s *TickerStore) GetTicker(_ context.Context, symbol string) (*TickerData, error) {
	s.mu.RLock()
	td, ok := s.tickers[symbol]
	s.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("no ticker data for %s", symbol)
	}
	snapshot := *td
	return &snapshot, nil
}

// GetAllTickers returns all ticker data as a slice.
func (s *TickerStore) GetAllTickers(_ context.Context) []*TickerData {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*TickerData, 0, len(s.tickers))
	for _, td := range s.tickers {
		snapshot := *td
		result = append(result, &snapshot)
	}
	return result
}
