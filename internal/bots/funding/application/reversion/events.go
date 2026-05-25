package reversion

import (
	"time"

	fundingdomain "crypto-bot/internal/bots/funding/domain"
	shared "crypto-bot/internal/domain"
)

// Flow names.
const (
	FlowReversion = "reversion"
)

// Reversion event topics.
const (
	TopicReversionCandidate           = "funding.reversion.candidate"
	TopicReversionArmed               = "funding.reversion.armed"
	TopicReversionWaitComplete        = "funding.reversion.wait_complete"
	TopicReversionConfirmed           = "funding.reversion.confirmed"
	TopicReversionIOCFired            = "funding.reversion.ioc_fired"
	TopicReversionOrderFilled         = "funding.reversion.order_filled"
	TopicReversionPositionClosed      = "funding.reversion.position_closed"
	TopicReversionTimeout             = "funding.reversion.timeout"
	TopicReversionAbort               = "funding.reversion.abort"
	TopicReversionError               = "funding.reversion.error"
	TopicReversionFinalPnL            = "funding.reversion.final_pnl"
	TopicReversionCompleted           = "funding.reversion.completed"
	TopicReversionCheckTimeout        = "funding.reversion.check_timeout"
	TopicReversionForceCloseInitiated = "funding.reversion.force_close_initiated"
)

// Map keys for goconst lint compliance.
const (
	keySymbol      = "symbol"
	keyFundingRate = "fundingRate"
	keyVolume      = "volume"
	keyOrderID     = "orderId"
	keyError       = "error"
	keyClosePrice  = "closePrice"
	keyReason      = "reason"
	keyEntryPrice  = "entryPrice"
	keyFee         = "fee"
	keyHoldFee     = "holdFee"
	keyTimeout     = "timeout"
)

// ReversionEvent defines the interface that all reversion lifecycle events must implement.
type ReversionEvent interface {
	GetFlow() string
	GetReqID() string
	GetSymbol() string
	GetMessage() string
	GetDataMap() map[string]interface{}
	ShouldNotify() bool
}

