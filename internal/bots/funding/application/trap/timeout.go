package trap

import (
	"context"
	"log/slog"
	"time"

	"crypto-bot/internal/bots/funding/application/cycle"
	"crypto-bot/internal/bots/funding/application/events"
	"crypto-bot/internal/bots/funding/domain"

	"github.com/ThreeDotsLabs/watermill/message"
)

func subscribeTrapOrderTimeoutGuard(ctx context.Context, rt *cycle.Runtime) {
	rt.Subscribe(ctx, events.TopicTrapOrderPlaced, func(msg *message.Message) {
		evt, err := cycle.Unmarshal[events.TrapFiredEvent](msg.Payload)
		if err != nil {
			rt.Log().Error("Unmarshal TrapFiredEvent failed", slog.Any("error", err))
			return
		}
		go handleTrapOrderTimeout(ctx, rt, evt)
	})
}

func handleTrapOrderTimeout(ctx context.Context, rt *cycle.Runtime, evt events.TrapFiredEvent) {
	timeout := time.Duration(rt.Config().FundingTrap.PostSettleTimeout)
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	startedAt := time.Now()
	reqID := rt.GetReqID()
	rt.RecordAndPublish(reqID, events.TopicTrapTimeoutStarted, events.TimeoutStartedEvent{
		Flow:       events.FlowTrap,
		Symbol:     evt.Symbol,
		DurationMs: timeout.Milliseconds(),
		StartedAt:  startedAt,
	})

	rt.Log().Info("Trap order timeout guard started",
		slog.String("orderID", evt.OrderID),
		slog.Duration("timeout", timeout),
	)

	if err := rt.Deps().Clock.Sleep(ctx, timeout); err != nil {
		return
	}

	firedAt := time.Now()
	_, _, fill, hasFill, terminal := rt.TrapSnapshot()
	if terminal {
		return
	}
	if hasFill {
		closeTimedOutTrapPosition(ctx, rt, fill, timeout, startedAt, firedAt)
		return
	}

	cancelCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	cancelRetries, err := rt.RetryWithBackoff(cancelCtx, trapRetryCount, func() error {
		return rt.Deps().Client.CancelOrder(cancelCtx, evt.Symbol, evt.OrderID)
	})
	if err != nil {
		rt.Log().Error("Trap order cancel failed - canceling all open orders", slog.Any("error", err))
		allRetries, allErr := rt.RetryWithBackoff(cancelCtx, trapRetryCount, func() error {
			return rt.Deps().Client.CancelAllOpenOrders(cancelCtx, evt.Symbol)
		})
		cancelRetries += allRetries
		if allErr != nil {
			reason := "critical_trap_cancel_failed: " + allErr.Error()
			reqID := rt.GetReqID()
			rt.RecordAndPublish(reqID, events.TopicTrapTimedOut, events.TimeoutFiredEvent{
				Flow:            events.FlowTrap,
				Symbol:          evt.Symbol,
				DurationMs:      timeout.Milliseconds(),
				StartedAt:       startedAt,
				FiredAt:         firedAt,
				CloseRetryCount: cancelRetries,
				Error:           allErr.Error(),
			})
			rt.RecordAndPublish(reqID, events.TopicTrapError, events.CycleErrorEvent{
				Flow:   events.FlowTrap,
				Symbol: evt.Symbol,
				Error:  reason,
			})
			rt.RecordAndPublish(reqID, events.TopicTrapAbort, events.CycleAbortEvent{
				Flow:   events.FlowTrap,
				Symbol: evt.Symbol,
				Reason: reason,
			})
			rt.MarkTrapTerminal()
			rt.Log().Error("CRITICAL trap cancel failed",
				slog.Any("error", allErr),
				slog.String("symbol", evt.Symbol),
			)
			return
		}
	}

	if !rt.TryMarkFlowTerminal(events.FlowTrap) {
		return
	}
	reqID = rt.GetReqID()
	rt.MarkTrapTerminal()
	rt.RecordAndPublish(reqID, events.TopicTrapTimedOut, events.TimeoutFiredEvent{
		Flow:            events.FlowTrap,
		Symbol:          evt.Symbol,
		DurationMs:      timeout.Milliseconds(),
		StartedAt:       startedAt,
		FiredAt:         firedAt,
		CloseRetryCount: cancelRetries,
	})
	rt.RecordAndPublish(reqID, events.TopicTrapTimeout, events.CycleTimeoutEvent{
		Flow:    events.FlowTrap,
		Symbol:  evt.Symbol,
		Timeout: timeout,
	})
}

