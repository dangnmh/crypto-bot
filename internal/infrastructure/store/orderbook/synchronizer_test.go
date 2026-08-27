package orderbook_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store/orderbook"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockSnapshotProvider struct {
	mu        sync.Mutex
	snapshots map[string]*domain.OrderBook
	err       error
}

func (m *mockSnapshotProvider) GetDepth(ctx context.Context, symbol string) (*domain.OrderBook, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	if snap, ok := m.snapshots[symbol]; ok {
		return snap, nil
	}
	return nil, errors.New("snapshot not found")
}

type mockCommitsProvider struct {
	mu      sync.Mutex
	commits map[string][]exchange.DepthCommit
}

func (m *mockCommitsProvider) GetDepthCommits(ctx context.Context, symbol string, limit int) ([]exchange.DepthCommit, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.commits[symbol]; ok {
		return c, nil
	}
	return nil, nil
}

func newTestSyncConfig(exch string, mode orderbook.SyncMode, strict bool) orderbook.SynchronizerConfig {
	return orderbook.SynchronizerConfig{
		Exchange:           exch,
		Mode:               mode,
		StrictSequence:     strict,
		MaxBufferCapacity:  500,
		SnapshotTimeout:    5 * time.Second,
		StaleTimeout:       10 * time.Second,
		CommitRecoverySize: 1000,
	}
}

func TestSynchronizerConfig_Validation(t *testing.T) {
	t.Parallel()

	valid := newTestSyncConfig("toobit", orderbook.SyncModeSnapshot, false)
	require.NoError(t, valid.Validate())

	missingExch := valid
	missingExch.Exchange = ""
	require.Error(t, missingExch.Validate())

	invalidMode := valid
	invalidMode.Mode = "UNKNOWN"
	require.Error(t, invalidMode.Validate())

	zeroBuffer := valid
	zeroBuffer.MaxBufferCapacity = 0
	require.Error(t, zeroBuffer.Validate())

	zeroTimeout := valid
	zeroTimeout.SnapshotTimeout = 0
	require.Error(t, zeroTimeout.Validate())

	zeroCommits := valid
	zeroCommits.CommitRecoverySize = 0
	require.Error(t, zeroCommits.Validate())
}

func TestSynchronizer_SnapshotMode_Toobit(t *testing.T) {
	t.Parallel()

	snapMock := &mockSnapshotProvider{
		snapshots: map[string]*domain.OrderBook{
			"BTCUSDT": {
				Symbol:  "BTCUSDT",
				Version: 10,
				Bids:    []domain.OrderBookEntry{{Price: 50000.0, Volume: 1.0}},
				Asks:    []domain.OrderBookEntry{{Price: 50100.0, Volume: 2.0}},
			},
		},
	}

	cfg := newTestSyncConfig("toobit", orderbook.SyncModeSnapshot, false)
	syncMgr := orderbook.NewSynchronizer(cfg, snapMock, nil, nil)
	defer func() { _ = syncMgr.Close() }()

	ctx := context.Background()

	// 0. InitializeSymbol works with REST snapshot
	err := syncMgr.InitializeSymbol(ctx, "BTCUSDT")
	require.NoError(t, err)
	assert.Equal(t, orderbook.SyncStateReady, syncMgr.GetState("BTCUSDT"))

	// 1. Process initial snapshot update
	update1 := &domain.OrderBook{
		Symbol:  "BTCUSDT",
		Version: 10,
		Bids: []domain.OrderBookEntry{
			{Price: 50000.0, Volume: 1.0},
		},
		Asks: []domain.OrderBookEntry{
			{Price: 50100.0, Volume: 2.0},
		},
	}
	err = syncMgr.ProcessUpdate(ctx, update1)
	require.NoError(t, err)

	assert.Equal(t, orderbook.SyncStateReady, syncMgr.GetState("BTCUSDT"))
	bboBid, bboAsk, ok := syncMgr.GetBBO("BTCUSDT")
	assert.True(t, ok)
	assert.Equal(t, 50000.0, bboBid)
	assert.Equal(t, 50100.0, bboAsk)

	// 2. Direct full snapshot update
	update2 := &domain.OrderBook{
		Symbol:  "BTCUSDT",
		Version: 11,
		Bids: []domain.OrderBookEntry{
			{Price: 50050.0, Volume: 3.0},
		},
		Asks: []domain.OrderBookEntry{
			{Price: 50080.0, Volume: 1.5},
		},
	}
	err = syncMgr.ProcessUpdate(ctx, update2)
	require.NoError(t, err)

	bboBid, bboAsk, ok = syncMgr.GetBBO("BTCUSDT")
	assert.True(t, ok)
	assert.Equal(t, 50050.0, bboBid)
	assert.Equal(t, 50080.0, bboAsk)
}

