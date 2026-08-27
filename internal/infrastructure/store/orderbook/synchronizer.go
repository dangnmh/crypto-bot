package orderbook

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
)

var (
	ErrSymbolNotFound        = errors.New("symbol not found in synchronizer")
	ErrSnapshotFetchFailed   = errors.New("snapshot fetch failed")
	ErrBufferOverflow        = errors.New("orderbook buffer overflow")
	ErrSequenceDiscontinuous = errors.New("sequence discontinuous")
)

// SynchronizerImpl implements the Synchronizer interface for multi-exchange orderbook depth management.
type SynchronizerImpl struct {
	cfg             SynchronizerConfig
	snapProvider    exchange.DepthProvider
	commitsProvider exchange.DepthCommitsProvider
	logger          *slog.Logger

	workers   map[string]*symbolWorker
	workersMu sync.RWMutex
}

// NewSynchronizer creates a new multi-exchange orderbook synchronizer.
// snapProvider is mandatory and must not be nil.
func NewSynchronizer(
	cfg SynchronizerConfig,
	snapProvider exchange.DepthProvider,
	commitsProvider exchange.DepthCommitsProvider,
	logger *slog.Logger,
) *SynchronizerImpl {
	if snapProvider == nil {
		panic("orderbook.NewSynchronizer: snapProvider must not be nil")
	}

	if err := cfg.Validate(); err != nil {
		panic(fmt.Sprintf("orderbook.NewSynchronizer: %v", err))
	}

	if logger == nil {
		logger = slog.Default()
	}

	return &SynchronizerImpl{
		cfg:             cfg,
		snapProvider:    snapProvider,
		commitsProvider: commitsProvider,
		logger:          logger.With("component", "OrderBookSynchronizer", "exchange", cfg.Exchange),
		workers:         make(map[string]*symbolWorker),
	}
}

func (s *SynchronizerImpl) getOrCreateWorker(ctx context.Context, symbol string) *symbolWorker {
	s.workersMu.Lock()
	defer s.workersMu.Unlock()

	worker, exists := s.workers[symbol]
	if !exists {
		worker = newSymbolWorker(symbol, s.cfg, s.snapProvider, s.commitsProvider, s.logger)
		s.workers[symbol] = worker
		go worker.run(ctx)
	}
	return worker
}

func (s *SynchronizerImpl) getWorker(symbol string) (*symbolWorker, bool) {
	s.workersMu.RLock()
	defer s.workersMu.RUnlock()

	worker, exists := s.workers[symbol]
	return worker, exists
}

// InitializeSymbol initializes or re-syncs a symbol with the REST snapshot.
func (s *SynchronizerImpl) InitializeSymbol(ctx context.Context, symbol string) error {
	worker := s.getOrCreateWorker(ctx, symbol)
	errCh := make(chan error, 1)
	select {
	case worker.inbox <- workerTask{msgType: msgInit, errCh: errCh}:
		select {
		case err := <-errCh:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ProcessUpdate ingests an incoming WebSocket depth update.
func (s *SynchronizerImpl) ProcessUpdate(ctx context.Context, ob *domain.OrderBook) error {
	if ob == nil || ob.Symbol == "" {
		return nil
	}

	worker := s.getOrCreateWorker(ctx, ob.Symbol)

	// Case 1: Pure Full Snapshot Stream (e.g. Toobit)
	if s.cfg.Mode == SyncModeSnapshot {
		worker.book.LoadSnapshot(ob)
		worker.setState(SyncStateReady)
		return nil
	}

	// Case 2: Incremental Diff Stream (e.g. MEXC, Binance, KuCoin)
	select {
	case worker.inbox <- workerTask{msgType: msgUpdate, ob: ob}:
		return nil
	default:
		s.logger.WarnContext(ctx, "Orderbook worker inbox full (buffer overflow)",
			slog.String("symbol", ob.Symbol),
			slog.Int64("event_version", ob.Version),
			slog.Int("inbox_len", len(worker.inbox)),
			slog.Int("inbox_cap", cap(worker.inbox)),
			slog.String("worker_state", string(worker.getState())),
			slog.Int64("curr_book_version", worker.book.Version()),
		)
		return ErrBufferOverflow
	}
}

// Flush blocks until all queued updates for the symbol are processed by its worker.
func (s *SynchronizerImpl) Flush(symbol string) {
	worker, ok := s.getWorker(symbol)
	if !ok {
		return
	}
	doneCh := make(chan struct{})
	select {
	case worker.inbox <- workerTask{msgType: msgFlush, doneCh: doneCh}:
		<-doneCh
	case <-worker.stopCh:
	}
}

// GetOrderBook returns the complete sorted snapshot for a symbol.
func (s *SynchronizerImpl) GetOrderBook(symbol string) (*domain.OrderBook, bool) {
	worker, ok := s.getWorker(symbol)
	if !ok || worker.getState() != SyncStateReady {
		return nil, false
	}
	return worker.book.GetSnapshot(0), true
}

// GetTopN returns the top N sorted depth levels for a symbol.
func (s *SynchronizerImpl) GetTopN(symbol string, limit int) (*domain.OrderBook, bool) {
	worker, ok := s.getWorker(symbol)
	if !ok || worker.getState() != SyncStateReady {
		return nil, false
	}
	return worker.book.GetSnapshot(limit), true
}

// GetBBO returns the Best Bid and Best Ask prices for a symbol in O(1) time.
func (s *SynchronizerImpl) GetBBO(symbol string) (bestBid, bestAsk float64, ok bool) {
	worker, ok := s.getWorker(symbol)
	if !ok || worker.getState() != SyncStateReady {
		return 0, 0, false
	}
	return worker.book.GetBBO()
}

// GetState returns the current synchronization state of a symbol.
func (s *SynchronizerImpl) GetState(symbol string) SyncState {
	worker, ok := s.getWorker(symbol)
	if !ok {
		return SyncStateUninitialized
	}
	return worker.getState()
}

// RemoveSymbol stops tracking and clears the orderbook for a symbol.
func (s *SynchronizerImpl) RemoveSymbol(symbol string) {
	s.workersMu.Lock()
	worker, exists := s.workers[symbol]
	if exists {
		delete(s.workers, symbol)
	}
	s.workersMu.Unlock()

	if exists {
		worker.stop()
	}
}

// Close gracefully shuts down the synchronizer and stops all workers.
func (s *SynchronizerImpl) Close() error {
	s.workersMu.Lock()
	workers := make([]*symbolWorker, 0, len(s.workers))
	for _, w := range s.workers {
		workers = append(workers, w)
	}
	clear(s.workers)
	s.workersMu.Unlock()

	for _, w := range workers {
		w.stop()
	}
	return nil
}
