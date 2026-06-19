package reversion

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"

	"github.com/cenkalti/backoff/v4"
)

type iocOrderPollResult struct {
	order        *exchange.OrderInfo
	lastState    shared.OrderState
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
	_ = r.deps.Clock.Sleep(ctx, time.Second*2)
	poll := r.pollIOCOrder(ctx, evt.Symbol, evt.OrderID)

	holdVol, err := r.getHoldVolume(ctx, evt.Symbol)
	if err != nil {
		poll.reason = err.Error()
	}

	outcome, reason := classifyIOCOutcome(poll.order, holdVol, poll.dealVol, poll.reason)

	timeout := time.Duration(evt.Candidate.Config.FundingReversion.PostSettleTimeout)
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	return IOCOutcomeCheckedEvent{
		BaseReversionEvent: nextReversionBase(evt.BaseReversionEvent, evt.Symbol, r.deps.Clock.Now()),
		OrderState:         poll.lastState,
		DealVol:            poll.dealVol,
		DealAvgPrice:       poll.dealAvgPrice,
		HoldVol:            holdVol,
		Outcome:            outcome,
		Reason:             reason,
		CheckedAt:          r.deps.Clock.Now(),
		Timeout:            timeout,
		FundingRate:        evt.Candidate.FundingRate,
		VolUSDT24h:         evt.Candidate.AmountUSDT24,
	}
}

var errNotTerminal = errors.New("order state not terminal")

func (r *StatelessRunner) pollIOCOrder(ctx context.Context, symbol, orderID string) iocOrderPollResult {
	var result iocOrderPollResult

	bo := backoff.WithContext(
		backoff.WithMaxRetries(
			backoff.NewExponentialBackOff(
				backoff.WithInitialInterval(time.Second),
				backoff.WithMaxInterval(time.Second*2)),
			5),
		ctx,
	)

	err := backoff.Retry(func() error {
		got, err := r.deps.Client.GetOrder(ctx, symbol, orderID)
		if err != nil {
			result.reason = err.Error()
			return err
		}
		if got == nil {
			result.reason = "order not found"
			return fmt.Errorf("order not found")
		}

		result.capture(got)
		if !exchange.IsTerminalOrderState(got.State) {
			return errNotTerminal
		}
		return nil
	}, bo)

	if err != nil && !errors.Is(err, errNotTerminal) {
		result.reason = err.Error()
	}

	return result
}

func (r *iocOrderPollResult) capture(order *exchange.OrderInfo) {
	r.order = order
	r.lastState = order.State
	r.dealVol = order.DealVol
	r.dealAvgPrice = order.DealAvgPrice
}

func classifyIOCOutcome(order *exchange.OrderInfo, holdVol, dealVol float64, reason string) (IOCOutcome, ReversionReason) {
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
		return IOCOutcomeUnknown, ReversionReason(reason)
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