func TestSynchronizer_IncrementalMode_MEXC_WithRecovery(t *testing.T) {
	t.Parallel()

	snapMock := &mockSnapshotProvider{
		snapshots: map[string]*domain.OrderBook{
			"ETHUSDT": {
				Symbol:  "ETHUSDT",
				Version: 100,
				Bids: []domain.OrderBookEntry{
					{Price: 3000.0, Volume: 5.0},
					{Price: 2990.0, Volume: 10.0},
				},
				Asks: []domain.OrderBookEntry{
					{Price: 3010.0, Volume: 2.0},
					{Price: 3020.0, Volume: 8.0},
				},
			},
		},
	}

	commitsMock := &mockCommitsProvider{
		commits: map[string][]exchange.DepthCommit{
			"ETHUSDT": {
				{
					Version: 102,
					Bids:    []domain.OrderBookEntry{{Price: 3005.0, Volume: 2.0}},
					Asks:    []domain.OrderBookEntry{{Price: 3010.0, Volume: 0.0}},
				},
			},
		},
	}

	cfg := newTestSyncConfig("mexc", orderbook.SyncModeIncremental, true)
	syncMgr := orderbook.NewSynchronizer(cfg, snapMock, commitsMock, nil)
	defer func() { _ = syncMgr.Close() }()

	ctx := context.Background()

	// 1. Initialize Symbol via REST snapshot
	err := syncMgr.InitializeSymbol(ctx, "ETHUSDT")
	require.NoError(t, err)
	assert.Equal(t, orderbook.SyncStateReady, syncMgr.GetState("ETHUSDT"))

	// 2. Sequential update: version 101 (version == 100 + 1)
	err = syncMgr.ProcessUpdate(ctx, &domain.OrderBook{
		Symbol:  "ETHUSDT",
		Version: 101,
		Bids:    []domain.OrderBookEntry{{Price: 3001.0, Volume: 1.0}},
	})
	require.NoError(t, err)
	syncMgr.Flush("ETHUSDT")

	bestBid, _, ok := syncMgr.GetBBO("ETHUSDT")
	assert.True(t, ok)
	assert.Equal(t, 3001.0, bestBid)

	// 3. Gap update: version 103 arrives (current is 101, missing 102) -> Gap Recovery triggers via Commits!
	err = syncMgr.ProcessUpdate(ctx, &domain.OrderBook{
		Symbol:  "ETHUSDT",
		Version: 103,
		Bids:    []domain.OrderBookEntry{{Price: 3006.0, Volume: 4.0}},
	})
	require.NoError(t, err)
	syncMgr.Flush("ETHUSDT")

	assert.Equal(t, orderbook.SyncStateReady, syncMgr.GetState("ETHUSDT"))
	bestBid, bestAsk, ok := syncMgr.GetBBO("ETHUSDT")
	assert.True(t, ok)
	assert.Equal(t, 3006.0, bestBid)
	// Ask 3010 was deleted in commit 102, so next best ask is 3020
	assert.Equal(t, 3020.0, bestAsk)

	// 4. Stale update: version 100 (ignored)
	err = syncMgr.ProcessUpdate(ctx, &domain.OrderBook{
		Symbol:  "ETHUSDT",
		Version: 100,
		Bids:    []domain.OrderBookEntry{{Price: 9999.0, Volume: 99.0}},
	})
	require.NoError(t, err)
	syncMgr.Flush("ETHUSDT")
	bestBid, _, _ = syncMgr.GetBBO("ETHUSDT")
	assert.Equal(t, 3006.0, bestBid) // 9999 was not applied

	// 5. Remove Symbol
	syncMgr.RemoveSymbol("ETHUSDT")
	assert.Equal(t, orderbook.SyncStateUninitialized, syncMgr.GetState("ETHUSDT"))
	_, ok = syncMgr.GetOrderBook("ETHUSDT")
	assert.False(t, ok)
}

