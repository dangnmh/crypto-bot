package reversion

import (
	"fmt"
	"math"
	"strings"
	"time"

	fundingdomain "crypto-bot/internal/bots/funding/domain"
	shared "crypto-bot/internal/domain"
	"crypto-bot/pkg/formatutil"
)

const (
	FlowReversion = "reversion"

	StatusCompleted = "completed"
	StatusAborted   = "aborted"
	StatusError     = "error"
)

const (
	TopicReversionCandidate              = "funding.reversion.candidate"
	TopicReversionArmMarketReady         = "funding.reversion.arm_market_ready"
	TopicReversionArmPlanCalculated      = "funding.reversion.arm_plan_calculated"
	TopicReversionSafetyChecked          = "funding.reversion.safety_checked"
	TopicReversionArmed                  = "funding.reversion.armed"
	TopicReversionWaitComplete           = "funding.reversion.wait_complete"
	TopicReversionConfirmed              = "funding.reversion.confirmed"
	TopicReversionMarginModeReady        = "funding.reversion.margin_mode_ready"
	TopicReversionFireTimingReady        = "funding.reversion.fire_timing_ready"
	TopicReversionFirePlanChecked        = "funding.reversion.fire_plan_checked"
	TopicReversionFireWindowReached      = "funding.reversion.fire_window_reached"
	TopicReversionPositionWatchReady     = "funding.reversion.position_watch_ready"
	TopicReversionIOCSubmitted           = "funding.reversion.ioc_submitted"
	TopicReversionIOCOutcomeChecked      = "funding.reversion.ioc_outcome_checked"
	TopicReversionOrderFilled            = "funding.reversion.order_filled"
	TopicReversionPositionClosed         = "funding.reversion.position_closed"
	TopicReversionTimeoutGuardScheduled  = "funding.reversion.timeout_guard_scheduled"
	TopicReversionTimeoutPositionChecked = "funding.reversion.timeout_position_checked"
	TopicReversionForceCloseInitiated    = "funding.reversion.force_close_initiated"
	TopicReversionForceCloseCompleted    = "funding.reversion.force_close_completed"
	TopicReversionTimeout                = "funding.reversion.timeout"
	TopicReversionAbort                  = "funding.reversion.abort"
	TopicReversionError                  = "funding.reversion.error"
	TopicReversionFinalPnL               = "funding.reversion.final_pnl"
	TopicReversionCompleted              = "funding.reversion.completed"
	TopicReversionTPSLRequired           = "funding.reversion.tpsl_required"
	TopicReversionTradeReport            = "funding.reversion.trade_report"
)

type IOCOutcome string

const (
	IOCOutcomeFilled         IOCOutcome = "filled"
	IOCOutcomePartialFilled  IOCOutcome = "partial_filled"
	IOCOutcomeCanceledNoFill IOCOutcome = "canceled_no_fill"
	IOCOutcomeUnknown        IOCOutcome = "unknown"
)

type ReversionReason string

const (
	reversionReasonIOCCanceledNoPosition ReversionReason = "ioc_canceled_no_position"
	reversionReasonIOCUnknownNoPosition  ReversionReason = "ioc_outcome_unknown_no_position"
)

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
	keyPassed      = "passed"
	keyBestBid     = "bestBid"
	keyBestAsk     = "bestAsk"
	keyLastPrice   = "lastPrice"
	keyIOCPrice    = "iocPrice"
	keySlippage    = "slippage"
	keyLatencyRTT  = "latencyRTTMs"
	keyHoldVol     = "holdVol"
	keyOutcome     = "outcome"
	keyVolumeUSDT  = "volumeUSDT"
	keyVolUSDT24h  = "volusdt24h"
)

type EventColor string

const (
	ColorYellow EventColor = "yellow"
	ColorGreen  EventColor = "green"
	ColorRed    EventColor = "red"
	ColorBlue   EventColor = "blue"
)

type ReversionEvent interface {
	GetFlow() string
	GetReqID() string
	GetSymbol() string
	GetExchange() string
	GetMessage() string
	GetDataMap() map[string]any
	ShouldNotify() bool
	GetColor() EventColor
}

