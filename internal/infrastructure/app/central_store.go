package app

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/internal/infrastructure/ws"
	pkgws "crypto-bot/pkg/ws"
)

// ──────────────────────────────────────────────────────────────────────
// CentralStore — composable, option-configured store container
// ──────────────────────────────────────────────────────────────────────.

// StoreOption configures a CentralStore during construction.
// Each option enables a specific store and optionally registers
// a background sync task for REST-sourced data.
type StoreOption func(*CentralStore)

// syncTask describes a background sync goroutine to launch on Start().
type syncTask struct {
	name    string
	startFn func(ctx context.Context)
}

// CentralStore is an immutable, composable container for all market-data
// stores a bot may need. Construct via NewCentralStore with StoreOption
// functions. After construction, stores cannot be added or removed.
//
// Each bot owns its own isolated CentralStore instance.
type CentralStore struct {
	// Individual stores (nil if not enabled).
	ticker   *store.TickerStore
	contract *store.ContractStore
	price    *store.PriceStore
	depth    *store.DepthStore
	funding  *store.FundingStore
	kline    *store.KlineStore

	// Lifecycle.
	readyWG   *sync.WaitGroup
	syncTasks []syncTask
}

// NewCentralStore creates an immutable CentralStore configured by the
// provided options. Only stores explicitly enabled via options will be
// non-nil. After construction, call Start() → WireWS() → WaitReady().
func NewCentralStore(opts ...StoreOption) *CentralStore {
	cs := &CentralStore{
		readyWG: &sync.WaitGroup{},
	}
	for _, opt := range opts {
		opt(cs)
	}
	return cs
}

// ──────────────────────────────────────────────────────────────────────
// Option functions
// ──────────────────────────────────────────────────────────────────────.

// WithTicker enables the TickerStore and registers a periodic REST sync.
func WithTicker(client exchange.Client, interval time.Duration) StoreOption {
	return func(cs *CentralStore) {
		cs.ticker = store.NewTickerStore(cs.readyWG)
		cs.syncTasks = append(cs.syncTasks, syncTask{
			name: "ticker",
			startFn: func(ctx context.Context) {
				cs.ticker.StartTickerSync(ctx, client, interval)
			},
		})
	}
}

// WithContract enables the ContractStore and registers a periodic REST sync.
func WithContract(client exchange.Client, interval time.Duration) StoreOption {
	return func(cs *CentralStore) {
		cs.contract = store.NewContractStore(cs.readyWG)
		cs.syncTasks = append(cs.syncTasks, syncTask{
			name: "contract",
			startFn: func(ctx context.Context) {
				cs.contract.StartContractSync(ctx, client, interval)
			},
		})
	}
}

// WithFunding enables the FundingStore and registers a periodic REST sync
// for the specified symbols.
func WithFunding(client exchange.Client, interval time.Duration, symbols []string) StoreOption {
	return func(cs *CentralStore) {
		cs.funding = store.NewFundingStore(cs.readyWG)
		cs.syncTasks = append(cs.syncTasks, syncTask{
			name: "funding",
			startFn: func(ctx context.Context) {
				cs.funding.StartFundingSync(ctx, client, symbols, interval)
			},
		})
	}
}

// WithPrice enables the PriceStore (WS-fed, no REST sync).
func WithPrice() StoreOption {
	return func(cs *CentralStore) {
		cs.price = store.NewPriceStore()
	}
}

// WithDepth enables the DepthStore (WS-fed, no REST sync).
func WithDepth() StoreOption {
	return func(cs *CentralStore) {
		cs.depth = store.NewDepthStore()
	}
}

// WithKline enables the KlineStore (WS-fed, no REST sync).
func WithKline() StoreOption {
	return func(cs *CentralStore) {
		cs.kline = store.NewKlineStore()
	}
}

// ──────────────────────────────────────────────────────────────────────
// Reader accessors — return nil if the store was not enabled
// ──────────────────────────────────────────────────────────────────────.

// Ticker returns the TickerReader, or nil if not enabled.
func (cs *CentralStore) Ticker() store.TickerReader {
	if cs.ticker == nil {
		return nil
	}
	return cs.ticker
}

// Contract returns the ContractReader, or nil if not enabled.
func (cs *CentralStore) Contract() store.ContractReader {
	if cs.contract == nil {
		return nil
	}
	return cs.contract
}

// Price returns the PriceReader, or nil if not enabled.
func (cs *CentralStore) Price() store.PriceReader {
	if cs.price == nil {
		return nil
	}
	return cs.price
}

// Depth returns the DepthReader, or nil if not enabled.
func (cs *CentralStore) Depth() store.DepthReader {
	if cs.depth == nil {
		return nil
	}
	return cs.depth
}

// Funding returns the FundingReader, or nil if not enabled.
func (cs *CentralStore) Funding() store.FundingReader {
	if cs.funding == nil {
		return nil
	}
	return cs.funding
}

// Kline returns the KlineReadWriter, or nil if not enabled.
func (cs *CentralStore) Kline() store.KlineReadWriter {
	if cs.kline == nil {
		return nil
	}
	return cs.kline
}

// ──────────────────────────────────────────────────────────────────────
// Lifecycle
// ──────────────────────────────────────────────────────────────────────.

// Start launches background sync goroutines for all REST-sourced stores.
// Call this before WaitReady().
func (cs *CentralStore) Start(ctx context.Context) {
	for _, task := range cs.syncTasks {
		go task.startFn(ctx)
	}
}

// WaitReady blocks until all REST-synced stores complete their initial fetch,
// or the context is cancelled.
func (cs *CentralStore) WaitReady(ctx context.Context) error {
	readyChan := make(chan struct{})
	go func() {
		cs.readyWG.Wait()
		close(readyChan)
	}()
	select {
	case <-readyChan:
		slog.Default().InfoContext(ctx, "🟢 CentralStore fully synchronized")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// WireWS auto-connects WebSocket message handlers to the appropriate
// store writers. This replaces per-bot manual pool.On(...) boilerplate.
// Only stores that are enabled will have handlers registered.
func (cs *CentralStore) WireWS(pool *pkgws.Pool, adapter ws.ExchangeAdapter) {
	if pool == nil || adapter == nil {
		return
	}

	if cs.price != nil {
		pool.On("ticker", func(data []byte) {
			symbol, pd, err := adapter.ParseTicker(data)
			if err != nil {
				slog.Error("WireWS ParseTicker error", slog.String("data", string(data)), slog.Any("error", err))
				return
			}
			if pd != nil {
				cs.price.UpdatePrice(symbol, pd)
			}
		})
	}

	if cs.depth != nil {
		pool.On("depth", func(data []byte) {
			symbol, ob, err := adapter.ParseDepth(data)
			if err != nil {
				slog.Error("WireWS ParseDepth error", slog.String("data", string(data)), slog.Any("error", err))
				return
			}
			if ob != nil {
				cs.depth.UpdateDepth(symbol, ob)
			}
		})
	}

	if cs.kline != nil {
		pool.On("kline", func(data []byte) {
			symbol, kl, err := adapter.ParseKline(data)
			if err != nil {
				slog.Error("WireWS ParseKline error", slog.String("data", string(data)), slog.Any("error", err))
				return
			}
			if kl != nil {
				cs.kline.AddKline(symbol, *kl)
			}
		})
	}
}
