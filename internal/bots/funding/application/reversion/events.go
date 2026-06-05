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
)

const (
	IOCOutcomeFilled                     = "filled"
	IOCOutcomePartialFilled              = "partial_filled"
	IOCOutcomeCanceledNoFill             = "canceled_no_fill"
	IOCOutcomeUnknown                    = "unknown"
	reversionReasonIOCCanceledNoPosition = "ioc_canceled_no_position"
	reversionReasonIOCUnknownNoPosition  = "ioc_outcome_unknown_no_position"
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
)

const (
	colorYellow = "yellow"
	colorGreen  = "green"
	colorRed    = "red"
)

type ReversionEvent interface {
	GetFlow() string
	GetReqID() string
	GetSymbol() string
	GetExchange() string
	GetMessage() string
	GetDataMap() map[string]any
	ShouldNotify() bool
	GetColor() string
}

type BaseReversionEvent struct {
	Flow                   string    `json:"flow,omitempty"`
	ReqID                  string    `json:"req_id,omitempty"`
	Symbol                 string    `json:"symbol"`
	Exchange               string    `json:"exchange,omitempty"`
	SendNotify             bool      `json:"send_notify,omitempty"`
	Color                  string    `json:"color,omitempty"`
	OrderID                string    `json:"order_id,omitempty"`
	ExternalID             string    `json:"external_id,omitempty"`
	Timestamp              time.Time `json:"timestamp"`
	EventID                string    `json:"event_id,omitempty"`
	Seq                    int64     `json:"seq,omitempty"`
	Topic                  string    `json:"topic,omitempty"`
	PreviousTopic          string    `json:"previous_topic,omitempty"`
	SettleTime             time.Time `json:"settle_time"`
	SupportLeverageOnOrder bool      `json:"support_leverage_on_order,omitempty"`
}

func (b BaseReversionEvent) GetFlow() string     { return b.Flow }
func (b BaseReversionEvent) GetReqID() string    { return b.ReqID }
func (b BaseReversionEvent) GetSymbol() string   { return b.Symbol }
func (b BaseReversionEvent) GetExchange() string { return b.Exchange }
func (b BaseReversionEvent) ShouldNotify() bool  { return b.SendNotify }
func (b BaseReversionEvent) GetColor() string {
	if b.Color != "" {
		return b.Color
	}
	return colorYellow
}

type CandidateFoundEvent struct {
	BaseReversionEvent
	Candidate fundingdomain.Candidate `json:"candidate"`
}

func (e CandidateFoundEvent) GetMessage() string { return "Candidate found for " + e.Symbol }
func (e CandidateFoundEvent) GetDataMap() map[string]any {
	return map[string]any{keySymbol: e.Symbol, keyFundingRate: e.Candidate.FundingRate, "side": e.Candidate.Side.String()}
}

func (e CandidateFoundEvent) DeduplicateKey() string {
	if e.SettleTime.IsZero() {
		return ""
	}
	return fmt.Sprintf("reversion:%s:%s:%d", e.Exchange, e.Symbol, e.SettleTime.Unix())
}

type ArmMarketReadyEvent struct {
	BaseReversionEvent
	Candidate fundingdomain.Candidate `json:"candidate"`
	MaxWaitMs int64                   `json:"max_wait_ms"`
	BestBid   float64                 `json:"best_bid"`
	BestAsk   float64                 `json:"best_ask"`
	LastPrice float64                 `json:"last_price"`
}

func (e ArmMarketReadyEvent) GetMessage() string { return "Arm market ready for " + e.Symbol }
func (e ArmMarketReadyEvent) GetDataMap() map[string]any {
	return map[string]any{keySymbol: e.Symbol, keyBestBid: e.BestBid, keyBestAsk: e.BestAsk, keyLastPrice: e.LastPrice}
}