type BaseReversionEvent struct {
	Flow          string      `json:"flow,omitempty"`
	ReqID         string      `json:"req_id,omitempty"`
	Symbol        string      `json:"symbol"`
	Exchange      string      `json:"exchange,omitempty"`
	SendNotify    bool        `json:"send_notify,omitempty"`
	Color         EventColor  `json:"color,omitempty"`
	OrderID       string      `json:"order_id,omitempty"`
	ExternalID    string      `json:"external_id,omitempty"`
	Timestamp     time.Time   `json:"timestamp"`
	EventID       string      `json:"event_id,omitempty"`
	Seq           int64       `json:"seq,omitempty"`
	Topic         string      `json:"topic,omitempty"`
	PreviousTopic string      `json:"previous_topic,omitempty"`
	SettleTime    time.Time   `json:"settle_time"`
	Side          shared.Side `json:"side,omitempty"`
	FundingRate   float64     `json:"funding_rate,omitempty"`
}

func (b BaseReversionEvent) GetFlow() string     { return b.Flow }
func (b BaseReversionEvent) GetReqID() string    { return b.ReqID }
func (b BaseReversionEvent) GetSymbol() string   { return b.Symbol }
func (b BaseReversionEvent) GetExchange() string { return b.Exchange }
func (b BaseReversionEvent) ShouldNotify() bool  { return b.SendNotify }
func (b BaseReversionEvent) GetColor() EventColor {
	if b.Color != "" {
		return b.Color
	}
	return ColorYellow
}

func (b BaseReversionEvent) DeduplicateKey() string {
	if b.ExternalID == "" || b.Topic == "" {
		return ""
	}
	return b.ExternalID + b.Topic
}

type CandidateFoundEvent struct {
	BaseReversionEvent
	Candidate fundingdomain.Candidate `json:"candidate"`
}

func (e CandidateFoundEvent) GetMessage() string { return "Candidate found" }
func (e CandidateFoundEvent) GetDataMap() map[string]any {
	return map[string]any{keyFundingRate: e.Candidate.FundingRate, "side": e.Candidate.Side.String()}
}

type ArmMarketReadyEvent struct {
	BaseReversionEvent
	Candidate fundingdomain.Candidate `json:"candidate"`
	MaxWaitMs int64                   `json:"max_wait_ms"`
}

func (e ArmMarketReadyEvent) GetMessage() string { return "Arm market ready" }
func (e ArmMarketReadyEvent) GetDataMap() map[string]any {
	return map[string]any{keyBestBid: e.Candidate.BestBid, keyBestAsk: e.Candidate.BestAsk, keyLastPrice: e.Candidate.LastPrice}
}

type ArmPlanCalculatedEvent struct {
	BaseReversionEvent
	Candidate fundingdomain.Candidate `json:"candidate"`
	IOCPrice  float64                 `json:"ioc_price"`
	RefPrice  float64                 `json:"ref_price"`
}

func (e ArmPlanCalculatedEvent) GetMessage() string { return "Arm plan calculated" }
func (e ArmPlanCalculatedEvent) GetDataMap() map[string]any {
	return map[string]any{keyIOCPrice: e.IOCPrice, keySlippage: e.Candidate.Slippage, keyVolume: e.Candidate.Volume}
}

type SafetyCheckedEvent struct {
	BaseReversionEvent
	Candidate      fundingdomain.Candidate `json:"candidate"`
	IOCPrice       float64                 `json:"ioc_price"`
	RefPrice       float64                 `json:"ref_price"`
	AdjustedVolume float64                 `json:"adjusted_volume"`
	Passed         bool                    `json:"passed"`
	RejectReason   string                  `json:"reject_reason,omitempty"`
}

func (e SafetyCheckedEvent) GetMessage() string {
	if !e.Passed {
		return "Safety rejected: " + e.RejectReason
	}
	return "Safety checked"
}
func (e SafetyCheckedEvent) GetDataMap() map[string]any {
	m := map[string]any{keyPassed: e.Passed, keyVolume: e.AdjustedVolume}
	if e.RejectReason != "" {
		m[keyReason] = e.RejectReason
	}
	return m
}

