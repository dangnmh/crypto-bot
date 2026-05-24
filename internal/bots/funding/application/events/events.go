// Package events defines all event types and topic constants for the
// Funding Reversion bot's event-driven cycle orchestration.
//
// Each topic represents a discrete event transition or significant occurrence
// within a trading cycle. Handlers subscribe to topics and publish downstream
// events, forming an event chain that replaces the former FSM.
package events

import (
	"encoding/json"
	"time"

	shared "crypto-bot/internal/domain"
)

// Notifiable allows an event to specify if it should trigger a notification.
type Notifiable interface {
	ShouldNotify() bool
}

// BaseEvent provides common fields for events that support notifications.
type BaseEvent struct {
	SendNotify bool `json:"sendNotify,omitempty"`
}

func (b BaseEvent) ShouldNotify() bool {
	return b.SendNotify
}

// JournalEnvelope is the event message written to the journal file.
// This serves as the single source of truth for cycle audit trails.
type JournalEnvelope struct {
	Seq        int64           `json:"seq"`
	Time       time.Time       `json:"time"`
	ReqID      string          `json:"req_id"`
	Symbol     string          `json:"symbol"`
	SettleTime time.Time       `json:"settle_time"`
	Flow       string          `json:"flow,omitempty"`
	Topic      string          `json:"topic"`
	Payload    json.RawMessage `json:"payload"`
}

// ──────────────────────────────────────────────────────────────────────.
// Topic Constants — flow-scoped event chain.
// ──────────────────────────────────────────────────────────────────────.
const (
	FlowReversion = "reversion"
	FlowTrap      = "trap"
	FlowScan      = "scan"
	FlowCycle     = "cycle"

	TopicScanStart          = "funding.scan.start"
	TopicScanCandidateFound = "funding.scan.candidate_found"
	TopicScanAbort          = "funding.scan.abort"

	TopicCycleStarted = "funding.cycle.started"

	TopicReversionCandidate      = "funding.reversion.candidate"
	TopicReversionArmed          = "funding.reversion.armed"
	TopicReversionWaitComplete   = "funding.reversion.wait_complete"
	TopicReversionConfirmed      = "funding.reversion.confirmed"
	TopicReversionIOCFired       = "funding.reversion.ioc_fired"
	TopicReversionOrderFilled    = "funding.reversion.order_filled"
	TopicReversionPositionClosed = "funding.reversion.position_closed"
	TopicReversionTimeout        = "funding.reversion.timeout"
	TopicReversionAbort          = "funding.reversion.abort"
	TopicReversionError          = "funding.reversion.error"
	TopicExcursionPriceObserved  = "funding.excursion.price_observed"
	TopicCleanupStarted          = "funding.cleanup.started"
	TopicCleanupCompleted        = "funding.cleanup.completed"
	TopicCycleCompleted          = "funding.cycle.completed"

	TopicTrapCandidate      = "funding.trap.candidate"
	TopicTrapOrderPlaced    = "funding.trap.order_placed"
	TopicTrapOrderFilled    = "funding.trap.order_filled"
	TopicTrapOrderSubmitted = "funding.trap.order_submitted"
	TopicTrapTrailingPlaced = "funding.trap.trailing_placed"
	TopicTrapPositionClosed = "funding.trap.position_closed"
	TopicTrapTimeout        = "funding.trap.timeout"
	TopicTrapTimeoutStarted = "funding.trap.timeout_started"
	TopicTrapTimedOut       = "funding.trap.timed_out"
	TopicTrapAbort          = "funding.trap.abort"
	TopicTrapError          = "funding.trap.error"
	TopicTrapOBWallFound    = "funding.trap.ob_wall_found"
	// nolint:gosec,godoclint // topic name contains "wall" which is not credentials; const in grouped block
	TopicTrapWallVerified = "trap.wall_verified"
	TopicTrapSkipped      = "funding.trap.skipped"

	TopicSymbolDisabled = "funding.system.symbol_disabled"

	// WS Order Events — order lifecycle from WebSocket callbacks.

	TopicOrderSubmitted = "funding.order.submitted"
	TopicOrderFilled    = "funding.order.filled"
	TopicOrderCancelled = "funding.order.cancelled"
	TopicOrderRejected  = "funding.order.rejected"

	TopicDealReceived = "funding.deal.received"

	TopicPositionUpdated = "funding.position.updated"

	TopicCycleFinalPnL = "funding.cycle.final_pnl"
)

