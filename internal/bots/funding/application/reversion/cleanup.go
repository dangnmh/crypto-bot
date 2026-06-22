package reversion

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/ThreeDotsLabs/watermill/message"
)

// completedCleanups tracks completed request IDs to avoid duplicate fallback cleanups.
var completedCleanups sync.Map

func (r *StatelessRunner) handleCleanup(ctx context.Context, msg *message.Message) error {
	var baseEvt struct {
		BaseReversionEvent
		Reason string `json:"reason"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(msg.Payload, &baseEvt); err != nil {
		r.log.Error("Failed to unmarshal cleanup base event", slog.Any("error", err))
		return err
	}
	symbol := baseEvt.Symbol

	r.unsubscribeWS(ctx, symbol)

	completedPrev := baseEvt.BaseReversionEvent

	// Check if this is a PositionClosedEvent containing rich trade metrics
	var closedEvt PositionClosedEvent
	err := json.Unmarshal(msg.Payload, &closedEvt)
	if err == nil && closedEvt.CloseVol > 0 {
		finalEvt := r.calculateFinalPnL(closedEvt)
		_ = r.publishEvent(ctx, TopicReversionFinalPnL, finalEvt)
		completedPrev = finalEvt.BaseReversionEvent
		completedPrev.Topic = TopicReversionFinalPnL
	}

	compEvt := ReversionCompletedEvent{
		BaseReversionEvent: nextReversionBase(completedPrev, symbol, r.deps.Clock.Now()),
		Reason:             "cleanup_finished",
	}
	_ = r.publishEvent(ctx, TopicReversionCompleted, compEvt)

	// 2. Compile, publish the unified trade report and evict from cache
	r.compileAndPublishReport(ctx, baseEvt.ReqID, baseEvt.Topic, baseEvt.Error)

	if reqID := baseEvt.ReqID; reqID != "" {
		completedCleanups.Store(reqID, true)
	}

	return nil
}

func (r *StatelessRunner) calculateFinalPnL(closeEvt PositionClosedEvent) FinalPnLEvent {
	return FinalPnLEvent{
		BaseReversionEvent: nextNotifyReversionBase(closeEvt.BaseReversionEvent, closeEvt.Symbol, r.deps.Clock.Now()),
		EntryPrice:         closeEvt.EntryPrice,
		ClosePrice:         closeEvt.ClosePrice,
		MaxVol:             closeEvt.CloseVol,
		GrossPnL:           closeEvt.GrossProfit,
		NetPnL:             closeEvt.NetProfit,
		PnLPct:             closeEvt.PnLPct,
		VolumeUSDT:         closeEvt.VolumeUSDT,
		Fees:               closeEvt.Fee,
		HoldFee:            closeEvt.HoldFee,
		HoldDurationMs:     closeEvt.HoldDurationMs,
	}
}

// compileAndPublishReport compiles the reversion cycle's final report, publishes it, and evicts from the cache.
func (r *StatelessRunner) compileAndPublishReport(ctx context.Context, reqID, topic, errorMsg string) {
	if r.cache == nil || reqID == "" {
		return
	}

	cachedVal, found := r.cache.Get(reqID)
	if !found {
		return
	}

	state, ok := cachedVal.(*CycleState)
	if !ok {
		return
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	// Determine status based on terminal topic
	var status string
	switch topic {
	case TopicReversionAbort:
		status = StatusAborted
	case TopicReversionError:
		status = StatusError
	default:
		status = StatusCompleted
	}
	state.Status = status

	// If error, also record the error msg
	if topic == TopicReversionError && errorMsg != "" {
		state.ErrorMsg = errorMsg
	}

	// Compile the final ReversionTradeReportEvent
	reportEvt := ReversionTradeReportEvent{
		BaseReversionEvent: BaseReversionEvent{
			Flow:       FlowReversion,
			ReqID:      state.ReqID,
			Symbol:     state.Symbol,
			Exchange:   state.Exchange,
			Timestamp:  r.deps.Clock.Now(),
			SettleTime: state.SettleTime,
			Side:       state.Side,
		},
		NormalizedSymbol:    GetNormalizedSymbol(state.Symbol),
		SettleTime:          state.SettleTime,
		Side:                state.Side,
		FundingRate:         state.FundingRate,
		CandidateFoundTime:  state.CandidateFoundTime,
		MarginUSDT:          state.MarginUSDT,
		Leverage:            state.Leverage,
		BufferTimeMs:        state.BufferTimeMs,
		LatencyRTTMs:        state.LatencyRTTMs,
		ActualSlippage:      state.ActualSlippage,
		FireOffsetMs:        state.FireOffsetMs,
		IOCOrderID:          state.IOCOrderID,
		IOCOutcome:          state.IOCOutcome,
		IOCReason:           state.IOCReason,
		FireIOCTime:         state.FireIOCTime,
		LocalFireIOCTime:    state.LocalFireIOCTime,
		OrderFilled:         state.OrderFilled,
		FillPrice:           state.FillPrice,
		ClosePrice:          state.ClosePrice,
		VolumeUSDT:          state.VolumeUSDT,
		GrossProfit:         state.GrossProfit,
		NetProfit:           state.NetProfit,
		PnLPct:              state.PnLPct,
		Fee:                 state.Fee,
		HoldFee:             state.HoldFee,
		HoldDurationMs:      state.HoldDurationMs,
		ExitReason:          state.ExitReason,
		CloseRetryCount:     state.CloseRetryCount,
		ForceCloseAttempted: state.ForceCloseAttempted,
		ForceCloseSucceeded: state.ForceCloseSucceeded,
		Status:              state.Status,
		ErrorMsg:            state.ErrorMsg,
	}

	// Publish the report event
	_ = r.publishEvent(ctx, TopicReversionTradeReport, reportEvt)
}
