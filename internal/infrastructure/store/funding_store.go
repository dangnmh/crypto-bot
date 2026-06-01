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

// FundingStore manages funding rates and settle times via REST synchronization.
type FundingStore struct {
	funding          map[string]*FundingData
	mu               sync.RWMutex
	logger           *slog.Logger
	fundingReadyOnce sync.Once
	readyWG          *sync.WaitGroup
}

// NewFundingStore creates a new FundingStore.
func NewFundingStore(wg *sync.WaitGroup, log *slog.Logger) *FundingStore {
	if wg == nil {
		wg = &sync.WaitGroup{}
	}
	wg.Add(1) // Register requirement for initial funding sync
	return &FundingStore{
		funding: make(map[string]*FundingData),
		logger:  log.With("component", "funding_store"),
		readyWG: wg,
	}
}

// StartFundingSync periodically fetches per-symbol funding rates and updates the store.
func (s *FundingStore) StartFundingSync(ctx context.Context, client exchange.Client, symbols []string, interval time.Duration) {
	s.logger.DebugContext(ctx, "🔄 Starting funding sync", slog.Duration("interval", interval), slog.Int("symbols", len(symbols)))

	defer s.logger.DebugContext(ctx, "🔄 Funding sync stopped")
	ticker.RunImmediate(ctx, interval, func() bool {
		s.syncFunding(ctx, client, symbols)
		return true
	})
}

func (s *FundingStore) syncFunding(ctx context.Context, client exchange.Client, symbols []string) {
	results, err := client.GetFundingRates(ctx, symbols)
	if err != nil {
		s.logger.WarnContext(ctx, "🟡 Bulk funding sync failed", slog.Any("error", err))
		return
	}

	// Create symbol set for quick lookup
	symbolMap := make(map[string]bool)
	for _, sym := range symbols {
		symbolMap[sym] = true
	}

	s.mu.Lock()
	for _, res := range results {
		if symbolMap[res.Symbol] {
			s.funding[res.Symbol] = &FundingData{
				Symbol:         res.Symbol,
				FundingRate:    res.Rate,
				NextSettleTime: res.SettleTime,
				UpdatedAt:      time.Now(),
			}
		}
	}
	s.mu.Unlock()

	s.logger.DebugContext(ctx, "store.SyncFunding.done", slog.Int("count", len(symbols)))
	s.fundingReadyOnce.Do(func() { s.readyWG.Done() })
}

// GetFunding returns the funding rate details for a symbol.
func (s *FundingStore) GetFunding(_ context.Context, symbol string) (*FundingData, error) {
	s.mu.RLock()
	fd, ok := s.funding[symbol]
	s.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("no funding data for %s", symbol)
	}
	snapshot := *fd
	return &snapshot, nil
}

// GetSettleTime returns the next funding settlement time for a symbol.
func (s *FundingStore) GetSettleTime(_ context.Context, symbol string) (time.Time, error) {
	s.mu.RLock()
	fd, ok := s.funding[symbol]
	s.mu.RUnlock()

	if !ok {
		return time.Time{}, fmt.Errorf("no funding data for %s", symbol)
	}

	if fd.NextSettleTime <= 0 {
		return time.Time{}, fmt.Errorf("invalid settle time for %s: %d", symbol, fd.NextSettleTime)
	}

	return time.UnixMilli(fd.NextSettleTime), nil
}