func TestSynchronizer_StrictSequence_KuCoin_GapRecovery(t *testing.T) {
	t.Parallel()

	snapMock := &mockSnapshotProvider{
		snapshots: map[string]*domain.OrderBook{
			"XBTUSDTM": {
				Symbol:  "XBTUSDTM",
				Version: 16,
				Bids: []domain.OrderBookEntry{
					{Price: 50000.0, Volume: 5.0},
				},
				Asks: []domain.OrderBookEntry{
					{Price: 50010.0, Volume: 2.0},
				},
			},
		},
	}

	cfg := newTestSyncConfig("kucoin", orderbook.SyncModeIncremental, true)
	syncMgr := orderbook.NewSynchronizer(cfg, snapMock, nil, nil)
	defer func() { _ = syncMgr.Close() }()

	ctx := context.Background()

	// 1. Initialize
	err := syncMgr.InitializeSymbol(ctx, "XBTUSDTM")
	require.NoError(t, err)
	assert.Equal(t, orderbook.SyncStateReady, syncMgr.GetState("XBTUSDTM"))

	// 2. Sequential update: 17 (16+1)
	err = syncMgr.ProcessUpdate(ctx, &domain.OrderBook{
		Symbol:  "XBTUSDTM",
		Version: 17,
		Bids:    []domain.OrderBookEntry{{Price: 50005.0, Volume: 1.0}},
	})
	require.NoError(t, err)
	syncMgr.Flush("XBTUSDTM")
	bestBid, _, _ := syncMgr.GetBBO("XBTUSDTM")
	assert.Equal(t, 50005.0, bestBid)

	// 3. Gap update on strict stream: version 20 (missing 18, 19) -> triggers snapshot recovery
	err = syncMgr.ProcessUpdate(ctx, &domain.OrderBook{
		Symbol:  "XBTUSDTM",
		Version: 20,
		Bids:    []domain.OrderBookEntry{{Price: 50008.0, Volume: 1.0}},
	})
	require.NoError(t, err)
	syncMgr.Flush("XBTUSDTM")
	assert.Equal(t, orderbook.SyncStateReady, syncMgr.GetState("XBTUSDTM"))
}

func TestSynchronizer_StrictSequence_GapFallbacks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		symbol   string
		snapVer  int64
		seqVer   int64
		commits  []exchange.DepthCommit
		gapEvent *domain.OrderBook
	}{
		{
			name:    "commit gap causes fallback to snapshot re-sync",
			symbol:  "SOLUSDT",
			snapVer: 100,
			seqVer:  101,
			commits: []exchange.DepthCommit{
				{Version: 104, Bids: []domain.OrderBookEntry{{Price: 150.5, Volume: 5.0}}},
			},
			gapEvent: &domain.OrderBook{
				Symbol:  "SOLUSDT",
				Version: 105,
				Bids:    []domain.OrderBookEntry{{Price: 150.8, Volume: 3.0}},
			},
		},
		{
			name:    "pending event gap after commits causes fallback to snapshot re-sync",
			symbol:  "DOGEUSDT",
			snapVer: 50,
			seqVer:  51,
			commits: []exchange.DepthCommit{
				{Version: 52, Bids: []domain.OrderBookEntry{{Price: 0.101, Volume: 50.0}}},
			},
			gapEvent: &domain.OrderBook{
				Symbol:  "DOGEUSDT",
				Version: 55,
				Bids:    []domain.OrderBookEntry{{Price: 0.102, Volume: 10.0}},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			snapMock := &mockSnapshotProvider{
				snapshots: map[string]*domain.OrderBook{
					tc.symbol: {
						Symbol:  tc.symbol,
						Version: tc.snapVer,
						Bids:    []domain.OrderBookEntry{{Price: 100.0, Volume: 10.0}},
						Asks:    []domain.OrderBookEntry{{Price: 101.0, Volume: 10.0}},
					},
				},
			}

			commitsMock := &mockCommitsProvider{
				commits: map[string][]exchange.DepthCommit{
					tc.symbol: tc.commits,
				},
			}

			cfg := newTestSyncConfig("mexc", orderbook.SyncModeIncremental, true)
			syncMgr := orderbook.NewSynchronizer(cfg, snapMock, commitsMock, nil)
			defer func() { _ = syncMgr.Close() }()

			ctx := context.Background()

			err := syncMgr.InitializeSymbol(ctx, tc.symbol)
			require.NoError(t, err)

			err = syncMgr.ProcessUpdate(ctx, &domain.OrderBook{
				Symbol:  tc.symbol,
				Version: tc.seqVer,
				Bids:    []domain.OrderBookEntry{{Price: 100.5, Volume: 1.0}},
			})
			require.NoError(t, err)
			syncMgr.Flush(tc.symbol)

			err = syncMgr.ProcessUpdate(ctx, tc.gapEvent)
			require.NoError(t, err)
			syncMgr.Flush(tc.symbol)
			assert.Equal(t, orderbook.SyncStateReady, syncMgr.GetState(tc.symbol))
		})
	}
}

