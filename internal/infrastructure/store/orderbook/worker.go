package orderbook

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync/atomic"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
)

type workerMsgType int

const (
	msgUpdate workerMsgType = iota
	msgSnapshotLoaded
	msgCommitsLoaded
	msgInit
	msgFlush
)

type workerTask struct {
	msgType  workerMsgType
	ob       *domain.OrderBook
	snapshot *domain.OrderBook
	commits  []exchange.DepthCommit
	err      error
	doneCh   chan struct{}
	errCh    chan error
}

type symbolWorker struct {
	symbol          string
	cfg             SynchronizerConfig
	snapProvider    exchange.DepthProvider
	commitsProvider exchange.DepthCommitsProvider
	logger          *slog.Logger

	book           *LocalOrderBook
	state          atomic.Value // holds SyncState
	inbox          chan workerTask
	stagingBuffer  []*domain.OrderBook
	initWaiters    []chan error
	stopCh         chan struct{}
	done           chan struct{}
	lastSyncAt     time.Time
	lastSnapFailAt time.Time
}

func newSymbolWorker(
	symbol string,
	cfg SynchronizerConfig,
	snapProvider exchange.DepthProvider,
	commitsProvider exchange.DepthCommitsProvider,
	logger *slog.Logger,
) *symbolWorker {
	w := &symbolWorker{
		symbol:          symbol,
		cfg:             cfg,
		snapProvider:    snapProvider,
		commitsProvider: commitsProvider,
		logger:          logger.With("symbol", symbol),
		book:            NewLocalOrderBook(symbol),
		inbox:           make(chan workerTask, cfg.MaxBufferCapacity),
		stopCh:          make(chan struct{}),
		done:            make(chan struct{}),
	}
	w.setState(SyncStateUninitialized)
	return w
}

func (w *symbolWorker) getState() SyncState {
	if val := w.state.Load(); val != nil {
		if st, ok := val.(SyncState); ok {
			return st
		}
	}
	return SyncStateUninitialized
}

func (w *symbolWorker) setState(st SyncState) {
	w.state.Store(st)
}

func (w *symbolWorker) stop() {
	close(w.stopCh)
	<-w.done
	w.book.Clear()
}

func (w *symbolWorker) run(ctx context.Context) {
	defer close(w.done)

	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()

	for {
		select {
		case <-w.stopCh:
			return
		case <-runCtx.Done():
			return
		case task, ok := <-w.inbox:
			if !ok {
				return
			}
			w.handleTask(runCtx, task)
		}
	}
}

func (w *symbolWorker) handleTask(ctx context.Context, task workerTask) {
	switch task.msgType {
	case msgUpdate:
		if task.ob != nil {
			w.handleUpdate(ctx, task.ob)
		}
	case msgSnapshotLoaded:
		w.handleSnapshotLoaded(ctx, task.snapshot, task.err)
	case msgCommitsLoaded:
		w.handleCommitsLoaded(ctx, task.commits, task.err)
	case msgInit:
		w.handleInit(ctx, task.errCh)
	case msgFlush:
		if w.getState() == SyncStateSyncing || w.getState() == SyncStateRecovering {
			go func(t workerTask) {
				time.Sleep(2 * time.Millisecond)
				select {
				case w.inbox <- t:
				case <-w.stopCh:
					if t.doneCh != nil {
						close(t.doneCh)
					}
				}
			}(task)
		} else if task.doneCh != nil {
			close(task.doneCh)
		}
	}
}

func (w *symbolWorker) handleInit(ctx context.Context, errCh chan error) {
	if w.getState() == SyncStateReady {
		if errCh != nil {
			errCh <- nil
		}
		return
	}
	if errCh != nil {
		w.initWaiters = append(w.initWaiters, errCh)
	}
	if w.getState() == SyncStateUninitialized {
		w.triggerAsyncSnapshot(ctx)
	}
}

