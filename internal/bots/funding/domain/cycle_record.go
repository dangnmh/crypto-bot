package domain

import (
	"encoding/json"
	"time"

	shared "crypto-bot/internal/domain"
)

// ──────────────────────────────────────────────────────────────────────
// CycleRecord — complete audit trail for one trading cycle.
// ──────────────────────────────────────────────────────────────────────.

// CycleRecord captures the complete audit trail for a single trading cycle.
// It is a value object — immutable after construction.
type CycleRecord struct {
	// Identity
	SchemaVersion int       `json:"schema_version"`
	ReqID         string    `json:"req_id"`
	Symbol        string    `json:"symbol"`
	SettleTime    time.Time `json:"settle_time"`
	CreatedAt     time.Time `json:"created_at"`
	Flows         []string  `json:"flows,omitempty"`

	// Outcome
	Outcome     CycleOutcome `json:"outcome"`
	AbortReason string       `json:"abort_reason,omitempty"`
	AbortPhase  Phase        `json:"abort_phase,omitempty"`

	// Decision
	Decision DecisionSnapshot `json:"decision"`

	// Execution
	IOC  IOCSnapshot  `json:"ioc"`
	Trap TrapSnapshot `json:"trap"`

	// Exit
	Exit ExitSnapshot `json:"exit"`

	// MFE/MAE (Maximum Favorable / Adverse Excursion)
	Excursion     ExcursionSnapshot `json:"excursion,omitempty"` // legacy alias for ioc_excursion
	IOCExcursion  ExcursionSnapshot `json:"ioc_excursion,omitempty"`
	TrapExcursion ExcursionSnapshot `json:"trap_excursion,omitempty"`

	// Config active during this cycle (full JSON for reproducibility)
	Config json.RawMessage `json:"config"`

	// Full event timeline from EventBus
	Timeline []TimelineEntry `json:"timeline"`
}

// ──────────────────────────────────────────────────────────────────────
// CycleOutcome — the final outcome of a cycle.
// ──────────────────────────────────────────────────────────────────────.

// CycleOutcome enumerates possible cycle endings.
type CycleOutcome string

const (
	OutcomeProfit  CycleOutcome = "profit"
	OutcomeLoss    CycleOutcome = "loss"
	OutcomeAborted CycleOutcome = "aborted"
	OutcomeTimeout CycleOutcome = "timeout"
	OutcomeNoFill  CycleOutcome = "no_fill"
)

// ──────────────────────────────────────────────────────────────────────
// Snapshot sub-types — one per cycle phase.
// ──────────────────────────────────────────────────────────────────────.

// DecisionSnapshot captures why we entered (or didn't enter) a trade.
type DecisionSnapshot struct {
	FRAtScan           float64     `json:"fr_at_scan"`
	FRAtRecheck        float64     `json:"fr_at_recheck,omitempty"`
	FRChanged          bool        `json:"fr_changed,omitempty"`
	Side               shared.Side `json:"side"`
	SafetyPassed       bool        `json:"safety_passed"`
	SafetyRejectReason string      `json:"safety_reject_reason,omitempty"`
}

// IOCSnapshot captures IOC order execution details.
type IOCSnapshot struct {
	Flow           string            `json:"flow,omitempty"`
	IntendedPrice  float64           `json:"intended_price,omitempty"`
	FillPrice      float64           `json:"fill_price,omitempty"`
	FillVolume     float64           `json:"fill_volume,omitempty"`
	Filled         bool              `json:"filled"`
	SlippagePct    float64           `json:"slippage_pct,omitempty"`
	OrderID        string            `json:"order_id,omitempty"`
	Error          string            `json:"error,omitempty"`
	FireTimestamp  time.Time         `json:"fire_timestamp,omitempty"`
	SettleOffsetMs int64             `json:"settle_offset_ms,omitempty"`
	LatencyRTTMs   int64             `json:"latency_rtt_ms,omitempty"`
	Excursion      ExcursionSnapshot `json:"excursion,omitempty"`
}

