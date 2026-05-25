package reversion

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/ThreeDotsLabs/watermill/message"
)

func (r *StatelessRunner) handleCleanup(ctx context.Context, msg *message.Message) error {
	var baseEvt struct {
		ReqID  string `json:"req_id"`
		Symbol string `json:"symbol"`
		Reason string `json:"reason"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(msg.Payload, &baseEvt); err != nil {
		r.log.Error("Failed to unmarshal cleanup base event", slog.Any("error", err))
		return err
	}
	symbol := baseEvt.Symbol

	r.unsubscribeWS(ctx, symbol)

	// Check if this is a PositionClosedEvent containing rich trade metrics
	var closedEvt PositionClosedEvent
	if err := json.Unmarshal(msg.Payload, &closedEvt); err == nil && closedEvt.CloseVol > 0 {
		finalEvt := r.calculateFinalPnL(closedEvt)
		_ = r.publishEvent(ctx, TopicReversionFinalPnL, finalEvt)
	}

	compEvt := ReversionCompletedEvent{
		BaseReversionEvent: BaseReversionEvent{
			Flow:       FlowReversion,
			ReqID:      baseEvt.ReqID,
			Symbol:     symbol,
			Timestamp:  r.deps.Clock.Now(),
			SendNotify: false,
		},
		Reason: "cleanup_finished",
	}
	_ = r.publishEvent(ctx, TopicReversionCompleted, compEvt)

	return nil
}

func (r *StatelessRunner) calculateFinalPnL(closeEvt PositionClosedEvent) FinalPnLEvent {
	return FinalPnLEvent{
		BaseReversionEvent: BaseReversionEvent{
			Flow:       FlowReversion,
			ReqID:      closeEvt.ReqID,
			Symbol:     closeEvt.Symbol,
			Timestamp:  r.deps.Clock.Now(),
			SendNotify: true,
		},
		EntryPrice:     closeEvt.EntryPrice,
		ClosePrice:     closeEvt.ClosePrice,
		MaxVol:         closeEvt.CloseVol,
		GrossPnL:       closeEvt.GrossProfit,
		NetPnL:         closeEvt.NetProfit,
		Fees:           closeEvt.Fee,
		HoldFee:        closeEvt.HoldFee,
		HoldDurationMs: closeEvt.HoldDurationMs,
	}
}