type ArmedEvent struct {
	BaseReversionEvent
	Candidate fundingdomain.Candidate `json:"candidate"`
}

func (e ArmedEvent) GetMessage() string { return "Armed" }
func (e ArmedEvent) GetDataMap() map[string]any {
	return map[string]any{keyVolume: e.Candidate.Volume, keyIOCPrice: e.Candidate.LastPrice, keySlippage: e.Candidate.Slippage}
}

type WaitCompleteEvent struct {
	BaseReversionEvent
	Candidate fundingdomain.Candidate `json:"candidate"`
}

func (e WaitCompleteEvent) GetMessage() string { return "Wait complete" }
func (e WaitCompleteEvent) GetDataMap() map[string]any {
	return map[string]any{"settleTime": e.SettleTime}
}

type ConfirmedEvent struct {
	BaseReversionEvent
	Candidate fundingdomain.Candidate `json:"candidate"`
}

func (e ConfirmedEvent) GetMessage() string { return "Recheck confirmed" }
func (e ConfirmedEvent) GetDataMap() map[string]any {
	return map[string]any{keyFundingRate: e.FundingRate}
}

type MarginModeReadyEvent struct {
	BaseReversionEvent
	Candidate fundingdomain.Candidate `json:"candidate"`
}

func (e MarginModeReadyEvent) GetMessage() string { return "Margin mode ready" }
func (e MarginModeReadyEvent) GetDataMap() map[string]any {
	return map[string]any{keyFundingRate: e.FundingRate}
}

type FireTimingReadyEvent struct {
	BaseReversionEvent
	Candidate        fundingdomain.Candidate `json:"candidate"`
	LatencyRTTMs     int64                   `json:"latency_rtt_ms"`
	FireOffsetMs     int64                   `json:"fire_offset_ms"`
	SnapshotOffsetMs int64                   `json:"snapshot_offset_ms"`
}

func (e FireTimingReadyEvent) GetMessage() string { return "Fire timing ready" }
func (e FireTimingReadyEvent) GetDataMap() map[string]any {
	return map[string]any{keyLatencyRTT: e.LatencyRTTMs, "fireOffsetMs": e.FireOffsetMs}
}

type FirePlanCheckedEvent struct {
	BaseReversionEvent
	Candidate      fundingdomain.Candidate `json:"candidate"`
	LatencyRTTMs   int64                   `json:"latency_rtt_ms"`
	FireOffsetMs   int64                   `json:"fire_offset_ms"`
	IOCPrice       float64                 `json:"ioc_price"`
	RefPrice       float64                 `json:"ref_price"`
	AdjustedVolume float64                 `json:"adjusted_volume"`
	Passed         bool                    `json:"passed"`
	RejectReason   string                  `json:"reject_reason,omitempty"`
}

func (e FirePlanCheckedEvent) GetMessage() string {
	if !e.Passed {
		return "Fire plan rejected: " + e.RejectReason
	}
	return "Fire plan checked"
}
func (e FirePlanCheckedEvent) GetDataMap() map[string]any {
	m := map[string]any{keyPassed: e.Passed, keyVolume: e.AdjustedVolume}
	if e.RejectReason != "" {
		m[keyReason] = e.RejectReason
	}
	return m
}

type FireWindowReachedEvent struct {
	BaseReversionEvent
	Candidate     fundingdomain.Candidate `json:"candidate"`
	LatencyRTTMs  int64                   `json:"latency_rtt_ms"`
	FireTimestamp time.Time               `json:"fire_timestamp"`
}

func (e FireWindowReachedEvent) GetMessage() string { return "Fire window reached" }
func (e FireWindowReachedEvent) GetDataMap() map[string]any {
	return map[string]any{keyLatencyRTT: e.LatencyRTTMs}
}

type PositionWatchReadyEvent struct {
	BaseReversionEvent
	Candidate     fundingdomain.Candidate `json:"candidate"`
	LatencyRTTMs  int64                   `json:"latency_rtt_ms"`
	FireTimestamp time.Time               `json:"fire_timestamp"`
	Timeout       time.Duration           `json:"timeout"`
}

