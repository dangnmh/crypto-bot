package reversion

import (
	"context"
	"log/slog"
	"time"
)

func (r *StatelessRunner) scheduleTimeoutGuard(ctx context.Context, evt IOCOutcomeCheckedEvent) error {
	return r.scheduleTimeoutGuardAfter(ctx, evt.BaseReversionEvent, evt.IOCEvent)
}

func (r *StatelessRunner) scheduleTimeoutGuardAfter(ctx context.Context, prev BaseReversionEvent, evt IOCSubmittedEvent) error {
	timeout := time.Duration(evt.Candidate.Config.FundingReversion.PostSettleTimeout)
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	guardEvt := TimeoutGuardScheduledEvent{
		BaseReversionEvent: nextReversionBase(prev, evt.Symbol, r.deps.Clock.Now()),
		IOCEvent:           evt,
		Timeout:            timeout,
		StartedAt:          r.deps.Clock.Now(),
	}

	return r.publishEvent(ctx, TopicReversionTimeoutGuardScheduled, guardEvt)
}

func (r *StatelessRunner) handleTimeoutGuardScheduled(ctx context.Context, evt TimeoutGuardScheduledEvent) error {
	go func() {
		_ = r.waitTimeoutDeadline(ctx, evt)
	}()
	return nil
}

func (r *StatelessRunner) timeoutGuard(ctx context.Context, firedEvt IOCSubmittedEvent) error {
	return r.scheduleTimeoutGuardAfter(ctx, firedEvt.BaseReversionEvent, firedEvt)
}

func (r *StatelessRunner) waitTimeoutDeadline(ctx context.Context, evt TimeoutGuardScheduledEvent) error {
	timeout := evt.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	settleTime := evt.IOCEvent.SettleTime
	r.log.InfoContext(ctx, "Reversion timeout guard started",
		slog.String("symbol", evt.Symbol),
		slog.Duration("timeout", timeout),
	)

	target := evt.StartedAt.Add(timeout)
	if !settleTime.IsZero() {
		target = settleTime.Add(timeout)
	}

	if !r.WaitUntil(ctx, evt.Symbol, target) {
		return ctx.Err()
	}

	holdVol, err := r.getHoldVolume(ctx, evt.Symbol)
	errText := ""
	if err != nil {
		errText = err.Error()
		r.log.ErrorContext(ctx, "Timeout guard failed to query position", slog.String("symbol", evt.Symbol), slog.Any("error", err))
	}

	next := TimeoutPositionCheckedEvent{
		BaseReversionEvent: nextReversionBase(evt.BaseReversionEvent, evt.Symbol, r.deps.Clock.Now()),
		IOCEvent:           evt.IOCEvent,
		Timeout:            timeout,
		StartedAt:          evt.StartedAt,
		HoldVol:            holdVol,
		Error:              errText,
	}
	return r.publishEvent(ctx, TopicReversionTimeoutPositionChecked, next)
}

func (r *StatelessRunner) handleTimeoutPositionChecked(ctx context.Context, evt TimeoutPositionCheckedEvent) error {
	if evt.Error != "" {
		r.abortAfter(ctx, evt.BaseReversionEvent, evt.Symbol, ReversionReason("position query failed: "+evt.Error))
		return nil
	}

	if evt.HoldVol <= 0 {
		timeoutEvt := TimeoutEvent{
			BaseReversionEvent:  nextReversionBase(evt.BaseReversionEvent, evt.Symbol, r.deps.Clock.Now()),
			Timeout:             evt.Timeout,
			Reason:              reversionReasonNoFill,
			ForceCloseAttempted: false,
		}
		return r.publishEvent(ctx, TopicReversionTimeout, timeoutEvt)
	}

	initEvt := ForceCloseInitiatedEvent{
		BaseReversionEvent: nextNotifyReversionBase(evt.BaseReversionEvent, evt.Symbol, r.deps.Clock.Now()),
		IOCEvent:           evt.IOCEvent,
		Timeout:            evt.Timeout,
		StartedAt:          evt.StartedAt,
		HoldVol:            evt.HoldVol,
		TimeoutSec:         evt.Timeout.Seconds(),
	}
	return r.publishEvent(ctx, TopicReversionForceCloseInitiated, initEvt)
}