func (w *symbolWorker) handleUpdate(ctx context.Context, ob *domain.OrderBook) {
	state := w.getState()

	// 1. If uninitialized, stage event and trigger async snapshot
	if state == SyncStateUninitialized {
		w.stagingBuffer = append(w.stagingBuffer, ob)
		if !w.lastSnapFailAt.IsZero() && time.Since(w.lastSnapFailAt) < 2*time.Second {
			return
		}
		w.logger.InfoContext(ctx, "Initializing orderbook worker via async REST snapshot",
			slog.String("symbol", w.symbol),
			slog.Int64("trigger_version", ob.Version),
		)
		w.triggerAsyncSnapshot(ctx)
		return
	}

	// 2. If currently syncing or recovering, stage incoming events in-memory (5ns)
	if state == SyncStateSyncing || state == SyncStateRecovering {
		w.stagingBuffer = append(w.stagingBuffer, ob)
		if len(w.stagingBuffer) > 5000 {
			w.stagingBuffer = w.stagingBuffer[len(w.stagingBuffer)-2500:]
		}
		return
	}

	// 3. Steady state (SyncStateReady): apply delta directly
	applied, err := w.applyDelta(ob.Bids, ob.Asks, ob.FirstVersion, ob.Version)
	if err == nil {
		if applied {
			w.lastSyncAt = time.Now()
		}
		return
	}

	// 4. Sequence gap detected under StrictSequence -> Trigger non-blocking async recovery
	w.logger.WarnContext(ctx, "Sequence gap detected in orderbook delta stream",
		slog.String("symbol", w.symbol),
		slog.Int64("book_version", w.book.Version()),
		slog.Int64("expected_version", w.book.Version()+1),
		slog.Int64("received_version", ob.Version),
		slog.Int64("first_version", ob.FirstVersion),
		slog.Bool("strict_sequence", w.cfg.StrictSequence),
		slog.String("error", err.Error()),
	)
	w.stagingBuffer = append(w.stagingBuffer, ob)
	w.triggerAsyncRecovery(ctx)
}

func (w *symbolWorker) triggerAsyncSnapshot(ctx context.Context) {
	w.setState(SyncStateSyncing)
	timeout := w.cfg.SnapshotTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	symbol := w.symbol
	snapProvider := w.snapProvider

	go func() {
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
		defer cancel()

		snap, err := snapProvider.GetDepth(fetchCtx, symbol)
		select {
		case w.inbox <- workerTask{
			msgType:  msgSnapshotLoaded,
			snapshot: snap,
			err:      err,
		}:
		case <-w.stopCh:
		}
	}()
}

func (w *symbolWorker) handleSnapshotLoaded(ctx context.Context, snap *domain.OrderBook, err error) {
	if err != nil {
		w.lastSnapFailAt = time.Now()
		w.logger.ErrorContext(ctx, "Failed to fetch async depth snapshot",
			slog.String("symbol", w.symbol),
			slog.String("error", err.Error()),
		)
		w.setState(SyncStateUninitialized)
		w.stagingBuffer = nil
		w.notifyInitWaiters(fmt.Errorf("%w: %s (%w)", ErrSnapshotFetchFailed, w.symbol, err))
		return
	}

	w.lastSnapFailAt = time.Time{}

	// 1. Establish baseline snapshot
	w.book.LoadSnapshot(snap)
	w.lastSyncAt = time.Now()

	// 2. Replay staged events
	if w.cfg.Mode != SyncModeSnapshot {
		w.replayStagedBuffer()
	} else {
		w.stagingBuffer = nil
	}

	w.setState(SyncStateReady)
	w.logger.InfoContext(ctx, "Orderbook depth synchronized successfully via actor pipeline",
		slog.String("symbol", w.symbol),
		slog.Int64("version", w.book.Version()),
		slog.String("mode", string(w.cfg.Mode)),
	)
	w.notifyInitWaiters(nil)
}

