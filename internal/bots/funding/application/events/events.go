// Package events defines all event types and topic constants for the
// Funding Reversion bot's event-driven cycle orchestration.
//
// Each topic represents a discrete event transition or significant occurrence
// within a trading cycle. Handlers subscribe to topics and publish downstream
// events, forming an event chain that replaces the former FSM.
package events

import (
	"time"

	shared "crypto-bot/internal/domain"
)

// ──────────────────────────────────────────────────────────────────────.
// Topic Constants — flow-scoped event chain.
// ──────────────────────────────────────────────────────────────────────.
const (
	FlowReversion = "reversion"
	FlowTrap      = "trap"

	TopicScanStart          = "funding.scan.start"
	TopicScanCandidateFound = "funding.scan.candidate_found"
	TopicScanAbort          = "funding.scan.abort"

	TopicReversionCandidate      = "funding.reversion.candidate"
	TopicReversionArmed          = "funding.reversion.armed"
	TopicReversionWaitComplete   = "funding.reversion.wait_complete"
	TopicReversionConfirmed      = "funding.reversion.confirmed"
	TopicReversionIOCFired       = "funding.reversion.ioc_fired"
	TopicReversionOrderFilled    = "funding.reversion.order_filled"
	TopicReversionTrailingPlaced = "funding.reversion.trailing_placed"
	TopicReversionPositionClosed = "funding.reversion.position_closed"
	TopicReversionTimeout        = "funding.reversion.timeout"
	TopicReversionAbort          = "funding.reversion.abort"
	TopicReversionError          = "funding.reversion.error"

	TopicTrapCandidate      = "funding.trap.candidate"
	TopicTrapOrderPlaced    = "funding.trap.order_placed"
	TopicTrapOrderFilled    = "funding.trap.order_filled"
	TopicTrapTrailingPlaced = "funding.trap.trailing_placed"
	TopicTrapPositionClosed = "funding.trap.position_closed"
	TopicTrapTimeout        = "funding.trap.timeout"
	TopicTrapAbort          = "funding.trap.abort"
	TopicTrapError          = "funding.trap.error"
	TopicTrapOBWallFound    = "funding.trap.ob_wall_found"
	TopicTrapSkipped        = "funding.trap.skipped"
)

// ──────────────────────────────────────────────────────────────────────.
// Event Payloads.
// ──────────────────────────────────────────────────────────────────────.

// CycleStartEvent kicks off a new trading cycle.
type CycleStartEvent struct {
	Symbol     string    `json:"symbol"`
	SettleTime time.Time `json:"settle_time"`
}

// CandidateFoundEvent is published when FR scan finds a tradeable candidate.
type CandidateFoundEvent struct {
	Flow        string      `json:"flow,omitempty"`
	Symbol      string      `json:"symbol"`
	FundingRate float64     `json:"funding_rate"`
	Side        shared.Side `json:"side"`
	CloseSide   shared.Side `json:"close_side"`
	LastPrice   float64     `json:"last_price"`
}

// ArmedEvent is published when WS subscriptions are up and IOC params are calculated.
type ArmedEvent struct {
	Flow     string    `json:"flow"`
	Symbol   string    `json:"symbol"`
	Settle   time.Time `json:"settle"`
	Volume   float64   `json:"volume,omitempty"`
	IOCPrice float64   `json:"ioc_price,omitempty"`
	TPPrice  float64   `json:"tp_price,omitempty"`
	SLPrice  float64   `json:"sl_price,omitempty"`
}

// WaitCompleteEvent signals that the pre-settle wait period has ended.
type WaitCompleteEvent struct {
	Flow   string    `json:"flow"`
	Symbol string    `json:"symbol"`
	Settle time.Time `json:"settle"`
}

// ConfirmedEvent is published after recheck passes — ready to fire.
type ConfirmedEvent struct {
	Flow        string      `json:"flow"`
	Symbol      string      `json:"symbol"`
	FundingRate float64     `json:"funding_rate"`
	Side        shared.Side `json:"side"`
	CloseSide   shared.Side `json:"close_side"`
}