func TestSynchronizer_ReaderMethodsAndErrors(t *testing.T) {
	t.Parallel()

	snapErr := errors.New("network timeout")
	snapMock := &mockSnapshotProvider{
		err: snapErr,
	}

	cfg := newTestSyncConfig("mexc", orderbook.SyncModeIncremental, true)
	cfg.MaxBufferCapacity = 2
	syncMgr := orderbook.NewSynchronizer(cfg, snapMock, nil, nil)
	defer func() { _ = syncMgr.Close() }()

	ctx := context.Background()

	// 1. Initialize fails due to snapshot error
	err := syncMgr.InitializeSymbol(ctx, "ADAUSDT")
	require.Error(t, err)
	assert.ErrorIs(t, err, orderbook.ErrSnapshotFetchFailed)

	// 2. NewSynchronizer panics if snapProvider is nil
	assert.Panics(t, func() {
		_ = orderbook.NewSynchronizer(cfg, nil, nil, nil)
	})

	// 3. Uninitialized symbol getters
	assert.Equal(t, orderbook.SyncStateUninitialized, syncMgr.GetState("UNKNOWN"))
	_, ok := syncMgr.GetOrderBook("UNKNOWN")
	assert.False(t, ok)
	_, ok = syncMgr.GetTopN("UNKNOWN", 5)
	assert.False(t, ok)
	_, _, ok = syncMgr.GetBBO("UNKNOWN")
	assert.False(t, ok)

	// 3. Nil update handling
	err = syncMgr.ProcessUpdate(ctx, nil)
	require.NoError(t, err)
	err = syncMgr.ProcessUpdate(ctx, &domain.OrderBook{Symbol: ""})
	require.NoError(t, err)

	// 4. Successful snapshot for XRP
	snapMock.mu.Lock()
	snapMock.err = nil
	snapMock.snapshots = map[string]*domain.OrderBook{
		"XRPUSDT": {
			Symbol:  "XRPUSDT",
			Version: 10,
			Bids: []domain.OrderBookEntry{
				{Price: 0.50, Volume: 100},
				{Price: 0.49, Volume: 200},
			},
			Asks: []domain.OrderBookEntry{
				{Price: 0.51, Volume: 150},
				{Price: 0.52, Volume: 250},
			},
		},
	}
	snapMock.mu.Unlock()

	err = syncMgr.InitializeSymbol(ctx, "XRPUSDT")
	require.NoError(t, err)

	ob, ok := syncMgr.GetOrderBook("XRPUSDT")
	assert.True(t, ok)
	assert.Len(t, ob.Bids, 2)
	assert.Len(t, ob.Asks, 2)

	topN, ok := syncMgr.GetTopN("XRPUSDT", 1)
	assert.True(t, ok)
	assert.Len(t, topN.Bids, 1)
	assert.Len(t, topN.Asks, 1)
}