func (w *symbolWorker) notifyInitWaiters(err error) {
	for _, ch := range w.initWaiters {
		if ch != nil {
			ch <- err
		}
	}
	w.initWaiters = nil
}

func (w *symbolWorker) triggerAsyncRecovery(ctx context.Context) {
	w.setState(SyncStateRecovering)

	if w.commitsProvider != nil {
		timeout := 5 * time.Second
		symbol := w.symbol
		provider := w.commitsProvider
		limit := w.cfg.CommitRecoverySize
		if limit <= 0 {
			limit = 1000
		}

		go func() {
			fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
			defer cancel()

			commits, err := provider.GetDepthCommits(fetchCtx, symbol, limit)
			select {
			case w.inbox <- workerTask{
				msgType: msgCommitsLoaded,
				commits: commits,
				err:     err,
			}:
			case <-w.stopCh:
			}
		}()
		return
	}

	// Fallback to async snapshot directly
	w.triggerAsyncSnapshot(ctx)
}

func (w *symbolWorker) handleCommitsLoaded(ctx context.Context, commits []exchange.DepthCommit, err error) {
	if err == nil && len(commits) > 0 {
		if err := w.applyCommits(commits); err == nil {
			w.replayStagedBuffer()
			w.setState(SyncStateReady)
			w.lastSyncAt = time.Now()
			w.logger.InfoContext(ctx, "Successfully bridged orderbook gap with async depth commits",
				slog.String("symbol", w.symbol),
				slog.Int64("new_book_version", w.book.Version()),
			)
			return
		}
	}

	// Commits failed to bridge gap -> Fallback to async full snapshot
	w.logger.InfoContext(ctx, "Falling back to async full snapshot re-sync",
		slog.String("symbol", w.symbol),
		slog.Int64("curr_book_version", w.book.Version()),
	)
	w.triggerAsyncSnapshot(ctx)
}

func (w *symbolWorker) replayStagedBuffer() {
	if len(w.stagingBuffer) == 0 {
		return
	}

	snapVer := w.book.Version()
	slices.SortFunc(w.stagingBuffer, func(a, b *domain.OrderBook) int {
		return cmp.Compare(a.Version, b.Version)
	})

	replayedCount := 0
	droppedCount := 0

	for _, delta := range w.stagingBuffer {
		if delta.Version <= snapVer {
			droppedCount++
			continue
		}

		w.book.ApplyDelta(delta.Bids, delta.Asks, delta.Version, time.Now())
		replayedCount++
	}

	w.logger.Debug("Replayed staged orderbook buffer after snapshot",
		slog.String("symbol", w.symbol),
		slog.Int64("snap_version", snapVer),
		slog.Int64("new_version", w.book.Version()),
		slog.Int("dropped_stale", droppedCount),
		slog.Int("replayed_newer", replayedCount),
	)

	w.stagingBuffer = nil
}

func (w *symbolWorker) applyDelta(bids, asks []domain.OrderBookEntry, firstVersion, version int64) (bool, error) {
	currVersion := w.book.Version()
	if version > 0 && currVersion > 0 && version <= currVersion {
		return false, nil
	}
	if w.cfg.StrictSequence && currVersion > 0 && version > 0 {
		expectedStart := currVersion + 1
		actualStart := firstVersion
		if actualStart == 0 {
			actualStart = version
		}
		if actualStart != expectedStart {
			return false, ErrSequenceDiscontinuous
		}
	}
	w.book.ApplyDelta(bids, asks, version, time.Now())
	return true, nil
}

func (w *symbolWorker) applyCommits(commits []exchange.DepthCommit) error {
	slices.SortFunc(commits, func(a, b exchange.DepthCommit) int {
		return cmp.Compare(a.Version, b.Version)
	})
	for _, commit := range commits {
		if _, err := w.applyDelta(commit.Bids, commit.Asks, commit.Version, commit.Version); err != nil {
			return err
		}
	}
	return nil
}