func (e PositionWatchReadyEvent) GetMessage() string { return "Position watch ready" }
func (e PositionWatchReadyEvent) GetDataMap() map[string]any {
	return map[string]any{keyTimeout: e.Timeout.String()}
}

type IOCSubmittedEvent struct {
	BaseReversionEvent
	Candidate        fundingdomain.Candidate `json:"candidate"`
	IntendedPrice    float64                 `json:"intended_price"`
	TPPrice          float64                 `json:"tp_price,omitempty"`
	SLPrice          float64                 `json:"sl_price,omitempty"`
	TPSLSubmitted    bool                    `json:"tpsl_submitted"`
	FireTimestamp    time.Time               `json:"fire_timestamp"`
	FireIOCTime      time.Time               `json:"fire_ioc_time"`
	LocalFireIOCTime time.Time               `json:"local_fire_ioc_time"`
	LatencyRTTMs     int64                   `json:"latency_rtt_ms,omitempty"`
	Error            string                  `json:"error,omitempty"`
}

func (e IOCSubmittedEvent) GetMessage() string {
	if e.Error != "" {
		return "IOC order failed: " + e.Error
	}
	return "IOC order submitted"
}
func (e IOCSubmittedEvent) GetDataMap() map[string]any {
	m := map[string]any{
		keyIOCPrice:    e.IntendedPrice,
		keyVolume:      e.Candidate.Volume,
		keyFundingRate: e.Candidate.FundingRate,
		keyVolUSDT24h:  e.Candidate.AmountUSDT24,
	}
	if e.OrderID != "" {
		m[keyOrderID] = e.OrderID
	}
	if e.ExternalID != "" {
		m["externalId"] = e.ExternalID
	}
	if e.Error != "" {
		m[keyError] = e.Error
	}
	return m
}
func (e IOCSubmittedEvent) ShouldNotify() bool { return e.SendNotify || e.Error != "" }

type IOCOutcomeCheckedEvent struct {
	BaseReversionEvent
	OrderState   shared.OrderState `json:"order_state"`
	DealVol      float64           `json:"deal_vol"`
	DealAvgPrice float64           `json:"deal_avg_price"`
	HoldVol      float64           `json:"hold_vol"`
	Outcome      IOCOutcome        `json:"outcome"`
	Reason       ReversionReason   `json:"reason"`
	CheckedAt    time.Time         `json:"checked_at"`
	Timeout      time.Duration     `json:"timeout"`
	VolUSDT24h   float64           `json:"vol_usdt_24h"`
}

func (e IOCOutcomeCheckedEvent) GetMessage() string {
	switch e.Outcome {
	case IOCOutcomeCanceledNoFill:
		return "IOC order canceled (no fill)"
	case IOCOutcomeUnknown:
		return "IOC outcome unknown: " + string(e.Reason)
	default:
		return "IOC outcome checked"
	}
}

func (e IOCOutcomeCheckedEvent) GetDataMap() map[string]any {
	m := map[string]any{
		keyOutcome:     string(e.Outcome),
		keyHoldVol:     e.HoldVol,
		keyFundingRate: e.FundingRate,
		keyVolUSDT24h:  e.VolUSDT24h,
	}
	if e.OrderID != "" {
		m[keyOrderID] = e.OrderID
	}
	if e.Reason != "" {
		m[keyReason] = string(e.Reason)
	}
	return m
}

func (e IOCOutcomeCheckedEvent) ShouldNotify() bool {
	return e.SendNotify || e.Outcome == IOCOutcomeCanceledNoFill || e.Outcome == IOCOutcomeUnknown
}

func (e IOCOutcomeCheckedEvent) GetColor() EventColor {
	if e.Outcome == IOCOutcomeCanceledNoFill {
		return ColorBlue
	}
	if e.Outcome == IOCOutcomeUnknown {
		if e.Reason == reversionReasonIOCUnknownNoPosition {
			return ColorBlue
		}
		return ColorRed
	}
	if e.Color != "" {
		return e.Color
	}
	return ColorYellow
}