func closeTimedOutTrapPosition(
	ctx context.Context,
	rt *cycle.Runtime,
	fill events.OrderFilledEvent,
	timeout time.Duration,
	startedAt time.Time,
	firedAt time.Time,
) {
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	positionMode := rt.CandidateCopy().Config.ParsedPositionMode
	retries, err := rt.RetryWithBackoff(closeCtx, trapRetryCount, func() error {
		return rt.Deps().Client.ClosePosition(closeCtx, fill.Symbol, fill.CloseSide, fill.FillVol, positionMode)
	})
	if err != nil {
		rt.Log().Error("Trap timeout exact close failed - closing all positions", slog.Any("error", err))
		allRetries, allErr := rt.RetryWithBackoff(closeCtx, trapRetryCount, func() error {
			return rt.Deps().Client.CloseAllPositions(closeCtx, fill.Symbol)
		})
		retries += allRetries
		if allErr != nil {
			reason := "critical_trap_close_failed: " + allErr.Error()
			reqID := rt.GetReqID()
			rt.RecordAndPublish(reqID, events.TopicTrapTimedOut, events.TimeoutFiredEvent{
				Flow:            events.FlowTrap,
				Symbol:          fill.Symbol,
				DurationMs:      timeout.Milliseconds(),
				StartedAt:       startedAt,
				FiredAt:         firedAt,
				CloseRetryCount: retries,
				Error:           allErr.Error(),
			})
			rt.RecordAndPublish(reqID, events.TopicTrapError, events.CycleErrorEvent{
				Flow:   events.FlowTrap,
				Symbol: fill.Symbol,
				Error:  reason,
			})
			rt.RecordAndPublish(reqID, events.TopicTrapAbort, events.CycleAbortEvent{
				Flow:   events.FlowTrap,
				Symbol: fill.Symbol,
				Reason: reason,
			})
			rt.MarkTrapTerminal()
			return
		}
	}

	if !rt.TryMarkFlowTerminal(events.FlowTrap) {
		return
	}
	reqID := rt.GetReqID()
	rt.MarkTrapTerminal()
	rt.RecordAndPublish(reqID, events.TopicTrapTimedOut, events.TimeoutFiredEvent{
		Flow:            events.FlowTrap,
		Symbol:          fill.Symbol,
		DurationMs:      timeout.Milliseconds(),
		StartedAt:       startedAt,
		FiredAt:         firedAt,
		CloseRetryCount: retries,
	})
	rt.RecordAndPublish(reqID, events.TopicTrapPositionClosed, events.PositionClosedEvent{
		Flow:            events.FlowTrap,
		Symbol:          fill.Symbol,
		CloseVol:        fill.FillVol,
		Reason:          "timeout_force_close",
		Method:          trapMethodFallbackClose,
		CloseRetryCount: retries,
	})
	rt.RecordAndPublish(reqID, events.TopicTrapTimeout, events.CycleTimeoutEvent{
		Flow:                events.FlowTrap,
		Symbol:              fill.Symbol,
		Timeout:             timeout,
		Reason:              "force_close",
		ForceCloseAttempted: true,
		ForceCloseSucceeded: true,
		CloseRetryCount:     retries,
	})
}

func watchTrapBranchTerminal(ctx context.Context, rt *cycle.Runtime) {
	rt.Subscribe(ctx, events.TopicTrapPositionClosed, func(msg *message.Message) {
		if evt, parseErr := cycle.Unmarshal[events.PositionClosedEvent](msg.Payload); parseErr == nil && evt.Flow != events.FlowTrap {
			return
		}
		rt.TryMarkFlowTerminal(events.FlowTrap)
		rt.MarkTrapTerminal()
	})
	rt.Subscribe(ctx, events.TopicTrapTimeout, func(msg *message.Message) {
		if evt, parseErr := cycle.Unmarshal[events.CycleTimeoutEvent](msg.Payload); parseErr == nil && evt.Flow != events.FlowTrap {
			return
		}
		rt.TryMarkFlowTerminal(events.FlowTrap)
		rt.MarkTrapTerminal()
	})
}

func OutcomeTerminal(outcome domain.TrapOutcome) bool {
	switch outcome {
	case domain.TrapOutcomeClosed, domain.TrapOutcomeTimeout, domain.TrapOutcomeAborted, domain.TrapOutcomeSkipped:
		return true
	default:
		return false
	}
}

// IsTrapOutcomeTerminal is exported for use by other packages.
func IsTrapOutcomeTerminal(outcome domain.TrapOutcome) bool {
	return OutcomeTerminal(outcome)
}