// BaseReversionEvent provides shared boilerplate fields and methods for all reversion events.
type BaseReversionEvent struct {
	Flow       string    `json:"flow,omitempty"`
	ReqID      string    `json:"req_id,omitempty"`
	Symbol     string    `json:"symbol"`
	SendNotify bool      `json:"send_notify,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
}

func (b BaseReversionEvent) GetFlow() string    { return b.Flow }
func (b BaseReversionEvent) GetReqID() string   { return b.ReqID }
func (b BaseReversionEvent) GetSymbol() string  { return b.Symbol }
func (b BaseReversionEvent) ShouldNotify() bool { return b.SendNotify }

// CandidateFoundEvent is the starting event containing the parsed candidate.
type CandidateFoundEvent struct {
	BaseReversionEvent
	Candidate  fundingdomain.Candidate `json:"candidate"`
	SettleTime time.Time               `json:"settle_time"`
}

func (e CandidateFoundEvent) GetMessage() string {
	return "Candidate found for " + e.Symbol
}
func (e CandidateFoundEvent) GetDataMap() map[string]interface{} {
	return map[string]interface{}{
		keySymbol:      e.Symbol,
		keyFundingRate: e.Candidate.FundingRate,
		"side":         e.Candidate.Side.String(),
	}
}

// ArmedEvent is published when WS sub and IOC price/vol check completes.
type ArmedEvent struct {
	BaseReversionEvent
	Candidate  fundingdomain.Candidate `json:"candidate"`
	Volume     float64                 `json:"volume"`
	IOCPrice   float64                 `json:"ioc_price"`
	Slippage   float64                 `json:"slippage"`
	SettleTime time.Time               `json:"settle_time"`
}

func (e ArmedEvent) GetMessage() string {
	return "Reversion armed for " + e.Symbol
}
func (e ArmedEvent) GetDataMap() map[string]interface{} {
	return map[string]interface{}{
		keySymbol:  e.Symbol,
		keyVolume:  e.Volume,
		"iocPrice": e.IOCPrice,
		"slippage": e.Slippage,
	}
}

// WaitCompleteEvent signals that the pre-settle wait period has completed.
type WaitCompleteEvent struct {
	BaseReversionEvent
	SettleTime time.Time               `json:"settle_time"`
	Candidate  fundingdomain.Candidate `json:"candidate"`
}

func (e WaitCompleteEvent) GetMessage() string {
	return "Wait complete for " + e.Symbol
}
func (e WaitCompleteEvent) GetDataMap() map[string]interface{} {
	return map[string]interface{}{
		keySymbol:    e.Symbol,
		"settleTime": e.SettleTime,
	}
}

// ConfirmedEvent is published after funding rate and recheck passes.
type ConfirmedEvent struct {
	BaseReversionEvent
	FundingRate float64                 `json:"funding_rate"`
	Candidate   fundingdomain.Candidate `json:"candidate"`
	SettleTime  time.Time               `json:"settle_time"`
}

func (e ConfirmedEvent) GetMessage() string {
	return "Recheck confirmed for " + e.Symbol
}
func (e ConfirmedEvent) GetDataMap() map[string]interface{} {
	return map[string]interface{}{
		keySymbol:      e.Symbol,
		keyFundingRate: e.FundingRate,
	}
}

// IOCFiredEvent is published after the IOC order is submitted to exchange.
type IOCFiredEvent struct {
	BaseReversionEvent
	OrderID       string      `json:"order_id"`
	Side          shared.Side `json:"side"`
	CloseSide     shared.Side `json:"close_side"`
	OrderType     int         `json:"order_type"`
	IntendedPrice float64     `json:"intended_price"`
	Volume        float64     `json:"volume"`
	TPPrice       float64     `json:"tp_price,omitempty"`
	SLPrice       float64     `json:"sl_price,omitempty"`
	SettleTime    time.Time   `json:"settle_time"`
	FireTimestamp time.Time   `json:"fire_timestamp"`
	LatencyRTTMs  int64       `json:"latency_rtt_ms,omitempty"`
	Error         string      `json:"error,omitempty"`
}

func (e IOCFiredEvent) GetMessage() string {
	if e.Error != "" {
		return "IOC Order FAILED to fire for " + e.Symbol + ": " + e.Error
	}
	return "IOC Order fired for " + e.Symbol
}
func (e IOCFiredEvent) GetDataMap() map[string]interface{} {
	return map[string]interface{}{
		keySymbol:       e.Symbol,
		keyOrderID:      e.OrderID,
		"intendedPrice": e.IntendedPrice,
		keyVolume:       e.Volume,
		keyError:        e.Error,
	}
}
func (e IOCFiredEvent) ShouldNotify() bool { return e.SendNotify || e.Error != "" }

// OrderFilledEvent is recorded when watcher detects a position fill.
type OrderFilledEvent struct {
	BaseReversionEvent
	OrderID     string      `json:"order_id"`
	Side        shared.Side `json:"side"`
	CloseSide   shared.Side `json:"close_side"`
	FillPrice   float64     `json:"fill_price"`
	FillVol     float64     `json:"fill_vol"`
	Fee         float64     `json:"fee,omitempty"`
	Profit      float64     `json:"profit,omitempty"`
	HoldFee     float64     `json:"hold_fee,omitempty"`
	SlippagePct float64     `json:"slippage_pct,omitempty"`
	TPPrice     float64     `json:"tp_price,omitempty"`
	SLPrice     float64     `json:"sl_price,omitempty"`
}

func (e OrderFilledEvent) GetMessage() string {
	return "Position FILLED for " + e.Symbol
}
func (e OrderFilledEvent) GetDataMap() map[string]interface{} {
	return map[string]interface{}{
		keySymbol:   e.Symbol,
		keyOrderID:  e.OrderID,
		"fillPrice": e.FillPrice,
		"fillVol":   e.FillVol,
	}
}

// PositionClosedEvent is published when watcher detects a flat position.
type PositionClosedEvent struct {
	BaseReversionEvent
	EntryPrice      float64     `json:"entry_price"`
	ClosePrice      float64     `json:"close_price"`
	CloseVol        float64     `json:"close_vol"`
	Reason          string      `json:"reason"`
	GrossProfit     float64     `json:"gross_profit"`
	NetProfit       float64     `json:"net_profit"`
	Fee             float64     `json:"fee"`
	HoldFee         float64     `json:"hold_fee"`
	HoldDurationMs  int64       `json:"hold_duration_ms"`
	TPPriceTouched  bool        `json:"tp_price_touched,omitempty"`
	SLPriceTouched  bool        `json:"sl_price_touched,omitempty"`
	Direction       shared.Side `json:"direction,omitempty"`
	Method          string      `json:"method,omitempty"`
	CloseRetryCount int         `json:"close_retry_count,omitempty"`
}

func (e PositionClosedEvent) GetMessage() string {
	return "Position CLOSED for " + e.Symbol + " (" + e.Reason + ")"
}
func (e PositionClosedEvent) GetDataMap() map[string]interface{} {
	return map[string]interface{}{
		keySymbol:     e.Symbol,
		keyEntryPrice: e.EntryPrice,
		keyClosePrice: e.ClosePrice,
		"closeVol":    e.CloseVol,
		keyReason:     e.Reason,
		"netProfit":   e.NetProfit,
		keyFee:        e.Fee,
		keyHoldFee:    e.HoldFee,
	}
}

// TimeoutEvent is published when safety timeout guard triggers.
type TimeoutEvent struct {
	BaseReversionEvent
	Timeout             time.Duration `json:"timeout"`
	Reason              string        `json:"reason"`
	ForceCloseAttempted bool          `json:"force_close_attempted,omitempty"`
	ForceCloseSucceeded bool          `json:"force_close_succeeded,omitempty"`
	CloseRetryCount     int           `json:"close_retry_count,omitempty"`
	Error               string        `json:"error,omitempty"`
}

func (e TimeoutEvent) GetMessage() string {
	if e.Error != "" {
		return "Timeout Guard TRIGGERED for " + e.Symbol + " but failed to close: " + e.Error
	}
	return "Timeout Guard TRIGGERED for " + e.Symbol
}
func (e TimeoutEvent) GetDataMap() map[string]interface{} {
	return map[string]interface{}{
		keySymbol:             e.Symbol,
		keyTimeout:            e.Timeout.String(),
		keyReason:             e.Reason,
		"forceCloseSucceeded": e.ForceCloseSucceeded,
	}
}
func (e TimeoutEvent) ShouldNotify() bool { return e.SendNotify || e.Error != "" }

// AbortEvent is published when a cycle aborts gracefully or due to check fail.
type AbortEvent struct {
	BaseReversionEvent
	Reason string `json:"reason"`
}

func (e AbortEvent) GetMessage() string {
	return "Cycle aborted for " + e.Symbol + ": " + e.Reason
}
func (e AbortEvent) GetDataMap() map[string]interface{} {
	return map[string]interface{}{
		keySymbol: e.Symbol,
		keyReason: e.Reason,
	}
}

// ErrorEvent signals an unexpected execution error.
type ErrorEvent struct {
	BaseReversionEvent
	Error string `json:"error"`
}

func (e ErrorEvent) GetMessage() string {
	return "Cycle error for " + e.Symbol + ": " + e.Error
}
func (e ErrorEvent) GetDataMap() map[string]interface{} {
	return map[string]interface{}{
		keySymbol: e.Symbol,
		keyError:  e.Error,
	}
}
func (e ErrorEvent) ShouldNotify() bool { return true }

// FinalPnLEvent is published at cycle end with the final reversion PnL summary.
type FinalPnLEvent struct {
	BaseReversionEvent
	Direction      shared.Side `json:"direction"`
	EntryPrice     float64     `json:"entry_price"`
	ClosePrice     float64     `json:"close_price"`
	MaxVol         float64     `json:"max_vol"`
	GrossPnL       float64     `json:"gross_pnl"`
	NetPnL         float64     `json:"net_pnl"`
	Fees           float64     `json:"fees"`
	HoldFee        float64     `json:"hold_fees"`
	HoldDurationMs int64       `json:"hold_duration_ms"`
}

func (e FinalPnLEvent) GetMessage() string {
	return "Final PnL for " + e.Symbol + " of Flow " + e.Flow
}
func (e FinalPnLEvent) GetDataMap() map[string]interface{} {
	return map[string]interface{}{
		keySymbol:     e.Symbol,
		keyEntryPrice: e.EntryPrice,
		keyClosePrice: e.ClosePrice,
		"netPnL":      e.NetPnL,
		keyFee:        e.Fees,
		keyHoldFee:    e.HoldFee,
	}
}

// ReversionCompletedEvent is published when the reversion flow finishes.
type ReversionCompletedEvent struct {
	BaseReversionEvent
	Reason string `json:"reason"`
}

func (e ReversionCompletedEvent) GetMessage() string {
	return "Reversion completed for " + e.Symbol
}
func (e ReversionCompletedEvent) GetDataMap() map[string]interface{} {
	return map[string]interface{}{
		keySymbol: e.Symbol,
		keyReason: e.Reason,
	}
}

// CheckTimeoutEvent is published to trigger the asynchronous timeout guard.
type CheckTimeoutEvent struct {
	BaseReversionEvent
	IOCEvent IOCFiredEvent `json:"ioc_event"`
}

func (e CheckTimeoutEvent) GetMessage() string {
	return "Timeout check initiated for " + e.Symbol
}
func (e CheckTimeoutEvent) GetDataMap() map[string]interface{} {
	return map[string]interface{}{
		keySymbol: e.Symbol,
	}
}

// ForceCloseInitiatedEvent is published when fallback close begins.
type ForceCloseInitiatedEvent struct {
	BaseReversionEvent
	HoldVol    float64 `json:"hold_vol"`
	TimeoutSec float64 `json:"timeout_sec"`
}

func (e ForceCloseInitiatedEvent) GetMessage() string {
	return "CRITICAL: Initiating safety timeout force close for " + e.Symbol
}
func (e ForceCloseInitiatedEvent) GetDataMap() map[string]interface{} {
	return map[string]interface{}{
		keySymbol:  e.Symbol,
		"holdVol":  e.HoldVol,
		keyTimeout: e.TimeoutSec,
	}
}