type ArmPlanCalculatedEvent struct {
	BaseReversionEvent
	Candidate       fundingdomain.Candidate `json:"candidate"`
	IOCPrice        float64                 `json:"ioc_price"`
	RefPrice        float64                 `json:"ref_price"`
	Slippage        float64                 `json:"slippage"`
	RequestedVolume float64                 `json:"requested_volume"`
}

func (e ArmPlanCalculatedEvent) GetMessage() string { return "Arm plan calculated for " + e.Symbol }
func (e ArmPlanCalculatedEvent) GetDataMap() map[string]any {
	return map[string]any{keySymbol: e.Symbol, keyIOCPrice: e.IOCPrice, keySlippage: e.Slippage, keyVolume: e.RequestedVolume}
}

type SafetyCheckedEvent struct {
	BaseReversionEvent
	Candidate       fundingdomain.Candidate `json:"candidate"`
	IOCPrice        float64                 `json:"ioc_price"`
	RefPrice        float64                 `json:"ref_price"`
	Slippage        float64                 `json:"slippage"`
	RequestedVolume float64                 `json:"requested_volume"`
	AdjustedVolume  float64                 `json:"adjusted_volume"`
	Passed          bool                    `json:"passed"`
	RejectReason    string                  `json:"reject_reason,omitempty"`
}

func (e SafetyCheckedEvent) GetMessage() string {
	if !e.Passed {
		return "Safety rejected for " + e.Symbol + ": " + e.RejectReason
	}
	return "Safety checked for " + e.Symbol
}
func (e SafetyCheckedEvent) GetDataMap() map[string]any {
	return map[string]any{keySymbol: e.Symbol, keyPassed: e.Passed, keyReason: e.RejectReason, keyVolume: e.AdjustedVolume}
}

type ArmedEvent struct {
	BaseReversionEvent
	Candidate fundingdomain.Candidate `json:"candidate"`
	Volume    float64                 `json:"volume"`
	IOCPrice  float64                 `json:"ioc_price"`
	Slippage  float64                 `json:"slippage"`
}

func (e ArmedEvent) GetMessage() string { return "Reversion armed for " + e.Symbol }
func (e ArmedEvent) GetDataMap() map[string]any {
	return map[string]any{keySymbol: e.Symbol, keyVolume: e.Volume, keyIOCPrice: e.IOCPrice, keySlippage: e.Slippage}
}

type WaitCompleteEvent struct {
	BaseReversionEvent
	Candidate fundingdomain.Candidate `json:"candidate"`
}

func (e WaitCompleteEvent) GetMessage() string { return "Wait complete for " + e.Symbol }
func (e WaitCompleteEvent) GetDataMap() map[string]any {
	return map[string]any{keySymbol: e.Symbol, "settleTime": e.SettleTime}
}

type ConfirmedEvent struct {
	BaseReversionEvent
	FundingRate float64                 `json:"funding_rate"`
	Candidate   fundingdomain.Candidate `json:"candidate"`
}

func (e ConfirmedEvent) GetMessage() string { return "Recheck confirmed for " + e.Symbol }
func (e ConfirmedEvent) GetDataMap() map[string]any {
	return map[string]any{keySymbol: e.Symbol, keyFundingRate: e.FundingRate}
}

type MarginModeReadyEvent struct {
	BaseReversionEvent
	FundingRate float64                 `json:"funding_rate"`
	Candidate   fundingdomain.Candidate `json:"candidate"`
}

func (e MarginModeReadyEvent) GetMessage() string { return "Margin mode ready for " + e.Symbol }
func (e MarginModeReadyEvent) GetDataMap() map[string]any {
	return map[string]any{keySymbol: e.Symbol, keyFundingRate: e.FundingRate}
}

type FireTimingReadyEvent struct {
	BaseReversionEvent
	Candidate        fundingdomain.Candidate `json:"candidate"`
	FundingRate      float64                 `json:"funding_rate"`
	LatencyRTTMs     int64                   `json:"latency_rtt_ms"`
	FireOffsetMs     int64                   `json:"fire_offset_ms"`
	SnapshotOffsetMs int64                   `json:"snapshot_offset_ms"`
}