// ──────────────────────────────────────────────────────────────────────.
// Event Payloads.
// ──────────────────────────────────────────────────────────────────────.

// CycleStartEvent kicks off a new trading cycle.
type CycleStartEvent struct {
	// Required
	Symbol     string    `json:"symbol"`
	SettleTime time.Time `json:"settle_time"`

	// Additional
	Config        json.RawMessage `json:"config,omitempty"`
	TakeProfitPct float64         `json:"take_profit_pct,omitempty"`
	StopLossPct   float64         `json:"stop_loss_pct,omitempty"`
}

// CandidateFoundEvent is published when FR scan finds a tradeable candidate.
type CandidateFoundEvent struct {
	// Required (kế thừa từ cycle start)
	Flow string `json:"flow"` // "reversion"

	// Required (core data)
	Symbol      string      `json:"symbol"`
	FundingRate float64     `json:"funding_rate"` // FR lúc scan
	Side        shared.Side `json:"side"`         // long/short
	CloseSide   shared.Side `json:"close_side"`
	LastPrice   float64     `json:"last_price"` // Reference price
	BestBid     float64     `json:"best_bid"`
	BestAsk     float64     `json:"best_ask"`

	// Additional
	Vol24h    float64 `json:"vol24h,omitempty"`
	SpreadPct float64 `json:"spread_pct,omitempty"`
}

// ArmedEvent is published when WS subscriptions are up and IOC params are calculated.
type ArmedEvent struct {
	// Required (kế thừa từ candidate)
	Flow        string      `json:"flow"`
	Symbol      string      `json:"symbol"`
	FundingRate float64     `json:"funding_rate"`
	Side        shared.Side `json:"side"`
	CloseSide   shared.Side `json:"close_side"`
	LastPrice   float64     `json:"last_price"`
	BestBid     float64     `json:"best_bid"`
	BestAsk     float64     `json:"best_ask"`

	// Additional (filter results)
	SafetyPassed       bool    `json:"safety_passed"`
	SafetyRejectReason string  `json:"safety_reject_reason,omitempty"`
	Volume             float64 `json:"volume,omitempty"`
	DesiredNotional    float64 `json:"desired_notional_usdt,omitempty"`
	ActualNotional     float64 `json:"actual_notional_usdt,omitempty"`
	MaxSafeNotional    float64 `json:"max_safe_notional_usdt,omitempty"`
}

// WaitCompleteEvent signals that the pre-settle wait period has ended.
type WaitCompleteEvent struct {
	// Required
	Flow       string    `json:"flow"`
	Symbol     string    `json:"symbol"`
	SettleTime time.Time `json:"settle_time"`

	// Additional
	WaitDurationMs int64 `json:"wait_duration_ms,omitempty"`
}

// ConfirmedEvent is published after recheck passes — ready to fire.
type ConfirmedEvent struct {
	// Required (kế thừa)
	Flow        string      `json:"flow"`
	Symbol      string      `json:"symbol"`
	FundingRate float64     `json:"funding_rate"` // FR tại recheck
	Side        shared.Side `json:"side"`
	CloseSide   shared.Side `json:"close_side"`

	// Additional
	FRChanged bool    `json:"fr_changed,omitempty"` // So với scan
	LastPrice float64 `json:"last_price,omitempty"`
}

