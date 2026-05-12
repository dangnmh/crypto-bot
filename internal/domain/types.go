// Package domain provides shared domain types used across all bots.
// This package MUST NOT import from infrastructure/ or any bot-specific package.
package domain

// ──────────────────────────────────────────────────────────────────────
// Side — trade direction
// ──────────────────────────────────────────────────────────────────────.

// Side represents a trade direction.
type Side int

const (
	SideOpenLong   Side = 1
	SideCloseShort Side = 2
	SideOpenShort  Side = 3
	SideCloseLong  Side = 4
)

// IsLong returns true if the side opens or closes a long position.
func (s Side) IsLong() bool {
	return s == SideOpenLong || s == SideCloseLong
}

// Opposite returns the inverse side (e.g., LONG -> SHORT).
func (s Side) Opposite() Side {
	switch s {
	case SideOpenLong:
		return SideOpenShort
	case SideOpenShort:
		return SideOpenLong
	case SideCloseLong:
		return SideCloseShort
	case SideCloseShort:
		return SideCloseLong
	default:
		return s
	}
}

// CloseSideFor returns the close side for a given open side.
func CloseSideFor(openSide Side) Side {
	if openSide == SideOpenLong {
		return SideCloseLong
	}
	return SideCloseShort
}

// String returns a human-readable label for the side.
func (s Side) String() string {
	switch s {
	case SideOpenLong:
		return "LONG"
	case SideOpenShort:
		return "SHORT"
	case SideCloseShort:
		return "CLOSE_SHORT"
	case SideCloseLong:
		return "CLOSE_LONG"
	default:
		return "UNKNOWN"
	}
}

// ──────────────────────────────────────────────────────────────────────
// Kline — candlestick data
// ──────────────────────────────────────────────────────────────────────.

// Kline represents a single candlestick bar.
type Kline struct {
	Timestamp int64
	Open      float64
	Close     float64
	High      float64
	Low       float64
	Volume    float64
	Amount    float64
}

// ──────────────────────────────────────────────────────────────────────
// OrderBook — depth data
// ──────────────────────────────────────────────────────────────────────.

// OrderBookEntry represents a single price level in the order book.
type OrderBookEntry struct {
	Price  float64
	Volume float64
}

// OrderBook represents the full or partial depth of a market.
type OrderBook struct {
	Symbol  string
	Version int64
	Asks    []OrderBookEntry // Sorted by price ascending (lowest ask first)
	Bids    []OrderBookEntry // Sorted by price descending (highest bid first)
}

// ──────────────────────────────────────────────────────────────────────
// OrderState — terminal state of an order
// ──────────────────────────────────────────────────────────────────────.

const (
	OrderStateFilled   = 3
	OrderStateCanceled = 4
	OrderStatePartial  = 5
)

// IsTerminalOrderState returns true if the order state is a terminal state.
func IsTerminalOrderState(state int) bool {
	return state == OrderStateFilled || state == OrderStateCanceled || state == OrderStatePartial
}

// ──────────────────────────────────────────────────────────────────────
// OrderType — execution strategy for an order
// ──────────────────────────────────────────────────────────────────────.

const (
	OrderTypeLimit    = 1
	OrderTypePostOnly = 2
	OrderTypeIOC      = 3
	OrderTypeFOK      = 4
	OrderTypeMarket   = 5
)

// ──────────────────────────────────────────────────────────────────────
// OpenType — margin mode
// ──────────────────────────────────────────────────────────────────────.

const (
	OpenTypeIsolated = 1
	OpenTypeCross    = 2
)

// ──────────────────────────────────────────────────────────────────────
// Interval — kline interval constants
// ──────────────────────────────────────────────────────────────────────.

const (
	IntervalMin1 = "Min1"
)
