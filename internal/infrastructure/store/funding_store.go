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
func NewFundingStore(wg *sync.WaitGroup) *FundingStore {
	if wg == nil {
		wg = &sync.WaitGroup{}
	}
	wg.Add(1) // Register requirement for initial funding sync
	return &FundingStore{
		funding: make(map[string]*FundingData),
		logger:  slog.Default().With("component", "funding_store"),
		readyWG: wg,
	}
}

// StartFundingSync periodically fetches per-symbol funding rates and updates the store.
func (s *FundingStore) StartFundingSync(ctx context.Context, client exchange.Client, symbols []string, interval time.Duration) {
	s.logger.Debug("🔄 Starting funding sync", "interval", interval, "symbols", len(symbols))

	defer s.logger.Debug("🔄 Funding sync stopped")
	ticker.RunImmediate(ctx, interval, func() bool {
		s.syncFunding(ctx, client, symbols)
		return true
	})
}

func (s *FundingStore) syncFunding(ctx context.Context, client exchange.Client, symbols []string) {
	for _, sym := range symbols {
		detail, err := client.GetFundingRate(ctx, sym)
		if err != nil {
			s.logger.Warn("🟡 Funding sync failed for symbol", "error", err, "symbol", sym)
			continue
		}

		fd := FundingDataFromExchange(detail)
		s.mu.Lock()
		s.funding[sym] = fd
		s.mu.Unlock()
	}

	s.logger.Debug("store.SyncFunding.done", "count", len(symbols))
	s.fundingReadyOnce.Do(func() { s.readyWG.Done() })
}

// GetFunding returns the funding rate details for a symbol.
func (s *FundingStore) GetFunding(symbol string) (*FundingData, error) {
	s.mu.RLock()
	fd, ok := s.funding[symbol]
	s.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("no funding data for %s", symbol)
	}
	return fd, nil
}

// GetSettleTime returns the next funding settlement time for a symbol.
func (s *FundingStore) GetSettleTime(symbol string) (time.Time, error) {
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
