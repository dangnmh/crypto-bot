// Package events defines all event types and topic constants for the
// Funding Reversion bot's event-driven cycle orchestration.
//
// Each topic represents a discrete phase transition or significant occurrence
// within a trading cycle. Handlers subscribe to topics and publish downstream
// events, forming an event chain that replaces the former FSM.
package events

import "time"

// ──────────────────────────────────────────────────────────────────────.
// Topic Constants — event chain flow.
// ──────────────────────────────────────────────────────────────────────.
//
// Pre-settle chain (sequential):
//
//	cycle.start → cycle.candidate.found → cycle.armed → cycle.wait.complete
//	→ cycle.confirmed → cycle.ioc.fired
//
// Post-settle fan-out (concurrent from cycle.ioc.fired):
//
//	cycle.ioc.fired  → [FillWatcher, TimeoutGuard, OBMonitor]
//	cycle.order.filled → TrailingHandler
//	cycle.trap.fired → FillWatcher (trap)
//	cycle.position.closed / cycle.timeout → CleanupHandler
const (
	// TopicCycleStart starts the pre-settle chain.
	TopicCycleStart     = "cycle.start"
	TopicCandidateFound = "cycle.candidate.found"
	TopicArmed          = "cycle.armed"
	TopicWaitComplete   = "cycle.wait.complete"
	TopicConfirmed      = "cycle.confirmed"

	// TopicIOCFired starts the execution phase.
	TopicIOCFired  = "cycle.ioc.fired"
	TopicTrapFired = "cycle.trap.fired"

	// TopicOrderFilled starts the post-settle phase.
	TopicOrderFilled    = "cycle.order.filled"
	TopicTrailingPlaced = "cycle.trailing.placed"
	TopicOBWallFound    = "cycle.ob.wall_found"
	TopicWallOrderFired = "cycle.wall_order.fired"
	TopicPositionClosed = "cycle.position.closed"
	TopicCycleTimeout   = "cycle.timeout"

	// TopicCycleDone signals the terminal phase.
	TopicCycleDone  = "cycle.done"
	TopicCycleAbort = "cycle.abort"
	TopicCycleError = "cycle.error"
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
	Symbol      string  `json:"symbol"`
	FundingRate float64 `json:"funding_rate"`
	Side        int     `json:"side"`
	CloseSide   int     `json:"close_side"`
	LastPrice   float64 `json:"last_price"`
}

// ArmedEvent is published when WS subscriptions are up and IOC params are calculated.
type ArmedEvent struct {
	Symbol   string    `json:"symbol"`
	Settle   time.Time `json:"settle"`
	Volume   float64   `json:"volume"`
	IOCPrice float64   `json:"ioc_price"`
	TPPrice  float64   `json:"tp_price"`
	SLPrice  float64   `json:"sl_price"`
}

// WaitCompleteEvent signals that the pre-settle wait period has ended.
type WaitCompleteEvent struct {
	Symbol string    `json:"symbol"`
	Settle time.Time `json:"settle"`
}

// ConfirmedEvent is published after recheck passes — ready to fire.
type ConfirmedEvent struct {
	Symbol      string  `json:"symbol"`
	FundingRate float64 `json:"funding_rate"`
	Side        int     `json:"side"`
	CloseSide   int     `json:"close_side"`
}

// IOCFiredEvent is published after the IOC order is submitted.
type IOCFiredEvent struct {
	Symbol    string    `json:"symbol"`
	OrderID   string    `json:"order_id"`
	Side      int       `json:"side"`
	CloseSide int       `json:"close_side"`
	Price     float64   `json:"price"`
	Volume    float64   `json:"volume"`
	TPPrice   float64   `json:"tp_price"`
	SLPrice   float64   `json:"sl_price"`
	Settle    time.Time `json:"settle"`
	Timestamp time.Time `json:"timestamp"`
}

// TrapFiredEvent is published after a trap order is placed.
type TrapFiredEvent struct {
	Symbol    string    `json:"symbol"`
	OrderID   string    `json:"order_id"`
	Side      int       `json:"side"`
	CloseSide int       `json:"close_side"`
	Price     float64   `json:"price"`
	Volume    float64   `json:"volume"`
	TPPrice   float64   `json:"tp_price"`
	SLPrice   float64   `json:"sl_price"`
	Source    string    `json:"source"` // "config" or "ob_monitor"
	Timestamp time.Time `json:"timestamp"`
}

// OrderFilledEvent is published when an order (IOC or Trap) is filled.
type OrderFilledEvent struct {
	Symbol       string  `json:"symbol"`
	OrderID      string  `json:"order_id"`
	Phase        string  `json:"phase"` // "ioc" or "trap"
	DealAvgPrice float64 `json:"deal_avg_price"`
	DealVol      float64 `json:"deal_vol"`
	Side         int     `json:"side"`
	CloseSide    int     `json:"close_side"`
}

// TrailingPlacedEvent is published after a trailing stop order is placed on MEXC.
type TrailingPlacedEvent struct {
	Symbol      string  `json:"symbol"`
	TrackID     string  `json:"track_id"`
	ActivePrice float64 `json:"active_price"`
	CallbackPct float64 `json:"callback_pct"`
	Phase       string  `json:"phase"` // "ioc" or "trap"
}

// OBWallFoundEvent is published when the post-settle OB monitor detects a wall.
type OBWallFoundEvent struct {
	Symbol    string  `json:"symbol"`
	WallPrice float64 `json:"wall_price"`
	WallVol   float64 `json:"wall_vol"`
	Side      int     `json:"side"`
}

// PositionClosedEvent signals that all positions for a symbol have been closed.
type PositionClosedEvent struct {
	Symbol string `json:"symbol"`
	Reason string `json:"reason"` // "trailing", "tp", "sl", "timeout", "manual"
}

// CycleTimeoutEvent is published when the safety timeout expires.
type CycleTimeoutEvent struct {
	Symbol  string        `json:"symbol"`
	Timeout time.Duration `json:"timeout"`
}

// CycleDoneEvent signals successful cycle completion.
type CycleDoneEvent struct {
	Symbol string `json:"symbol"`
}

// CycleAbortEvent signals cycle was aborted (e.g., FR too low, safety check failed).
type CycleAbortEvent struct {
	Symbol string `json:"symbol"`
	Reason string `json:"reason"`
	Phase  string `json:"phase"` // which phase triggered the abort
}

// CycleErrorEvent signals an unexpected error during the cycle.
type CycleErrorEvent struct {
	Symbol string `json:"symbol"`
	Error  string `json:"error"`
	Phase  string `json:"phase"`
}