func (r *StatelessRunner) forceCloseTimedOutPosition(
	ctx context.Context,
	prev BaseReversionEvent,
	firedEvt IOCSubmittedEvent,
	holdVol float64,
	timeout time.Duration,
	startedAt time.Time,
) {
	symbol := firedEvt.Symbol

	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	retries, err := r.forceClosePosition(closeCtx, symbol, 3)
	errText := ""
	if err != nil {
		errText = err.Error()
	}

	completedEvt := ForceCloseCompletedEvent{
		BaseReversionEvent: nextReversionBase(prev, symbol, r.deps.Clock.Now()),
		IOCEvent:           firedEvt,
		Timeout:            timeout,
		StartedAt:          startedAt,
		HoldVol:            holdVol,
		CloseRetryCount:    retries,
		Succeeded:          err == nil,
		Error:              errText,
	}
	_ = r.publishEvent(ctx, TopicReversionForceCloseCompleted, completedEvt)
}

func (r *StatelessRunner) handleForceCloseInitiated(ctx context.Context, evt ForceCloseInitiatedEvent) error {
	r.forceCloseTimedOutPosition(ctx, evt.BaseReversionEvent, evt.IOCEvent, evt.HoldVol, evt.Timeout, evt.StartedAt)
	return nil
}

func (r *StatelessRunner) handleForceCloseCompleted(ctx context.Context, evt ForceCloseCompletedEvent) error {
	if !evt.Succeeded {
		r.publishReversionCritical(ctx, evt.BaseReversionEvent, evt.Symbol, "critical_timeout_close_failed: "+evt.Error)
		return nil
	}

	now := r.deps.Clock.Now()
	timeoutEvt := TimeoutEvent{
		BaseReversionEvent:  nextReversionBase(evt.BaseReversionEvent, evt.Symbol, now),
		Timeout:             evt.Timeout,
		Reason:              ReversionReason("force_close"),
		ForceCloseAttempted: true,
		ForceCloseSucceeded: true,
		CloseRetryCount:     evt.CloseRetryCount,
		HoldVol:             evt.HoldVol,
		HoldDurationMs:      now.Sub(evt.StartedAt).Milliseconds(),
		Direction:           evt.IOCEvent.Side,
	}
	return r.publishEvent(ctx, TopicReversionTimeout, timeoutEvt)
}

func (r *StatelessRunner) handleTimeout(ctx context.Context, evt TimeoutEvent) error {
	if !evt.ForceCloseSucceeded {
		r.abortAfter(ctx, evt.BaseReversionEvent, evt.Symbol, evt.Reason)
		return nil
	}

	closeEvt := PositionClosedEvent{
		BaseReversionEvent: nextReversionBase(evt.BaseReversionEvent, evt.Symbol, r.deps.Clock.Now()),
		CloseVol:           evt.HoldVol,
		Reason:             "timeout_force_close",
		Method:             string(reversionMethodFallbackClose),
		HoldDurationMs:     evt.HoldDurationMs,
		CloseRetryCount:    evt.CloseRetryCount,
		Direction:          evt.Direction,
	}
	return r.publishEvent(ctx, TopicReversionPositionClosed, closeEvt)
}

func (r *StatelessRunner) forceClosePosition(
	ctx context.Context,
	symbol string,
	maxRetries int,
) (int, error) {
	retries, err := r.RetryWithBackoff(ctx, maxRetries, func() error {
		return r.deps.Client.CloseAllPositions(ctx, symbol)
	})
	return retries, err
}

func (r *StatelessRunner) publishReversionCritical(ctx context.Context, prev BaseReversionEvent, symbol, reason string) {
	errEvt := ErrorEvent{
		BaseReversionEvent: nextNotifyReversionBase(prev, symbol, r.deps.Clock.Now()),
		Error:              reason,
	}
	_ = r.publishEvent(ctx, TopicReversionError, errEvt)

	errBase := errEvt.BaseReversionEvent
	errBase.Topic = TopicReversionError
	abortEvt := AbortEvent{
		BaseReversionEvent: nextReversionBase(errBase, symbol, r.deps.Clock.Now()),
		Reason:             ReversionReason(reason),
	}
	_ = r.publishEvent(ctx, TopicReversionAbort, abortEvt)
}

func (r *StatelessRunner) getHoldVolume(ctx context.Context, symbol string) (float64, error) {
	positions, err := r.deps.Client.GetOpenPositions(ctx, symbol)
	if err != nil {
		return 0, err
	}
	var totalVol float64
	for i := range positions {
		totalVol += positions[i].HoldVol
	}
	return totalVol, nil
}