type OrderFilledEvent struct {
	BaseReversionEvent
	Side        shared.Side `json:"side"`
	CloseSide   shared.Side `json:"close_side"`
	FillPrice   float64     `json:"fill_price"`
	FillVol     float64     `json:"fill_vol"`
	VolumeUSDT  float64     `json:"volume_usdt"`
	Fee         float64     `json:"fee,omitempty"`
	Profit      float64     `json:"profit,omitempty"`
	HoldFee     float64     `json:"hold_fee,omitempty"`
	SlippagePct float64     `json:"slippage_pct,omitempty"`
	TPPrice     float64     `json:"tp_price,omitempty"`
	SLPrice     float64     `json:"sl_price,omitempty"`
}

func (e OrderFilledEvent) GetMessage() string { return "Position filled" }
func (e OrderFilledEvent) GetDataMap() map[string]any {
	m := map[string]any{
		"fillPrice":   e.FillPrice,
		"fillVol":     e.FillVol,
		keyVolumeUSDT: e.VolumeUSDT,
	}
	if e.OrderID != "" {
		m[keyOrderID] = e.OrderID
	}
	return m
}

type PositionClosedEvent struct {
	BaseReversionEvent
	EntryPrice      float64 `json:"entry_price"`
	ClosePrice      float64 `json:"close_price"`
	CloseVol        float64 `json:"close_vol"`
	Reason          string  `json:"reason"`
	GrossProfit     float64 `json:"gross_profit"`
	NetProfit       float64 `json:"net_profit"`
	PnLPct          float64 `json:"pnl_pct"`
	VolumeUSDT      float64 `json:"volume_usdt"`
	Fee             float64 `json:"fee"`
	HoldFee         float64 `json:"hold_fee"`
	HoldDurationMs  int64   `json:"hold_duration_ms"`
	TPPriceTouched  bool    `json:"tp_price_touched,omitempty"`
	SLPriceTouched  bool    `json:"sl_price_touched,omitempty"`
	Method          string  `json:"method,omitempty"`
	CloseRetryCount int     `json:"close_retry_count,omitempty"`
}

func (e PositionClosedEvent) GetMessage() string {
	return "Position closed: " + e.Reason
}
func (e PositionClosedEvent) GetDataMap() map[string]any {
	return map[string]any{
		keyEntryPrice:    e.EntryPrice,
		keyClosePrice:    e.ClosePrice,
		"closeVol":       e.CloseVol,
		keyReason:        e.Reason,
		"netProfit":      e.NetProfit,
		keyFee:           e.Fee,
		keyHoldFee:       e.HoldFee,
		keyVolumeUSDT:    e.VolumeUSDT,
		"holdDurationMs": e.HoldDurationMs,
	}
}
func (e PositionClosedEvent) GetColor() EventColor {
	if e.NetProfit > 0 {
		return ColorGreen
	}
	if e.NetProfit < 0 {
		return ColorRed
	}
	return ColorYellow
}
func (e PositionClosedEvent) ShouldNotify() bool { return false }

type TimeoutGuardScheduledEvent struct {
	BaseReversionEvent
	Timeout   time.Duration `json:"timeout"`
	StartedAt time.Time     `json:"started_at"`
}

func (e TimeoutGuardScheduledEvent) GetMessage() string {
	return "Timeout guard scheduled"
}
func (e TimeoutGuardScheduledEvent) GetDataMap() map[string]any {
	return map[string]any{keyTimeout: e.Timeout.String()}
}

type TimeoutPositionCheckedEvent struct {
	BaseReversionEvent
	Timeout   time.Duration `json:"timeout"`
	StartedAt time.Time     `json:"started_at"`
	HoldVol   float64       `json:"hold_vol"`
	Error     string        `json:"error,omitempty"`
}

func (e TimeoutPositionCheckedEvent) GetMessage() string {
	if e.Error != "" {
		return "Timeout position check failed: " + e.Error
	}
	return "Timeout position checked"
}
func (e TimeoutPositionCheckedEvent) GetDataMap() map[string]any {
	m := map[string]any{keyHoldVol: e.HoldVol}
	if e.Error != "" {
		m[keyError] = e.Error
	}
	return m
}
func (e TimeoutPositionCheckedEvent) ShouldNotify() bool { return e.SendNotify || e.Error != "" }