func (e FireTimingReadyEvent) GetMessage() string { return "Fire timing ready for " + e.Symbol }
func (e FireTimingReadyEvent) GetDataMap() map[string]any {
	return map[string]any{keySymbol: e.Symbol, keyLatencyRTT: e.LatencyRTTMs, "fireOffsetMs": e.FireOffsetMs}
}

type FirePlanCheckedEvent struct {
	BaseReversionEvent
	Candidate       fundingdomain.Candidate `json:"candidate"`
	LatencyRTTMs    int64                   `json:"latency_rtt_ms"`
	FireOffsetMs    int64                   `json:"fire_offset_ms"`
	BestBid         float64                 `json:"best_bid"`
	BestAsk         float64                 `json:"best_ask"`
	LastPrice       float64                 `json:"last_price"`
	IOCPrice        float64                 `json:"ioc_price"`
	RefPrice        float64                 `json:"ref_price"`
	Slippage        float64                 `json:"slippage"`
	RequestedVolume float64                 `json:"requested_volume"`
	AdjustedVolume  float64                 `json:"adjusted_volume"`
	Passed          bool                    `json:"passed"`
	RejectReason    string                  `json:"reject_reason,omitempty"`
}

func (e FirePlanCheckedEvent) GetMessage() string {
	if !e.Passed {
		return "Fire plan rejected for " + e.Symbol + ": " + e.RejectReason
	}
	return "Fire plan checked for " + e.Symbol
}
func (e FirePlanCheckedEvent) GetDataMap() map[string]any {
	return map[string]any{keySymbol: e.Symbol, keyPassed: e.Passed, keyReason: e.RejectReason, keyVolume: e.AdjustedVolume}
}

type FireWindowReachedEvent struct {
	BaseReversionEvent
	Candidate     fundingdomain.Candidate `json:"candidate"`
	LatencyRTTMs  int64                   `json:"latency_rtt_ms"`
	FireTimestamp time.Time               `json:"fire_timestamp"`
}

func (e FireWindowReachedEvent) GetMessage() string { return "Fire window reached for " + e.Symbol }
func (e FireWindowReachedEvent) GetDataMap() map[string]any {
	return map[string]any{keySymbol: e.Symbol, keyLatencyRTT: e.LatencyRTTMs}
}

type PositionWatchReadyEvent struct {
	BaseReversionEvent
	Candidate     fundingdomain.Candidate `json:"candidate"`
	LatencyRTTMs  int64                   `json:"latency_rtt_ms"`
	FireTimestamp time.Time               `json:"fire_timestamp"`
	Timeout       time.Duration           `json:"timeout"`
}

func (e PositionWatchReadyEvent) GetMessage() string { return "Position watch ready for " + e.Symbol }
func (e PositionWatchReadyEvent) GetDataMap() map[string]any {
	return map[string]any{keySymbol: e.Symbol, keyTimeout: e.Timeout.String()}
}

type IOCSubmittedEvent struct {
	BaseReversionEvent
	OrderID       string      `json:"order_id"`
	ExternalID    string      `json:"external_id"`
	Side          shared.Side `json:"side"`
	CloseSide     shared.Side `json:"close_side"`
	OrderType     int         `json:"order_type"`
	IntendedPrice float64     `json:"intended_price"`
	Volume        float64     `json:"volume"`
	TPPrice       float64     `json:"tp_price,omitempty"`
	SLPrice       float64     `json:"sl_price,omitempty"`
	TPSLSubmitted bool        `json:"tpsl_submitted"`
	FireTimestamp time.Time   `json:"fire_timestamp"`
	LatencyRTTMs  int64       `json:"latency_rtt_ms,omitempty"`
	Error         string      `json:"error,omitempty"`
}

