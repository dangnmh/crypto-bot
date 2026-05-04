package app

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"crypto-bot/internal/infrastructure/store"

	"crypto-bot/pkg/types"
)

// ──────────────────────────────────────────────────────────────────────
// StoreRegistry — composable store container for bots
// ──────────────────────────────────────────────────────────────────────.

// StoreRegistry groups all injectable stores that a bot may need.
// Only non-nil stores participate in the lifecycle.
type StoreRegistry struct {
	Ticker   *store.TickerStore
	Contract *store.ContractStore
	Price    *store.PriceStore
	Depth    *store.DepthStore
	// Optional — only for bots that need these
	Funding *store.FundingStore
	Kline   *store.KlineStore

	readyWG *sync.WaitGroup
}

// NewStoreRegistry creates a StoreRegistry with the required base stores
// (Ticker, Contract, Price) pre-initialized. Optional stores (Funding, Kline, Depth)
// can be set after construction.
func NewStoreRegistry() *StoreRegistry {
	wg := &sync.WaitGroup{}
	return &StoreRegistry{
		Ticker:   store.NewTickerStore(wg),
		Contract: store.NewContractStore(wg),
		Price:    store.NewPriceStore(),
		Depth:    store.NewDepthStore(),
		readyWG:  wg,
	}
}

// WithFunding adds a FundingStore to the registry.
func (r *StoreRegistry) WithFunding() *StoreRegistry {
	r.Funding = store.NewFundingStore(r.readyWG)
	return r
}

// WithKline adds a KlineStore to the registry.
func (r *StoreRegistry) WithKline() *StoreRegistry {
	r.Kline = store.NewKlineStore()
	return r
}

// SyncConfig holds the intervals needed by StartStores.
type StoreSyncConfig struct {
	TickerInterval   types.Duration
	ContractInterval types.Duration
	// Optional — only used if FundingStore is present
	FundingInterval types.Duration
	FundingSymbols  []string
}

// StartStores launches background sync goroutines for all non-nil stores.
func (r *StoreRegistry) StartStores(ctx context.Context, engine *Engine, cfg StoreSyncConfig) {
	go r.Ticker.StartTickerSync(ctx, engine.Client, time.Duration(cfg.TickerInterval))
	go r.Contract.StartContractSync(ctx, engine.Client, time.Duration(cfg.ContractInterval))

	if r.Funding != nil && len(cfg.FundingSymbols) > 0 {
		go r.Funding.StartFundingSync(ctx, engine.Client, cfg.FundingSymbols, time.Duration(cfg.FundingInterval))
	}
}

// WaitReady blocks until all REST-synced stores complete their initial fetch.
func (r *StoreRegistry) WaitReady(ctx context.Context) error {
	readyChan := make(chan struct{})
	go func() {
		r.readyWG.Wait()
		close(readyChan)
	}()
	select {
	case <-readyChan:
		slog.Info("🟢 Store data fully synchronized")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

