package orderbook

import (
	"context"
	"fmt"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"

	"github.com/go-playground/validator/v10"
)

// SyncMode defines how the orderbook is synchronized.
type SyncMode string

const (
	// SyncModeSnapshot is for exchanges pushing full top-N depth snapshots on every WS event (e.g. Toobit).
	SyncModeSnapshot SyncMode = "SNAPSHOT"

	// SyncModeIncremental is for exchanges pushing incremental diffs requiring REST snapshot + sequential delta application (e.g. MEXC, Binance).
	SyncModeIncremental SyncMode = "INCREMENTAL"
)

// SyncState represents the synchronization status of a local orderbook.
type SyncState string

const (
	SyncStateUninitialized SyncState = "UNINITIALIZED"
	SyncStateSyncing       SyncState = "SYNCING"
	SyncStateReady         SyncState = "READY"
	SyncStateRecovering    SyncState = "RECOVERING"
)

// DepthSnapshotProvider is an alias to exchange.DepthProvider.
type DepthSnapshotProvider = exchange.DepthProvider

// DepthCommit is an alias to exchange.DepthCommit.
type DepthCommit = exchange.DepthCommit

// DepthCommitsProvider is an alias to exchange.DepthCommitsProvider.
type DepthCommitsProvider = exchange.DepthCommitsProvider

// OrderBookReader exposes read-only access to local orderbooks.
type OrderBookReader interface {
	GetOrderBook(symbol string) (*domain.OrderBook, bool)
	GetTopN(symbol string, limit int) (*domain.OrderBook, bool)
	GetBBO(symbol string) (bestBid, bestAsk float64, ok bool)
	GetState(symbol string) SyncState
}

// OrderBookUpdater handles incoming WebSocket depth updates.
type OrderBookUpdater interface {
	ProcessUpdate(ctx context.Context, ob *domain.OrderBook) error
}

// Synchronizer is the composite interface for multi-exchange orderbook management.
type Synchronizer interface {
	OrderBookReader
	OrderBookUpdater
	InitializeSymbol(ctx context.Context, symbol string) error
	RemoveSymbol(symbol string)
	Close() error
}

// SynchronizerConfig configures the behavior of the OrderBookSynchronizer.
type SynchronizerConfig struct {
	Exchange           string        `json:"exchange" validate:"required"`
	Mode               SyncMode      `json:"mode" validate:"required,oneof=SNAPSHOT INCREMENTAL"`
	StrictSequence     bool          `json:"strictSequence"` // If true, strictly requires version == currVersion + 1 (e.g. KuCoin); if false, allows monotonic versioned deltas (e.g. MEXC).
	MaxBufferCapacity  int           `json:"maxBufferCapacity" validate:"gt=0"`
	SnapshotTimeout    time.Duration `json:"snapshotTimeout" validate:"gt=0"`
	StaleTimeout       time.Duration `json:"staleTimeout"`
	CommitRecoverySize int           `json:"commitRecoverySize" validate:"gt=0"`
}

var _validate = validator.New()

// Validate verifies that the synchronizer configuration is valid.
func (c *SynchronizerConfig) Validate() error {
	if err := _validate.Struct(c); err != nil {
		return fmt.Errorf("invalid synchronizer config: %w", err)
	}
	return nil
}