type ForceCloseInitiatedEvent struct {
	BaseReversionEvent
	Timeout    time.Duration `json:"timeout"`
	StartedAt  time.Time     `json:"started_at"`
	HoldVol    float64       `json:"hold_vol"`
	TimeoutSec float64       `json:"timeout_sec"`
}

func (e ForceCloseInitiatedEvent) GetMessage() string {
	return "CRITICAL: Safety timeout close initiated"
}
func (e ForceCloseInitiatedEvent) GetDataMap() map[string]any {
	return map[string]any{keyHoldVol: e.HoldVol, keyTimeout: e.TimeoutSec}
}

type ForceCloseCompletedEvent struct {
	BaseReversionEvent
	Timeout         time.Duration `json:"timeout"`
	StartedAt       time.Time     `json:"started_at"`
	HoldVol         float64       `json:"hold_vol"`
	CloseRetryCount int           `json:"close_retry_count"`
	Succeeded       bool          `json:"succeeded"`
	Error           string        `json:"error,omitempty"`
}

func (e ForceCloseCompletedEvent) GetMessage() string {
	if !e.Succeeded {
		return "Force close failed: " + e.Error
	}
	return "Force close completed"
}
func (e ForceCloseCompletedEvent) GetDataMap() map[string]any {
	m := map[string]any{keyHoldVol: e.HoldVol, "retries": e.CloseRetryCount, "succeeded": e.Succeeded}
	if e.Error != "" {
		m[keyError] = e.Error
	}
	return m
}
func (e ForceCloseCompletedEvent) ShouldNotify() bool { return e.SendNotify || e.Error != "" }

type TimeoutEvent struct {
	BaseReversionEvent
	Timeout             time.Duration   `json:"timeout"`
	Reason              ReversionReason `json:"reason"`
	ForceCloseAttempted bool            `json:"force_close_attempted,omitempty"`
	ForceCloseSucceeded bool            `json:"force_close_succeeded,omitempty"`
	CloseRetryCount     int             `json:"close_retry_count,omitempty"`
	HoldVol             float64         `json:"hold_vol,omitempty"`
	HoldDurationMs      int64           `json:"hold_duration_ms,omitempty"`
	Error               string          `json:"error,omitempty"`
}

func (e TimeoutEvent) GetMessage() string {
	if e.Error != "" {
		return "Timeout guard failed: " + e.Error
	}
	return "Timeout guard triggered"
}
func (e TimeoutEvent) GetDataMap() map[string]any {
	m := map[string]any{
		keyTimeout:            e.Timeout.String(),
		keyReason:             string(e.Reason),
		"forceCloseSucceeded": e.ForceCloseSucceeded,
	}
	if e.Error != "" {
		m[keyError] = e.Error
	}
	return m
}
func (e TimeoutEvent) ShouldNotify() bool { return e.SendNotify || e.Error != "" }

type AbortEvent struct {
	BaseReversionEvent
	Reason ReversionReason `json:"reason"`
}

func (e AbortEvent) GetMessage() string {
	return "Cycle aborted: " + string(e.Reason)
}
func (e AbortEvent) GetDataMap() map[string]any {
	return map[string]any{keyReason: string(e.Reason)}
}
func (e AbortEvent) GetColor() EventColor {
	if e.Reason == reversionReasonIOCCanceledNoPosition || e.Reason == reversionReasonIOCUnknownNoPosition {
		return ColorBlue
	}
	if e.Color != "" {
		return e.Color
	}
	return ColorYellow
}

type ErrorEvent struct {
	BaseReversionEvent
	Error string `json:"error"`
}

func (e ErrorEvent) GetMessage() string { return "Cycle error: " + e.Error }
func (e ErrorEvent) GetDataMap() map[string]any {
	return map[string]any{keyError: e.Error}
}
func (e ErrorEvent) ShouldNotify() bool { return true }