func TestSynchronizer_BufferSnapshotReplay_Pattern(t *testing.T) {
	t.Parallel()

	snapMock := &mockSnapshotProvider{
		snapshots: map[string]*domain.OrderBook{
			"BTCUSDT": {
				Symbol:  "BTCUSDT",
				Version: 100,
				Bids: []domain.OrderBookEntry{
					{Price: 50000.0, Volume: 1.0},
				},
				Asks: []domain.OrderBookEntry{
					{Price: 50100.0, Volume: 1.0},
				},
			},
		},
	}

	cfg := newTestSyncConfig("binance", orderbook.SyncModeIncremental, true)
	syncMgr := orderbook.NewSynchronizer(cfg, snapMock, nil, nil)
	defer func() { _ = syncMgr.Close() }()

	ctx := context.Background()

	// 1. Send first delta when uninitialized -> triggers snapshot fetch and stages the delta
	// Staged delta version 99 (stale)
	err := syncMgr.ProcessUpdate(ctx, &domain.OrderBook{
		Symbol:  "BTCUSDT",
		Version: 99,
		Bids:    []domain.OrderBookEntry{{Price: 49990.0, Volume: 10.0}},
	})
	require.NoError(t, err)

	// Send delta version 102 (> snapshot version 100)
	err = syncMgr.ProcessUpdate(ctx, &domain.OrderBook{
		Symbol:  "BTCUSDT",
		Version: 102,
		Bids:    []domain.OrderBookEntry{{Price: 50010.0, Volume: 2.0}},
	})
	require.NoError(t, err)

	// Send delta version 103
	err = syncMgr.ProcessUpdate(ctx, &domain.OrderBook{
		Symbol:  "BTCUSDT",
		Version: 103,
		Bids:    []domain.OrderBookEntry{{Price: 50020.0, Volume: 3.0}},
	})
	require.NoError(t, err)

	syncMgr.Flush("BTCUSDT")

	// Orderbook should be ready and at version 103
	assert.Equal(t, orderbook.SyncStateReady, syncMgr.GetState("BTCUSDT"))
	bestBid, _, ok := syncMgr.GetBBO("BTCUSDT")
	assert.True(t, ok)
	assert.Equal(t, 50020.0, bestBid)

	// 2. Next live update version 104 is applied directly in live stream mode
	err = syncMgr.ProcessUpdate(ctx, &domain.OrderBook{
		Symbol:  "BTCUSDT",
		Version: 104,
		Bids:    []domain.OrderBookEntry{{Price: 50030.0, Volume: 4.0}},
	})
	require.NoError(t, err)

	syncMgr.Flush("BTCUSDT")
	bestBid, _, ok = syncMgr.GetBBO("BTCUSDT")
	assert.True(t, ok)
	assert.Equal(t, 50030.0, bestBid)
}

func TestSynchronizer_IncrementalMode_BatchedVersionInterval_MEXC(t *testing.T) {
	t.Parallel()

	snapMock := &mockSnapshotProvider{
		snapshots: map[string]*domain.OrderBook{
			"BTC_USDT": {
				Symbol:  "BTC_USDT",
				Version: 100,
				Bids:    []domain.OrderBookEntry{{Price: 50000.0, Volume: 1.0}},
				Asks:    []domain.OrderBookEntry{{Price: 50100.0, Volume: 2.0}},
			},
		},
	}

	cfg := newTestSyncConfig("mexc", orderbook.SyncModeIncremental, true)
	syncMgr := orderbook.NewSynchronizer(cfg, snapMock, nil, nil)
	defer func() { _ = syncMgr.Close() }()

	ctx := context.Background()

	// 1. Initialize symbol from snapshot v100
	err := syncMgr.InitializeSymbol(ctx, "BTC_USDT")
	require.NoError(t, err)
	assert.Equal(t, orderbook.SyncStateReady, syncMgr.GetState("BTC_USDT"))

	// 2. Process batched delta from v101 to v138 (MEXC begin: 101, end: 138)
	err = syncMgr.ProcessUpdate(ctx, &domain.OrderBook{
		Symbol:       "BTC_USDT",
		FirstVersion: 101,
		Version:      138,
		Bids: []domain.OrderBookEntry{
			{Price: 50050.0, Volume: 3.0},
		},
		Asks: []domain.OrderBookEntry{
			{Price: 50080.0, Volume: 1.5},
		},
	})
	require.NoError(t, err)

	syncMgr.Flush("BTC_USDT")

	// Orderbook should be ready and at version 138
	assert.Equal(t, orderbook.SyncStateReady, syncMgr.GetState("BTC_USDT"))
	bestBid, bestAsk, ok := syncMgr.GetBBO("BTC_USDT")
	assert.True(t, ok)
	assert.Equal(t, 50050.0, bestBid)
	assert.Equal(t, 50080.0, bestAsk)

	ob, ok := syncMgr.GetOrderBook("BTC_USDT")
	assert.True(t, ok)
	assert.Equal(t, int64(138), ob.Version)

	// 3. Process next contiguous batched delta from v139 to v150
	err = syncMgr.ProcessUpdate(ctx, &domain.OrderBook{
		Symbol:       "BTC_USDT",
		FirstVersion: 139,
		Version:      150,
		Bids: []domain.OrderBookEntry{
			{Price: 50060.0, Volume: 4.0},
		},
	})
	require.NoError(t, err)

	syncMgr.Flush("BTC_USDT")

	bestBid, _, ok = syncMgr.GetBBO("BTC_USDT")
	assert.True(t, ok)
	assert.Equal(t, 50060.0, bestBid)
}
