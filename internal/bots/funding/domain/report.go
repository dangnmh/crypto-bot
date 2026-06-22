package domain

import (
	"context"
	"time"

	shared "crypto-bot/internal/domain"
)

// TradeReport is the domain entity representing a finalized trade reversion cycle.
type TradeReport struct {
	ReqID              string      `json:"req_id"` // client-side externalOrderId (Cycle ID)
	EventID            string      `json:"event_id"`
	Timestamp          time.Time   `json:"timestamp"`
	SettleTime         time.Time   `json:"settle_time"`
	Exchange           string      `json:"exchange"`
	Symbol             string      `json:"symbol"`
	NormalizedSymbol   string      `json:"normalized_symbol"`
	Side               shared.Side `json:"side"`
	FundingRate        float64     `json:"funding_rate"`
	CandidateFoundTime time.Time   `json:"candidate_found_time"`

	// Config settings
	MarginUSDT   float64 `json:"margin_usdt"`
	Leverage     int     `json:"leverage"`
	BufferTimeMs int64   `json:"buffer_time_ms"`

	// Execution & Latency Fields
	LatencyRTTMs   int64   `json:"latency_rtt_ms"`
	ActualSlippage float64 `json:"actual_slippage"`
	FireOffsetMs   int64   `json:"fire_offset_ms"`

	// Order Tracking Fields
	IOCOrderID       string    `json:"ioc_order_id"`
	IOCOutcome       string    `json:"ioc_outcome"`
	IOCReason        string    `json:"ioc_reason"`
	FireIOCTime      time.Time `json:"fire_ioc_time"`
	LocalFireIOCTime time.Time `json:"local_fire_ioc_time"`

	// Position & Financial Performance Fields
	OrderFilled    bool    `json:"order_filled"`
	FillPrice      float64 `json:"fill_price"`
	ClosePrice     float64 `json:"close_price"`
	VolumeUSDT     float64 `json:"volume_usdt"`
	GrossProfit    float64 `json:"gross_profit"`
	NetProfit      float64 `json:"net_profit"`
	PnLPct         float64 `json:"pnl_pct"`
	Fee            float64 `json:"fee"`
	HoldFee        float64 `json:"hold_fee"`
	HoldDurationMs int64   `json:"hold_duration_ms"`
	ExitReason     string  `json:"exit_reason"`

	// Risk & Termination Status Fields
	CloseRetryCount     int    `json:"close_retry_count"`
	ForceCloseAttempted bool   `json:"force_close_attempted"`
	ForceCloseSucceeded bool   `json:"force_close_succeeded"`
	Status              string `json:"status"` // "completed", "aborted", "error"
	ErrorMsg            string `json:"error_msg"`
}

// TradeReportRepository defines the contract for persisting TradeReports.
type TradeReportRepository interface {
	Save(ctx context.Context, report *TradeReport) error
}
