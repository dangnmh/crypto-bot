// Package domain provides shared domain types used across all bots.
// This package MUST NOT import from infrastructure/ or any bot-specific package.
package domain

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// ──────────────────────────────────────────────────────────────────────
// Side — trade direction
// ──────────────────────────────────────────────────────────────────────.

// Side represents a trade direction.
type Side int

const (
	SideUnknown    Side = 0
	SideOpenLong   Side = 1
	SideCloseShort Side = 2
	SideOpenShort  Side = 3
	SideCloseLong  Side = 4
)

const (
	sideLabelUnknown    = "UNKNOWN"
	sideLabelLong       = "LONG"
	sideLabelOpenLong   = "OPEN_LONG"
	sideLabelCloseShort = "CLOSE_SHORT"
	sideLabelShort      = "SHORT"
	sideLabelOpenShort  = "OPEN_SHORT"
	sideLabelCloseLong  = "CLOSE_LONG"
)

var sideByString = map[string]Side{
	sideLabelUnknown:    SideUnknown,
	sideLabelLong:       SideOpenLong,
	sideLabelOpenLong:   SideOpenLong,
	sideLabelCloseShort: SideCloseShort,
	sideLabelShort:      SideOpenShort,
	sideLabelOpenShort:  SideOpenShort,
	sideLabelCloseLong:  SideCloseLong,
}

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
	case SideUnknown:
		return sideLabelUnknown
	case SideOpenLong:
		return sideLabelLong
	case SideOpenShort:
		return sideLabelShort
	case SideCloseShort:
		return sideLabelCloseShort
	case SideCloseLong:
		return sideLabelCloseLong
	default:
		return sideLabelUnknown
	}
}

// MarshalJSON encodes Side as its enum label instead of its exchange numeric code.
func (s Side) MarshalJSON() ([]byte, error) {
	if !s.IsValid() {
		return nil, fmt.Errorf("invalid side: %d", s)
	}
	return json.Marshal(s.String())
}

// UnmarshalJSON accepts either the enum label (for readable config/events) or
// the exchange numeric code (for compatibility with older JSON records).
func (s *Side) UnmarshalJSON(data []byte) error {
	var label string
	if err := json.Unmarshal(data, &label); err == nil {
		side, err := ParseSide(label)
		if err != nil {
			return err
		}
		*s = side
		return nil
	}

	var numeric int
	if err := json.Unmarshal(data, &numeric); err != nil {
		return fmt.Errorf("parse side: %w", err)
	}
	side := Side(numeric)
	if !side.IsValid() {
		return fmt.Errorf("invalid side: %d", numeric)
	}
	*s = side
	return nil
}

// IsValid returns true when the side is one of the known enum values.
func (s Side) IsValid() bool {
	switch s {
	case SideUnknown, SideOpenLong, SideCloseShort, SideOpenShort, SideCloseLong:
		return true
	default:
		return false
	}
}

// ParseSide converts a side enum label into its numeric value.
func ParseSide(value string) (Side, error) {
	normalized := strings.ToUpper(strings.TrimSpace(value))
	if normalized == "" {
		return 0, fmt.Errorf("side is empty")
	}

	if numeric, err := strconv.Atoi(normalized); err == nil {
		side := Side(numeric)
		if side.IsValid() {
			return side, nil
		}
		return 0, fmt.Errorf("invalid side: %d", numeric)
	}

	side, ok := sideByString[normalized]
	if !ok {
		return 0, fmt.Errorf("invalid side: %q", value)
	}
	return side, nil
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