type FinalPnLEvent struct {
	BaseReversionEvent
	EntryPrice     float64 `json:"entry_price"`
	ClosePrice     float64 `json:"close_price"`
	MaxVol         float64 `json:"max_vol"`
	GrossPnL       float64 `json:"gross_pnl"`
	NetPnL         float64 `json:"net_pnl"`
	PnLPct         float64 `json:"pnl_pct"`
	VolumeUSDT     float64 `json:"volume_usdt"`
	Fees           float64 `json:"fees"`
	HoldFee        float64 `json:"hold_fees"`
	HoldDurationMs int64   `json:"hold_duration_ms"`
}

func (e FinalPnLEvent) GetMessage() string {
	var sideStr string
	switch e.Side {
	case shared.SideOpenLong:
		sideStr = "Long"
	case shared.SideOpenShort:
		sideStr = "Short"
	default:
		raw := e.Side.String()
		if raw != "" {
			sideStr = strings.ToUpper(raw[:1]) + strings.ToLower(raw[1:])
		} else {
			sideStr = "Unknown"
		}
	}

	var priceDiffPct float64
	if e.EntryPrice > 0 {
		if e.Side == shared.SideOpenShort {
			priceDiffPct = ((e.EntryPrice - e.ClosePrice) / e.EntryPrice) * 100.0
		} else {
			priceDiffPct = ((e.ClosePrice - e.EntryPrice) / e.EntryPrice) * 100.0
		}
	}

	sign := ""
	if priceDiffPct > 0 {
		sign = "+"
	}

	priceStr := fmt.Sprintf("%s ➔ %s (%s%.2f%%)",
		formatutil.FormatPriceWithCommas(e.EntryPrice),
		formatutil.FormatPriceWithCommas(e.ClosePrice),
		sign,
		priceDiffPct,
	)
	sizeStr := fmt.Sprintf("%s USDT", formatutil.FormatUSDWithCommas(e.VolumeUSDT))
	feesStr := fmt.Sprintf("Exec: $%s | Funding: %s", formatutil.FormatUSDWithCommasAndDecimals(math.Abs(e.Fees), 4), formatutil.FormatFundingFee(e.HoldFee))
	netPnLStr := formatutil.FormatNetPnL(e.NetPnL, e.PnLPct)

	frSign := ""
	if e.FundingRate > 0 {
		frSign = "+"
	}
	frStr := fmt.Sprintf("%s%.1f%%", frSign, e.FundingRate*100)

	return fmt.Sprintf("PnL: %s [%s] | Side: %s | FR: %s\n• Price: %s | Size: %s\n• Fees: %s\n• Order ID: %s | Client ID: %s",
		netPnLStr,
		formatutil.FormatDuration(e.HoldDurationMs),
		sideStr,
		frStr,
		priceStr,
		sizeStr,
		feesStr,
		e.OrderID,
		e.ExternalID,
	)
}

func (e FinalPnLEvent) GetDataMap() map[string]any {
	return nil
}

func (e FinalPnLEvent) GetColor() EventColor {
	if e.NetPnL > 0 {
		return ColorGreen
	}
	if e.NetPnL < 0 {
		return ColorRed
	}
	return ColorYellow
}

type ReversionCompletedEvent struct {
	BaseReversionEvent
	Reason string `json:"reason"`
}

func (e ReversionCompletedEvent) GetMessage() string { return "Reversion completed" }
func (e ReversionCompletedEvent) GetDataMap() map[string]any {
	return map[string]any{keyReason: e.Reason}
}

type TPSLRequiredEvent struct {
	BaseReversionEvent
	PositionMode    shared.PositionMode `json:"position_mode"`
	TakeProfitPrice float64             `json:"take_profit_price,omitempty"`
	StopLossPrice   float64             `json:"stop_loss_price,omitempty"`
	Volume          float64             `json:"volume,omitempty"`
}

func (e TPSLRequiredEvent) GetMessage() string {
	return "Immediate TP/SL required"
}
func (e TPSLRequiredEvent) GetDataMap() map[string]any {
	m := map[string]any{
		"tp": e.TakeProfitPrice,
		"sl": e.StopLossPrice,
	}
	if e.OrderID != "" {
		m[keyOrderID] = e.OrderID
	}
	return m
}