// TrapSnapshot captures hedge trap order details.
type TrapSnapshot struct {
	Flow             string            `json:"flow,omitempty"`
	Enabled          bool              `json:"enabled"`
	Source           string            `json:"source,omitempty"` // "ob_monitor" or "static_limit"
	Price            float64           `json:"price,omitempty"`
	Filled           bool              `json:"filled"`
	FillPrice        float64           `json:"fill_price,omitempty"`
	FillVolume       float64           `json:"fill_volume,omitempty"`
	OrderID          string            `json:"order_id,omitempty"`
	TPPctConfigured  float64           `json:"tp_pct_configured,omitempty"`
	SLPctConfigured  float64           `json:"sl_pct_configured,omitempty"`
	TPPriceSubmitted float64           `json:"tp_price_submitted,omitempty"`
	SLPriceSubmitted float64           `json:"sl_price_submitted,omitempty"`
	Excursion        ExcursionSnapshot `json:"excursion,omitempty"`
}

// ExitSnapshot captures how and when the position was closed.
type ExitSnapshot struct {
	Reason              string  `json:"reason,omitempty"` // "trailing", "tp", "sl", "timeout", "trailing_failed_fallback"
	HoldDurationMs      int64   `json:"hold_duration_ms,omitempty"`
	TPPctConfigured     float64 `json:"tp_pct_configured,omitempty"`
	SLPctConfigured     float64 `json:"sl_pct_configured,omitempty"`
	TPPriceSubmitted    float64 `json:"tp_price_submitted,omitempty"`
	SLPriceSubmitted    float64 `json:"sl_price_submitted,omitempty"`
	TrailingActivated   bool    `json:"trailing_activated"`
	TrailingActivePrice float64 `json:"trailing_active_price,omitempty"`
	TrailingCallbackPct float64 `json:"trailing_callback_pct,omitempty"`

	// Dynamic vs Static comparison (when dynamic pricing enabled)
	DynamicPricingEnabled bool    `json:"dynamic_pricing_enabled"`
	DynamicTPPct          float64 `json:"dynamic_tp_pct,omitempty"`
	StaticTPPct           float64 `json:"static_tp_pct,omitempty"`
	DynamicSLPct          float64 `json:"dynamic_sl_pct,omitempty"`
	StaticSLPct           float64 `json:"static_sl_pct,omitempty"`
	ATRValue              float64 `json:"atr_value,omitempty"`
}

// ExcursionSnapshot captures MFE/MAE price tracking data.
// This is the single most valuable data for tuning TP/SL parameters.
type ExcursionSnapshot struct {
	// MFE (Maximum Favorable Excursion) — best price reached in our favor.
	MFEPrice float64   `json:"mfe_price,omitempty"`
	MFEPct   float64   `json:"mfe_pct,omitempty"`
	MFETime  time.Time `json:"mfe_time,omitempty"`

	// MAE (Maximum Adverse Excursion) — worst price reached against us.
	MAEPrice float64   `json:"mae_price,omitempty"`
	MAEPct   float64   `json:"mae_pct,omitempty"`
	MAETime  time.Time `json:"mae_time,omitempty"`

	// Comparison with configured TP/SL
	MFEvsTP float64 `json:"mfe_vs_tp,omitempty"` // Positive = TP conservative, Negative = TP ambitious
	MAEvsSL float64 `json:"mae_vs_sl,omitempty"` // Positive = SL too tight, Negative = SL safe

	// Hindsight "ideal" values
	IdealTPPct   float64 `json:"ideal_tp_pct,omitempty"`   // mfe_pct * 0.8
	IdealSLPct   float64 `json:"ideal_sl_pct,omitempty"`   // mae_pct * 1.2
	TPEfficiency float64 `json:"tp_efficiency,omitempty"`  // 0-1: how much of MFE we captured
	SLWasTouched bool    `json:"sl_was_touched,omitempty"` // MAE >= configured SL
}

// TimelineEntry records a single event in the cycle's event bus.
type TimelineEntry struct {
	Time    time.Time       `json:"time"`
	Topic   string          `json:"topic"`
	MsgID   string          `json:"msg_id"`
	Payload json.RawMessage `json:"payload"`
}

// MarketSnapshot captures market state at a specific phase.
type MarketSnapshot struct {
	Phase     Phase   `json:"phase"` // "scan", "arm", "fire"
	LastPrice float64 `json:"last_price"`
	BestBid   float64 `json:"best_bid"`
	BestAsk   float64 `json:"best_ask"`
	Spread    float64 `json:"spread"`
}
