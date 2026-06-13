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
	if err != nil {
		r.log.DebugContext(ctx, "unmarshal failed", slog.Any("error", err))
	} else if closedEvt.CloseVol > 0 {
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

	if reqID := baseEvt.ReqID; reqID != "" {
		completedCleanups.Store(reqID, true)
	}

	return nil
}

func (r *StatelessRunner) calculateFinalPnL(closeEvt PositionClosedEvent) FinalPnLEvent {
	return FinalPnLEvent{
		BaseReversionEvent: nextNotifyReversionBase(closeEvt.BaseReversionEvent, closeEvt.Symbol, r.deps.Clock.Now()),
		Direction:          closeEvt.Direction,
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
