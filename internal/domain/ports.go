package domain

import (
	"context"
	"time"
)

// ──────────────────────────────────────────────────────────────────────
// Clock — server-synced time provider
// ──────────────────────────────────────────────────────────────────────.

// Clock provides access to server-synchronized time.
// Implemented by timesync.TimeSync.
type Clock interface {
	// Now returns the estimated current server time.
	Now() time.Time
	// Until returns the duration from server-now until the target time.
	Until(target time.Time) time.Duration
	// GetServerTime returns the estimated server time in milliseconds.
	GetServerTime() int64
	// LatencyMs returns the last measured round-trip time in milliseconds.
	LatencyMs() int64
	// Offset returns the current clock offset in milliseconds.
	Offset() int64
	// IsHealthy returns true if the time sync is in a good state.
	IsHealthy() bool
	// MsUntilTarget returns ms until a target server timestamp.
	MsUntilTarget(targetServerTimeMs int64) int64
	// Sleep blocks until the duration elapses or the context is cancelled.
	Sleep(ctx context.Context, d time.Duration) error
}

// ──────────────────────────────────────────────────────────────────────
// OrderPlacer — minimal order execution interface (ISP)
// ──────────────────────────────────────────────────────────────────────.

// OrderRequest represents a generic order placement request.
type OrderRequest struct {
	Symbol          string
	Price           float64
	Vol             float64
	Leverage        int
	Side            Side
	Type            int // OrderTypeLimit, OrderTypePostOnly, etc.
	OpenType        int // OpenTypeIsolated, OpenTypeCross
	ExternalOID     string
	PositionID      int64
	PositionMode    int
	ReduceOnly      bool
	StopLossPrice   float64
	TakeProfitPrice float64
}

// OrderPlacer provides order execution capabilities.
// This is a narrow interface following the Interface Segregation Principle.
type OrderPlacer interface {
	CreateOrder(ctx context.Context, req OrderRequest) (orderID string, err error)
	CancelOrder(ctx context.Context, symbol, orderID string) error
	CancelAllOpenOrders(ctx context.Context, symbol string) error
	CloseAllPositions(ctx context.Context, symbol string) error
}

// ──────────────────────────────────────────────────────────────────────
// MarketReader — minimal market data interface (ISP)
// ──────────────────────────────────────────────────────────────────────.

// MarketReader provides read-only access to market data.
type MarketReader interface {
	GetServerTime(ctx context.Context) (int64, error)
}
