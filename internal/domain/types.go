// Package domain provides shared domain types used across all bots.
// This package MUST NOT import from infrastructure/ or any bot-specific package.
package domain

import (
	"database/sql/driver"
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

// IsClose returns true if the side is a position closing order (SideCloseLong or SideCloseShort).
func (s Side) IsClose() bool {
	return s == SideCloseLong || s == SideCloseShort
}

// IsOpen returns true if the side is a position opening order (SideOpenLong or SideOpenShort).
func (s Side) IsOpen() bool {
	return s == SideOpenLong || s == SideOpenShort
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

// Value implements driver.Valuer for database SQL/GORM serialization.
func (s Side) Value() (driver.Value, error) {
	return s.String(), nil
}

// Scan implements sql.Scanner for database SQL/GORM deserialization.
func (s *Side) Scan(value any) error {
	if value == nil {
		*s = SideUnknown
		return nil
	}
	switch v := value.(type) {
	case string:
		parsed, err := ParseSide(v)
		if err != nil {
			return fmt.Errorf("cannot scan string %q into Side: %w", v, err)
		}
		*s = parsed
		return nil
	case []byte:
		parsed, err := ParseSide(string(v))
		if err != nil {
			return fmt.Errorf("cannot scan bytes %q into Side: %w", string(v), err)
		}
		*s = parsed
		return nil
	case int64:
		*s = Side(v)
		return nil
	default:
		return fmt.Errorf("cannot scan %T into Side", value)
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
	Symbol       string
	FirstVersion int64            // Starting commit version for batched updates (e.g. MEXC data.begin)
	Version      int64            // Ending commit version / sequence (e.g. MEXC data.end / KuCoin sequence)
	Asks         []OrderBookEntry // Sorted by price ascending (lowest ask first)
	Bids         []OrderBookEntry // Sorted by price descending (highest bid first)
}

// ──────────────────────────────────────────────────────────────────────
// OrderState — terminal state of an order
// ──────────────────────────────────────────────────────────────────────.

// OrderState represents the terminal or execution state of an order.
type OrderState int

const (
	OrderStateNew             OrderState = 0
	OrderStatePartiallyFilled OrderState = 1
	OrderStateFilled          OrderState = 3
	OrderStateCanceled        OrderState = 4
	OrderStatePartial         OrderState = 5
	OrderStateUntriggered     OrderState = 6
)

// IsNotFilledOrderState returns true if the order state is a not filled state.
func IsNotFilledOrderState(state OrderState) bool {
	return state == OrderStateNew || state == OrderStateCanceled || state == OrderStateUntriggered
}

// IsTerminalOrderState returns true if the order state is a terminal state.
func IsTerminalOrderState(state OrderState) bool {
	return state == OrderStateFilled || state == OrderStateCanceled || state == OrderStatePartial
}

// ──────────────────────────────────────────────────────────────────────
// OrderType — execution strategy for an order
// ──────────────────────────────────────────────────────────────────────.

// OrderType represents the execution type of an order.
type OrderType int

const (
	OrderTypeLimit    OrderType = 1
	OrderTypePostOnly OrderType = 2
	OrderTypeIOC      OrderType = 3
	OrderTypeFOK      OrderType = 4
	OrderTypeMarket   OrderType = 5
)

// ──────────────────────────────────────────────────────────────────────
// OpenType — margin mode
// ──────────────────────────────────────────────────────────────────────.

// OpenType represents the margin mode (isolated vs cross).
type OpenType int

const (
	OpenTypeIsolated OpenType = 1
	OpenTypeCross    OpenType = 2
)

type MarginMode string

const (
	MarginModeIsolated MarginMode = "ISOLATED"
	MarginModeCross    MarginMode = "CROSS"
)

// ──────────────────────────────────────────────────────────────────────
// PositionMode — position mode setting
// ──────────────────────────────────────────────────────────────────────.

// PositionMode represents the position mode setting (hedge vs one-way).
type PositionMode int

const (
	PositionModeHedge  PositionMode = 1
	PositionModeOneWay PositionMode = 2
)

// ──────────────────────────────────────────────────────────────────────
// Interval — kline interval constants
// ──────────────────────────────────────────────────────────────────────.

// Interval represents the timeframe interval for candlesticks.
type Interval string

const (
	IntervalMin1 Interval = "Min1"
)
