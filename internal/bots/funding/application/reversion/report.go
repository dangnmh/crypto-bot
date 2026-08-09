package reversion

import (
	"reflect"
	"sync"
	"time"

	shared "crypto-bot/internal/domain"

	"github.com/patrickmn/go-cache"
)

// CycleState tracks the state of an active reversion trade cycle in the cache.
type CycleState struct {
	ReqID              string      `json:"req_id"`
	Symbol             string      `json:"symbol"`
	Exchange           string      `json:"exchange"`
	Side               shared.Side `json:"side"`
	FundingRate        float64     `json:"funding_rate"`
	CandidateFoundTime time.Time   `json:"candidate_found_time"`
	SettleTime         time.Time   `json:"settle_time"`

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
	Vol24hUSDT     float64 `json:"vol_24h_usdt"`

	// Risk & Termination Status Fields
	CloseRetryCount     int    `json:"close_retry_count"`
	ForceCloseAttempted bool   `json:"force_close_attempted"`
	ForceCloseSucceeded bool   `json:"force_close_succeeded"`
	Status              string `json:"status"` // "completed", "aborted", "error"
	ErrorMsg            string `json:"error_msg"`

	mu sync.Mutex `json:"-"`
}

// ReversionTradeReportEvent is published at the end of a cycle.
type ReversionTradeReportEvent struct {
	BaseReversionEvent
	NormalizedSymbol   string      `json:"normalized_symbol"`
	SettleTime         time.Time   `json:"settle_time"`
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

var _cacheMu sync.Mutex

// recordEventState updates the in-memory cache when new events are emitted.
func (r *StatelessRunner) recordEventState(topic string, rawEvt any) {
	revEvt, ok := rawEvt.(ReversionEvent)
	if !ok {
		return
	}
	reqID := revEvt.GetReqID()
	if reqID == "" {
		return
	}

	// De-reference pointer if rawEvt is a pointer
	val := rawEvt
	rv := reflect.ValueOf(rawEvt)
	if rv.Kind() == reflect.Pointer && !rv.IsNil() {
		val = rv.Elem().Interface()
	}

	// Load or create state
	var state *CycleState
	_cacheMu.Lock()
	cachedVal, found := r.cache.Get(reqID)
	if found {
		var ok bool
		state, ok = cachedVal.(*CycleState)
		_cacheMu.Unlock()
		if !ok {
			return
		}
	} else {
		state = &CycleState{
			ReqID:    reqID,
			Symbol:   r.symbol,
			Exchange: r.exchange,
			Status:   "running",
		}
		r.cache.Set(reqID, state, cache.DefaultExpiration)
		_cacheMu.Unlock()
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	if r.recordStage1State(state, val) {
		return
	}
	if r.recordStage2State(state, val) {
		return
	}
	_ = r.recordStage3State(state, val)
}

func (r *StatelessRunner) recordStage1State(state *CycleState, val any) bool {
	switch evt := val.(type) {
	case CandidateFoundEvent:
		state.SettleTime = evt.SettleTime
		state.Side = evt.Candidate.Side
		state.FundingRate = evt.Candidate.FundingRate
		state.CandidateFoundTime = evt.Timestamp
		state.MarginUSDT = evt.Candidate.Config.MarginUSDT
		state.Leverage = evt.Candidate.Config.Leverage
		state.Vol24hUSDT = evt.Candidate.Vol24USDT
		return true
	case ArmMarketReadyEvent:
		state.SettleTime = evt.SettleTime
		return true
	case ArmPlanCalculatedEvent:
		state.SettleTime = evt.SettleTime
		return true
	case ArmedEvent:
		state.SettleTime = evt.SettleTime
		return true
	case WaitCompleteEvent:
		state.SettleTime = evt.SettleTime
		return true
	case ConfirmedEvent:
		state.SettleTime = evt.SettleTime
		state.FundingRate = evt.FundingRate
		return true
	case MarginModeReadyEvent:
		state.SettleTime = evt.SettleTime
		state.FundingRate = evt.FundingRate
		return true
	default:
		return false
	}
}

func (r *StatelessRunner) recordStage2State(state *CycleState, val any) bool {
	switch evt := val.(type) {
	case FireTimingReadyEvent:
		state.SettleTime = evt.SettleTime
		state.LatencyRTTMs = evt.LatencyRTTMs
		state.FireOffsetMs = evt.FireOffsetMs
		state.BufferTimeMs = time.Duration(evt.Candidate.Config.FundingReversion.BufferTime).Milliseconds()
		return true
	case FirePlanCheckedEvent:
		state.SettleTime = evt.SettleTime
		return true
	case FireWindowReachedEvent:
		state.SettleTime = evt.SettleTime
		return true
	case PositionWatchReadyEvent:
		state.SettleTime = evt.SettleTime
		return true
	case IOCSubmittedEvent:
		state.SettleTime = evt.SettleTime
		state.IOCOrderID = evt.OrderID
		if !evt.FireIOCTime.IsZero() {
			state.FireIOCTime = evt.FireIOCTime
		}
		if !evt.LocalFireIOCTime.IsZero() {
			state.LocalFireIOCTime = evt.LocalFireIOCTime
		}
		if evt.Error != "" {
			state.Status = StatusAborted
			state.ErrorMsg = evt.Error
			state.IOCReason = evt.Error
		}
		return true
	case IOCOutcomeCheckedEvent:
		state.SettleTime = evt.SettleTime
		state.IOCOutcome = string(evt.Outcome)
		state.IOCReason = string(evt.Reason)
		return true
	case OrderFilledEvent:
		state.SettleTime = evt.SettleTime
		state.FillPrice = evt.FillPrice
		state.ActualSlippage = evt.SlippagePct
		state.OrderFilled = true
		return true
	case PositionClosedEvent:
		state.SettleTime = evt.SettleTime
		state.OrderFilled = true
		state.FillPrice = evt.EntryPrice
		state.ClosePrice = evt.ClosePrice
		state.VolumeUSDT = evt.VolumeUSDT
		state.GrossProfit = evt.GrossProfit
		state.NetProfit = evt.NetProfit
		state.PnLPct = evt.PnLPct
		state.Fee = evt.Fee
		state.HoldFee = evt.HoldFee
		state.HoldDurationMs = evt.HoldDurationMs
		state.ExitReason = evt.Reason
		state.CloseRetryCount = evt.CloseRetryCount
		return true
	default:
		return false
	}
}

func (r *StatelessRunner) recordStage3State(state *CycleState, val any) bool {
	switch evt := val.(type) {
	case TimeoutGuardScheduledEvent:
		state.SettleTime = evt.SettleTime
		return true
	case TimeoutPositionCheckedEvent:
		state.SettleTime = evt.SettleTime
		if evt.Error != "" {
			state.ErrorMsg = evt.Error
		}
		return true
	case ForceCloseInitiatedEvent:
		state.SettleTime = evt.SettleTime
		state.ForceCloseAttempted = true
		return true
	case ForceCloseCompletedEvent:
		state.SettleTime = evt.SettleTime
		state.ForceCloseAttempted = true
		state.ForceCloseSucceeded = evt.Succeeded
		if evt.Error != "" {
			state.ErrorMsg = evt.Error
		}
		return true
	case TimeoutEvent:
		state.SettleTime = evt.SettleTime
		state.ForceCloseAttempted = evt.ForceCloseAttempted
		state.ForceCloseSucceeded = evt.ForceCloseSucceeded
		state.CloseRetryCount = evt.CloseRetryCount
		state.HoldDurationMs = evt.HoldDurationMs
		state.ExitReason = "timeout"
		if evt.Error != "" {
			state.ErrorMsg = evt.Error
		}
		return true
	case AbortEvent:
		state.SettleTime = evt.SettleTime
		state.Status = StatusAborted
		state.ErrorMsg = string(evt.Reason)
		state.IOCReason = string(evt.Reason)
		return true
	case ErrorEvent:
		state.SettleTime = evt.SettleTime
		state.Status = StatusError
		state.ErrorMsg = evt.Error
		return true
	default:
		return false
	}
}
