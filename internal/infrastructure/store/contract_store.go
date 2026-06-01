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

// ContractStore manages contract specification data via REST synchronization.
type ContractStore struct {
	contracts         map[string]*ContractData
	mu                sync.RWMutex
	logger            *slog.Logger
	contractReadyOnce sync.Once
	readyWG           *sync.WaitGroup
}

// NewContractStore creates a new ContractStore.
func NewContractStore(wg *sync.WaitGroup, log *slog.Logger) *ContractStore {
	if wg == nil {
		wg = &sync.WaitGroup{}
	}
	wg.Add(1)
	return &ContractStore{
		contracts: make(map[string]*ContractData),
		logger:    log.With("component", "contract_store"),
		readyWG:   wg,
	}
}

// StartContractSync periodically fetches contract details and updates the store.
func (s *ContractStore) StartContractSync(ctx context.Context, client exchange.Client, interval time.Duration) {
	s.logger.DebugContext(ctx, "🔄 Starting contract sync", slog.Duration("interval", interval))

	defer s.logger.DebugContext(ctx, "🔄 Contract sync stopped")
	ticker.RunImmediate(ctx, interval, func() bool {
		s.syncContracts(ctx, client)
		return true
	})
}

func (s *ContractStore) syncContracts(ctx context.Context, client exchange.Client) {
	details, err := client.GetContractDetails(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "🔴 Contract sync failed", slog.Any("error", err))
		return
	}

	s.mu.Lock()
	for i := range details {
		s.contracts[details[i].Symbol] = ContractDataFromExchange(&details[i])
	}
	s.mu.Unlock()

	s.logger.DebugContext(ctx, "store.SyncContracts", slog.Int("count", len(details)))
	s.contractReadyOnce.Do(func() { s.readyWG.Done() })
}

// GetContract returns the contract specification for a symbol.
func (s *ContractStore) GetContract(_ context.Context, symbol string) (*ContractData, error) {
	s.mu.RLock()
	cd, ok := s.contracts[symbol]
	s.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("no contract data for %s", symbol)
	}
	snapshot := *cd
	return &snapshot, nil
}