// IOCFiredEvent is published after the IOC order is submitted.
type IOCFiredEvent struct {
	// Required
	Flow          string      `json:"flow"`
	Symbol        string      `json:"symbol"`
	OrderID       string      `json:"order_id"`
	Side          shared.Side `json:"side"`
	CloseSide     shared.Side `json:"close_side"`
	OrderType     int         `json:"order_type,omitempty"`
	IntendedPrice float64     `json:"intended_price"` // Giá định bắn
	FireTimestamp time.Time   `json:"fire_timestamp"` // Server time khi bắn
	Volume        float64     `json:"volume"`
	SettleTime    time.Time   `json:"settle_time"`

	// Additional
	TPPrice      float64 `json:"tp_price,omitempty"`
	SLPrice      float64 `json:"sl_price,omitempty"`
	LatencyRTTMs int64   `json:"latency_rtt_ms,omitempty"`
	Error        string  `json:"error,omitempty"`
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
	BaseEvent `json:"-"`
	// Required
	Flow      string      `json:"flow"`
	Symbol    string      `json:"symbol"`
	OrderID   string      `json:"order_id"`
	Side      shared.Side `json:"side"`
	CloseSide shared.Side `json:"close_side"`
	FillPrice float64     `json:"fill_price"` // Giá thực tế
	FillVol   float64     `json:"fill_vol"`   // Vol thực tế

	// Additional
	Fee         float64 `json:"fee,omitempty"`
	Profit      float64 `json:"profit,omitempty"`
	HoldFee     float64 `json:"hold_fee,omitempty"` // Funding fee - IOC quá sớm?
	SlippagePct float64 `json:"slippage_pct,omitempty"`
	TPPrice     float64 `json:"tp_price,omitempty"`
	SLPrice     float64 `json:"sl_price,omitempty"`
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
	Error     string    `json:"error,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// TrapWallVerifiedEvent is published when the trap wall is verified.
type TrapWallVerifiedEvent struct {
	Flow            string      `json:"flow"`
	Symbol          string      `json:"symbol"`
	WallPrice       float64     `json:"wall_price"`
	WallVerified    bool        `json:"wall_verified"`
	WallAgeMs       int64       `json:"wall_age_ms,omitempty"`
	WallDistancePct float64     `json:"wall_distance_pct,omitempty"`
	Side            shared.Side `json:"side"`
}

// TrapOrderSubmittedEvent is published when a trap order is submitted.
type TrapOrderSubmittedEvent struct {
	Flow            string      `json:"flow"`
	Symbol          string      `json:"symbol"`
	Source          string      `json:"source,omitempty"`
	OrderID         string      `json:"order_id"`
	Side            shared.Side `json:"side"`
	CloseSide       shared.Side `json:"close_side"`
	Price           float64     `json:"price"`
	Volume          float64     `json:"volume"`
	TPPrice         float64     `json:"tp_price"`
	SLPrice         float64     `json:"sl_price"`
	TPPct           float64     `json:"tp_pct,omitempty"`
	SLPct           float64     `json:"sl_pct,omitempty"`
	WallPrice       float64     `json:"wall_price,omitempty"`
	WallVerified    bool        `json:"wall_verified,omitempty"`
	WallAgeMs       int64       `json:"wall_age_ms,omitempty"`
	WallDistancePct float64     `json:"wall_distance_pct,omitempty"`
}

// TimeoutStartedEvent is published when a timeout guard starts.
type TimeoutStartedEvent struct {
	Flow       string    `json:"flow"`
	Symbol     string    `json:"symbol"`
	DurationMs int64     `json:"duration_ms"`
	StartedAt  time.Time `json:"started_at"`
}

// TimeoutFiredEvent is published when a timeout fires.
type TimeoutFiredEvent struct {
	Flow            string    `json:"flow"`
	Symbol          string    `json:"symbol"`
	DurationMs      int64     `json:"duration_ms"`
	StartedAt       time.Time `json:"started_at"`
	FiredAt         time.Time `json:"fired_at"`
	CloseRetryCount int       `json:"close_retry_count,omitempty"`
	Error           string    `json:"error,omitempty"`
}

// PositionClosedEvent signals that all positions for a symbol have been closed.
type PositionClosedEvent struct {
	// Required
	Flow       string  `json:"flow"`
	Symbol     string  `json:"symbol"`
	ClosePrice float64 `json:"close_price"`
	CloseVol   float64 `json:"close_vol"`
	Reason     string  `json:"reason"` // "trailing", "tp", "sl", "timeout", "manual"

	// Additional
	Profit          float64 `json:"profit,omitempty"`
	NetProfit       float64 `json:"net_profit,omitempty"`
	Fee             float64 `json:"fee,omitempty"`
	HoldDurationMs  int64   `json:"hold_duration_ms,omitempty"`
	TPPriceTouched  bool    `json:"tp_price_touched,omitempty"`
	SLPriceTouched  bool    `json:"sl_price_touched,omitempty"`
	Method          string  `json:"method,omitempty"` // order.close / fallback
	CloseRetryCount int     `json:"close_retry_count,omitempty"`
}

// CycleTimeoutEvent is published when the safety timeout expires.
type CycleTimeoutEvent struct {
	// Required
	Flow    string        `json:"flow"`
	Symbol  string        `json:"symbol"`
	Timeout time.Duration `json:"timeout"`
	Reason  string        `json:"reason"` // "no_fill", "force_close"

	// Additional
	ForceCloseAttempted bool   `json:"force_close_attempted,omitempty"`
	ForceCloseSucceeded bool   `json:"force_close_succeeded,omitempty"`
	CloseRetryCount     int    `json:"close_retry_count,omitempty"`
	Error               string `json:"error,omitempty"`
}

// CycleDoneEvent signals successful cycle completion.
type CycleDoneEvent struct {
	Flow   string `json:"flow"`
	Symbol string `json:"symbol"`
}

// CycleAbortEvent signals cycle was aborted (e.g., FR too low, safety check failed).
type CycleAbortEvent struct {
	BaseEvent `json:"-"`
	Flow      string `json:"flow,omitempty"`
	Symbol    string `json:"symbol"`
	Reason    string `json:"reason"`
}

// CycleErrorEvent signals an unexpected error during the cycle.
type CycleErrorEvent struct {
	BaseEvent `json:"-"`
	Flow      string `json:"flow"`
	Symbol    string `json:"symbol"`
	Error     string `json:"error"`
}

// SymbolDisabledEvent is published when a symbol is disabled due to critical failure.
type SymbolDisabledEvent struct {
	BaseEvent  `json:"-"`
	Symbol     string `json:"symbol"`
	Reason     string `json:"reason"`
	Source     string `json:"source"` // "timeout_force_close", "fallback_close"
	RetryCount int    `json:"retry_count"`
}

// CleanupStartedEvent is published when cleanup starts.
type CleanupStartedEvent struct {
	TerminalFlow string    `json:"terminal_flow"`
	TerminalType string    `json:"terminal_type"`
	Reason       string    `json:"reason"`
	StartedAt    time.Time `json:"started_at"`
}

// CleanupCompletedEvent is published when cleanup completes.
type CleanupCompletedEvent struct {
	TerminalFlow         string    `json:"terminal_flow"`
	TerminalType         string    `json:"terminal_type"`
	Reason               string    `json:"reason"`
	StartedAt            time.Time `json:"started_at"`
	CompletedAt          time.Time `json:"completed_at"`
	Unsubscribed         bool      `json:"unsubscribed"`
	ExcursionFinalized   bool      `json:"excursion_finalized"`
	TrapCancelRetryCount int       `json:"trap_cancel_retry_count,omitempty"`
	TrapCloseRetryCount  int       `json:"trap_close_retry_count,omitempty"`
	TrapCleanupError     string    `json:"trap_cleanup_error,omitempty"`
}

// CycleCompletedEvent is published when the cycle completes successfully.
type CycleCompletedEvent struct {
	Reason string `json:"reason"`
}

// FinalPnLEvent is published at cycle end with the total PnL summary.
type FinalPnLEvent struct {
	BaseEvent      `json:"-"`        // Embedded for notification control
	Symbol         string            `json:"symbol"`
	TotalPnL       float64           `json:"total_pnl"`
	IocPnL         float64           `json:"ioc_pnl"`
	TrapPnL        float64           `json:"trap_pnl"`
	FundingFeePaid float64           `json:"funding_fee_paid"`
	TradingFees    float64           `json:"trading_fees"`
	NetPnL         float64           `json:"net_pnl"`
	ClosePrice     float64           `json:"close_price"`
	HoldDurationMs int64             `json:"hold_duration_ms"`
	EventCount     int               `json:"event_count"`
	Journey        []JournalEnvelope `json:"journey,omitempty"`
}

// WS Order Event Payloads — from WebSocket callbacks (distinct from flow events).

// WSOrderSubmittedEvent is published when an order is submitted to the exchange.
type WSOrderSubmittedEvent struct {
	OrderID   string    `json:"order_id"`
	ExtOID    string    `json:"ext_oid,omitempty"`
	Symbol    string    `json:"symbol"`
	Side      string    `json:"side"`       // "OPEN_LONG", "OPEN_SHORT", "CLOSE_LONG", "CLOSE_SHORT"
	OrderType string    `json:"order_type"` // "IOC", "LIMIT_TRAP", "TRAILING_STOP"
	Price     float64   `json:"price"`
	Volume    float64   `json:"volume"`
	Timestamp time.Time `json:"timestamp"`
	Error     string    `json:"error,omitempty"`
}

// WSOrderFilledEvent is published when an order is filled (from WS deal callback).
type WSOrderFilledEvent struct {
	OrderID   string    `json:"order_id"`
	Symbol    string    `json:"symbol"`
	Side      string    `json:"side"`
	FillPrice float64   `json:"fill_price"`
	FillVol   float64   `json:"fill_vol"`
	Fee       float64   `json:"fee,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// WSOrderCancelledEvent is published when an order is cancelled.
type WSOrderCancelledEvent struct {
	OrderID   string    `json:"order_id"`
	Symbol    string    `json:"symbol"`
	Side      string    `json:"side"`
	Reason    string    `json:"reason,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// WSOrderRejectedEvent is published when an order is rejected by the exchange.
type WSOrderRejectedEvent struct {
	BaseEvent `json:"-"`
	OrderID   string    `json:"order_id"`
	Symbol    string    `json:"symbol"`
	Side      string    `json:"side"`
	Error     string    `json:"error"`
	Timestamp time.Time `json:"timestamp"`
}

// WSDealReceivedEvent is published when an execution deal is received from WebSocket.
type WSDealReceivedEvent struct {
	OrderID   string    `json:"order_id"`
	Symbol    string    `json:"symbol"`
	Side      int       `json:"side"` // 1=long, -1=short
	DealPrice float64   `json:"deal_price"`
	DealVol   float64   `json:"deal_vol"`
	DealTime  int64     `json:"deal_time"`
	Timestamp time.Time `json:"timestamp"`
}

// WSPositionUpdatedEvent is published when position exposure is updated.
type WSPositionUpdatedEvent struct {
	Symbol        string    `json:"symbol"`
	Side          int       `json:"side"` // 1=long, -1=short, 0=closed
	Size          float64   `json:"size"`
	EntryPrice    float64   `json:"entry_price"`
	MarkPrice     float64   `json:"mark_price"`
	UnrealizedPnL float64   `json:"unrealized_pnl"`
	Leverage      int       `json:"leverage"`
	Timestamp     time.Time `json:"timestamp"`
}