func (e IOCSubmittedEvent) GetMessage() string {
	if e.Error != "" {
		return "IOC order failed for " + e.Symbol + ": " + e.Error
	}
	return "IOC order submitted for " + e.Symbol
}
func (e IOCSubmittedEvent) GetDataMap() map[string]any {
	return map[string]any{
		keySymbol:    e.Symbol,
		keyOrderID:   e.OrderID,
		"externalId": e.ExternalID,
		keyIOCPrice:  e.IntendedPrice,
		keyVolume:    e.Volume,
		keyError:     e.Error,
	}
}
func (e IOCSubmittedEvent) ShouldNotify() bool { return e.SendNotify || e.Error != "" }

type IOCOutcomeCheckedEvent struct {
	BaseReversionEvent
	IOCEvent     IOCSubmittedEvent `json:"ioc_event"`
	OrderID      string            `json:"order_id"`
	OrderState   int               `json:"order_state"`
	DealVol      float64           `json:"deal_vol"`
	DealAvgPrice float64           `json:"deal_avg_price"`
	HoldVol      float64           `json:"hold_vol"`
	Outcome      string            `json:"outcome"`
	Reason       string            `json:"reason"`
	CheckedAt    time.Time         `json:"checked_at"`
}

func (e IOCOutcomeCheckedEvent) GetMessage() string { return "IOC outcome checked for " + e.Symbol }
func (e IOCOutcomeCheckedEvent) GetDataMap() map[string]any {
	return map[string]any{keySymbol: e.Symbol, keyOrderID: e.OrderID, keyOutcome: e.Outcome, keyHoldVol: e.HoldVol, keyReason: e.Reason}
}