// IOCFiredEvent is published after the IOC order is submitted.
type IOCFiredEvent struct {
	Flow      string      `json:"flow"`
	Symbol    string      `json:"symbol"`
	OrderID   string      `json:"order_id"`
	Side      shared.Side `json:"side"`
	CloseSide shared.Side `json:"close_side"`
	Price     float64     `json:"price"`
	Volume    float64     `json:"volume"`
	TPPrice   float64     `json:"tp_price"`
	SLPrice   float64     `json:"sl_price"`
	Settle    time.Time   `json:"settle"`
	Timestamp time.Time   `json:"timestamp"`
}

// TrapFiredEvent is published after a trap order is placed.
type TrapFiredEvent struct {
	Flow      string      `json:"flow"`
	Symbol    string      `json:"symbol"`
	OrderID   string      `json:"order_id"`
	Side      shared.Side `json:"side"`
	CloseSide shared.Side `json:"close_side"`
	Price     float64     `json:"price"`
	Volume    float64     `json:"volume"`
	TPPrice   float64     `json:"tp_price"`
	SLPrice   float64     `json:"sl_price"`
	Source    string      `json:"source"` // "config" or "ob_monitor"
	Timestamp time.Time   `json:"timestamp"`
}

// OrderFilledEvent is published when an order (IOC or Trap) is filled.
type OrderFilledEvent struct {
	Flow         string      `json:"flow"`
	Symbol       string      `json:"symbol"`
	OrderID      string      `json:"order_id"`
	DealAvgPrice float64     `json:"deal_avg_price"`
	DealVol      float64     `json:"deal_vol"`
	Side         shared.Side `json:"side"`
	CloseSide    shared.Side `json:"close_side"`
}

// TrailingPlacedEvent is published after a trailing stop order is placed on MEXC.
type TrailingPlacedEvent struct {
	Flow        string  `json:"flow"`
	Symbol      string  `json:"symbol"`
	TrackID     string  `json:"track_id"`
	ActivePrice float64 `json:"active_price"`
	CallbackPct float64 `json:"callback_pct"`
}

// OBWallFoundEvent is published when the post-settle OB monitor detects a wall.
type OBWallFoundEvent struct {
	Flow            string      `json:"flow"`
	Symbol          string      `json:"symbol"`
	WallPrice       float64     `json:"wall_price"`
	WallVol         float64     `json:"wall_vol"`
	WallVerified    bool        `json:"wall_verified"`
	WallAgeMs       int64       `json:"wall_age_ms,omitempty"`
	WallDistancePct float64     `json:"wall_distance_pct,omitempty"`
	Side            shared.Side `json:"side"`
}

// TrapSkippedEvent is published when Trap intentionally ends before placing an order.
type TrapSkippedEvent struct {
	Flow      string    `json:"flow"`
	Symbol    string    `json:"symbol"`
	Reason    string    `json:"reason"`
	Source    string    `json:"source,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// PositionClosedEvent signals that all positions for a symbol have been closed.
type PositionClosedEvent struct {
	Flow   string `json:"flow"`
	Symbol string `json:"symbol"`
	Reason string `json:"reason"` // "trailing", "tp", "sl", "timeout", "manual"
}

// CycleTimeoutEvent is published when the safety timeout expires.
type CycleTimeoutEvent struct {
	Flow    string        `json:"flow"`
	Symbol  string        `json:"symbol"`
	Timeout time.Duration `json:"timeout"`
}

// CycleDoneEvent signals successful cycle completion.
type CycleDoneEvent struct {
	Flow   string `json:"flow"`
	Symbol string `json:"symbol"`
}

// CycleAbortEvent signals cycle was aborted (e.g., FR too low, safety check failed).
type CycleAbortEvent struct {
	Flow   string `json:"flow,omitempty"`
	Symbol string `json:"symbol"`
	Reason string `json:"reason"`
}

// CycleErrorEvent signals an unexpected error during the cycle.
type CycleErrorEvent struct {
	Flow   string `json:"flow"`
	Symbol string `json:"symbol"`
	Error  string `json:"error"`
}
