package reversion

import (
	"context"
	"log/slog"

	"crypto-bot/internal/infrastructure/exchange"
)

type iocOrderPollResult struct {
	order        *exchange.OrderInfo
	lastState    int
	dealVol      float64
	dealAvgPrice float64
	reason       string
}

func (r *StatelessRunner) handleIOCSubmitted(ctx context.Context, evt IOCSubmittedEvent) error {
	if evt.Error != "" {
		return nil
	}
	outcome := r.resolveIOCOutcome(ctx, evt)
	return r.publishEvent(ctx, TopicReversionIOCOutcomeChecked, outcome)
}

func (r *StatelessRunner) resolveIOCOutcome(ctx context.Context, evt IOCSubmittedEvent) IOCOutcomeCheckedEvent {
	poll := r.pollIOCOrder(ctx, evt.Symbol, evt.OrderID)

	holdVol, err := r.getHoldVolume(ctx, evt.Symbol)
	if err != nil {
		poll.reason = err.Error()
	}

	outcome, reason := classifyIOCOutcome(poll.order, holdVol, poll.dealVol, poll.reason)

	return IOCOutcomeCheckedEvent{
		BaseReversionEvent: nextReversionBase(evt.BaseReversionEvent, evt.Symbol, r.deps.Clock.Now()),
		IOCEvent:           evt,
		OrderID:            evt.OrderID,
		OrderState:         poll.lastState,
		DealVol:            poll.dealVol,
		DealAvgPrice:       poll.dealAvgPrice,
		HoldVol:            holdVol,
		Outcome:            outcome,
		Reason:             reason,
		CheckedAt:          r.deps.Clock.Now(),
	}
}

func (r *StatelessRunner) pollIOCOrder(ctx context.Context, symbol, orderID string) iocOrderPollResult {
	deadline := r.deps.Clock.Now().Add(iocOutcomePollTimeout)
	var result iocOrderPollResult

	for {
		got, err := r.deps.Client.GetOrder(ctx, symbol, orderID)
		if err != nil {
			result.reason = err.Error()
		} else if got != nil {
			result.capture(got)
			if exchange.IsTerminalOrderState(got.State) {
				break
			}
		}

		if !r.deps.Clock.Now().Before(deadline) {
			break
		}
		if err := r.deps.Clock.Sleep(ctx, iocOutcomePollInterval); err != nil {
			result.reason = err.Error()
			break
		}
	}

	return result
}

func (r *iocOrderPollResult) capture(order *exchange.OrderInfo) {
	r.order = order
	r.lastState = order.State
	r.dealVol = order.DealVol
	r.dealAvgPrice = order.DealAvgPrice
}

func classifyIOCOutcome(order *exchange.OrderInfo, holdVol, dealVol float64, reason string) (string, string) {
	switch {
	case holdVol > 0 || dealVol > 0:
		if order != nil && order.State == exchange.OrderStatePartial {
			return IOCOutcomePartialFilled, ""
		}
		return IOCOutcomeFilled, ""
	case order != nil && order.State == exchange.OrderStateCanceled:
		return IOCOutcomeCanceledNoFill, reversionReasonIOCCanceledNoPosition
	case reason == "":
		return IOCOutcomeUnknown, reversionReasonIOCUnknownNoPosition
	default:
		return IOCOutcomeUnknown, reason
	}
}

func (r *StatelessRunner) handleIOCOutcomeChecked(ctx context.Context, evt IOCOutcomeCheckedEvent) error {
	switch evt.Outcome {
	case IOCOutcomeFilled, IOCOutcomePartialFilled:
		return r.scheduleTimeoutGuard(ctx, evt)
	case IOCOutcomeCanceledNoFill:
		r.abortAfter(ctx, evt.BaseReversionEvent, evt.Symbol, reversionReasonIOCCanceledNoPosition)
	default:
		r.abortAfter(ctx, evt.BaseReversionEvent, evt.Symbol, reversionReasonIOCUnknownNoPosition)
	}
	return nil
}

func (r *StatelessRunner) handleTPSLRequired(ctx context.Context, evt TPSLRequiredEvent) error {
	provider, ok := r.deps.Client.(exchange.TPSLProvider)
	if !ok {
		r.log.WarnContext(ctx, "Exchange client does not support PlaceTPSL", slog.String("symbol", evt.Symbol))
		return nil
	}

	req := exchange.TPSLRequest{
		Symbol:          evt.Symbol,
		PositionMode:    evt.PositionMode,
		Side:            evt.Side,
		TakeProfitPrice: evt.TakeProfitPrice,
		StopLossPrice:   evt.StopLossPrice,
		Volume:          evt.Volume,
	}

	r.log.InfoContext(ctx, "Placing immediate post-fill TP/SL trigger orders",
		slog.String("symbol", evt.Symbol),
		slog.Float64("tp", evt.TakeProfitPrice),
		slog.Float64("sl", evt.StopLossPrice),
		slog.Float64("vol", evt.Volume),
	)

	if err := provider.PlaceTPSL(ctx, req); err != nil {
		r.log.ErrorContext(ctx, "Failed to place post-fill TP/SL orders", slog.Any("error", err), slog.String("symbol", evt.Symbol))
		return nil
	}

	return nil
}