type OrderFilledEvent struct {
	BaseReversionEvent
	OrderID     string      `json:"order_id"`
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

func (e OrderFilledEvent) GetMessage() string { return "Position filled for " + e.Symbol }
func (e OrderFilledEvent) GetDataMap() map[string]any {
	return map[string]any{keySymbol: e.Symbol, keyOrderID: e.OrderID, "fillPrice": e.FillPrice, "fillVol": e.FillVol, keyVolumeUSDT: e.VolumeUSDT}
}

type PositionClosedEvent struct {
	BaseReversionEvent
	EntryPrice      float64     `json:"entry_price"`
	ClosePrice      float64     `json:"close_price"`
	CloseVol        float64     `json:"close_vol"`
	Reason          string      `json:"reason"`
	GrossProfit     float64     `json:"gross_profit"`
	NetProfit       float64     `json:"net_profit"`
	PnLPct          float64     `json:"pnl_pct"`
	VolumeUSDT      float64     `json:"volume_usdt"`
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
	return "Position closed for " + e.Symbol + " (" + e.Reason + ")"
}
func (e PositionClosedEvent) GetDataMap() map[string]any {
	return map[string]any{
		keySymbol:        e.Symbol,
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
func (e PositionClosedEvent) GetColor() string {
	if e.NetProfit > 0 {
		return colorGreen
	}
	if e.NetProfit < 0 {
		return colorRed
	}
	return colorYellow
}
func (e PositionClosedEvent) ShouldNotify() bool { return false }

type TimeoutGuardScheduledEvent struct {
	BaseReversionEvent
	IOCEvent  IOCSubmittedEvent `json:"ioc_event"`
	Timeout   time.Duration     `json:"timeout"`
	StartedAt time.Time         `json:"started_at"`
}

func (e TimeoutGuardScheduledEvent) GetMessage() string {
	return "Timeout guard scheduled for " + e.Symbol
}
func (e TimeoutGuardScheduledEvent) GetDataMap() map[string]any {
	return map[string]any{keySymbol: e.Symbol, keyTimeout: e.Timeout.String()}
}

type TimeoutPositionCheckedEvent struct {
	BaseReversionEvent
	IOCEvent  IOCSubmittedEvent `json:"ioc_event"`
	Timeout   time.Duration     `json:"timeout"`
	StartedAt time.Time         `json:"started_at"`
	HoldVol   float64           `json:"hold_vol"`
	Error     string            `json:"error,omitempty"`
}

func (e TimeoutPositionCheckedEvent) GetMessage() string {
	if e.Error != "" {
		return "Timeout position check failed for " + e.Symbol + ": " + e.Error
	}
	return "Timeout position checked for " + e.Symbol
}
func (e TimeoutPositionCheckedEvent) GetDataMap() map[string]any {
	return map[string]any{keySymbol: e.Symbol, keyHoldVol: e.HoldVol, keyError: e.Error}
}
func (e TimeoutPositionCheckedEvent) ShouldNotify() bool { return e.SendNotify || e.Error != "" }

type ForceCloseInitiatedEvent struct {
	BaseReversionEvent
	IOCEvent   IOCSubmittedEvent `json:"ioc_event"`
	Timeout    time.Duration     `json:"timeout"`
	StartedAt  time.Time         `json:"started_at"`
	HoldVol    float64           `json:"hold_vol"`
	TimeoutSec float64           `json:"timeout_sec"`
}

func (e ForceCloseInitiatedEvent) GetMessage() string {
	return "CRITICAL: Initiating safety timeout force close for " + e.Symbol
}
func (e ForceCloseInitiatedEvent) GetDataMap() map[string]any {
	return map[string]any{keySymbol: e.Symbol, keyHoldVol: e.HoldVol, keyTimeout: e.TimeoutSec}
}

type ForceCloseCompletedEvent struct {
	BaseReversionEvent
	IOCEvent        IOCSubmittedEvent `json:"ioc_event"`
	Timeout         time.Duration     `json:"timeout"`
	StartedAt       time.Time         `json:"started_at"`
	HoldVol         float64           `json:"hold_vol"`
	CloseRetryCount int               `json:"close_retry_count"`
	Succeeded       bool              `json:"succeeded"`
	Error           string            `json:"error,omitempty"`
}

func (e ForceCloseCompletedEvent) GetMessage() string {
	if !e.Succeeded {
		return "Force close failed for " + e.Symbol + ": " + e.Error
	}
	return "Force close completed for " + e.Symbol
}
func (e ForceCloseCompletedEvent) GetDataMap() map[string]any {
	return map[string]any{keySymbol: e.Symbol, keyHoldVol: e.HoldVol, "retries": e.CloseRetryCount, "succeeded": e.Succeeded, keyError: e.Error}
}
func (e ForceCloseCompletedEvent) ShouldNotify() bool { return e.SendNotify || e.Error != "" }

type TimeoutEvent struct {
	BaseReversionEvent
	Timeout             time.Duration `json:"timeout"`
	Reason              string        `json:"reason"`
	ForceCloseAttempted bool          `json:"force_close_attempted,omitempty"`
	ForceCloseSucceeded bool          `json:"force_close_succeeded,omitempty"`
	CloseRetryCount     int           `json:"close_retry_count,omitempty"`
	HoldVol             float64       `json:"hold_vol,omitempty"`
	HoldDurationMs      int64         `json:"hold_duration_ms,omitempty"`
	Direction           shared.Side   `json:"direction,omitempty"`
	Error               string        `json:"error,omitempty"`
}

func (e TimeoutEvent) GetMessage() string {
	if e.Error != "" {
		return "Timeout guard triggered for " + e.Symbol + " but failed to close: " + e.Error
	}
	return "Timeout guard triggered for " + e.Symbol
}
func (e TimeoutEvent) GetDataMap() map[string]any {
	return map[string]any{keySymbol: e.Symbol, keyTimeout: e.Timeout.String(), keyReason: e.Reason, "forceCloseSucceeded": e.ForceCloseSucceeded}
}
func (e TimeoutEvent) ShouldNotify() bool { return e.SendNotify || e.Error != "" }

type AbortEvent struct {
	BaseReversionEvent
	Reason string `json:"reason"`
}

func (e AbortEvent) GetMessage() string { return "Cycle aborted for " + e.Symbol + ": " + e.Reason }
func (e AbortEvent) GetDataMap() map[string]any {
	return map[string]any{keySymbol: e.Symbol, keyReason: e.Reason}
}

type ErrorEvent struct {
	BaseReversionEvent
	Error string `json:"error"`
}

func (e ErrorEvent) GetMessage() string { return "Cycle error for " + e.Symbol + ": " + e.Error }
func (e ErrorEvent) GetDataMap() map[string]any {
	return map[string]any{keySymbol: e.Symbol, keyError: e.Error}
}
func (e ErrorEvent) ShouldNotify() bool { return true }

type FinalPnLEvent struct {
	BaseReversionEvent
	Direction      shared.Side `json:"direction"`
	EntryPrice     float64     `json:"entry_price"`
	ClosePrice     float64     `json:"close_price"`
	MaxVol         float64     `json:"max_vol"`
	GrossPnL       float64     `json:"gross_pnl"`
	NetPnL         float64     `json:"net_pnl"`
	PnLPct         float64     `json:"pnl_pct"`
	VolumeUSDT     float64     `json:"volume_usdt"`
	Fees           float64     `json:"fees"`
	HoldFee        float64     `json:"hold_fees"`
	HoldDurationMs int64       `json:"hold_duration_ms"`
}

func (e FinalPnLEvent) GetMessage() string {
	var sideStr string
	switch e.Direction {
	case shared.SideOpenLong:
		sideStr = "Long"
	case shared.SideOpenShort:
		sideStr = "Short"
	default:
		raw := e.Direction.String()
		if raw != "" {
			sideStr = strings.ToUpper(raw[:1]) + strings.ToLower(raw[1:])
		} else {
			sideStr = "Unknown"
		}
	}

	var priceDiffPct float64
	if e.EntryPrice > 0 {
		if e.Direction == shared.SideOpenShort {
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

	return fmt.Sprintf("Final PnL for %s of Flow %s\n• Side: %s\n• Price: %s\n• Size: %s\n• Fees: %s\n• Net PnL: %s [%s]",
		e.Symbol,
		e.Flow,
		sideStr,
		priceStr,
		sizeStr,
		feesStr,
		netPnLStr,
		formatutil.FormatDuration(e.HoldDurationMs),
	)
}

func (e FinalPnLEvent) GetDataMap() map[string]any {
	return nil
}

func (e FinalPnLEvent) GetColor() string {
	if e.NetPnL > 0 {
		return colorGreen
	}
	if e.NetPnL < 0 {
		return colorRed
	}
	return colorYellow
}

type ReversionCompletedEvent struct {
	BaseReversionEvent
	Reason string `json:"reason"`
}

func (e ReversionCompletedEvent) GetMessage() string { return "Reversion completed for " + e.Symbol }
func (e ReversionCompletedEvent) GetDataMap() map[string]any {
	return map[string]any{keySymbol: e.Symbol, keyReason: e.Reason}
}

type TPSLRequiredEvent struct {
	BaseReversionEvent
	OrderID         string  `json:"order_id"`
	Side            int     `json:"side"`
	PositionMode    int     `json:"position_mode"`
	TakeProfitPrice float64 `json:"take_profit_price,omitempty"`
	StopLossPrice   float64 `json:"stop_loss_price,omitempty"`
	Volume          float64 `json:"volume,omitempty"`
}

func (e TPSLRequiredEvent) GetMessage() string {
	return "Immediate TP/SL placement required for " + e.Symbol
}
func (e TPSLRequiredEvent) GetDataMap() map[string]any {
	return map[string]any{
		keySymbol:  e.Symbol,
		keyOrderID: e.OrderID,
		"tp":       e.TakeProfitPrice,
		"sl":       e.StopLossPrice,
	}
}
