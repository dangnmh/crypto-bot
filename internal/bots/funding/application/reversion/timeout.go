package reversion

import (
	"context"
	"log/slog"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	applogger "crypto-bot/pkg/logger"
)

func (r *StatelessRunner) handleIOCFired(ctx context.Context, evt IOCFiredEvent) error {
	if evt.Error != "" {
		return nil
	}

	cfg, ok := r.getSymbolConfig(evt.Symbol)
	if !ok {
		r.log.Error("Symbol config not found for timeout handler", slog.String("symbol", evt.Symbol))
		return nil
	}

	timeout := time.Duration(cfg.FundingReversion.PostSettleTimeout)
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	// 1. Subscribe watcher to personal position updates
	r.deps.OrderNotifier.OnPositionUpdate(ctx, evt.Symbol, timeout*2, func(pos exchange.PersonalPositionUpdate) {
		r.handlePositionUpdate(ctx, pos, evt.ReqID)
	})

	// 2. Publish event to trigger the asynchronous timeout guard
	checkEvt := CheckTimeoutEvent{
		BaseReversionEvent: BaseReversionEvent{
			Flow:      FlowReversion,
			ReqID:     evt.ReqID,
			Symbol:    evt.Symbol,
			Timestamp: r.deps.Clock.Now(),
		},
		IOCEvent: evt,
	}

	return r.publishEvent(ctx, TopicReversionCheckTimeout, checkEvt)
}

func (r *StatelessRunner) handleCheckTimeout(ctx context.Context, evt CheckTimeoutEvent) error {
	// Start timeout guard in the background to avoid blocking subscription loop
	go func() {
		_ = r.timeoutGuard(ctx, evt.IOCEvent)
	}()
	return nil
}

func (r *StatelessRunner) timeoutGuard(ctx context.Context, firedEvt IOCFiredEvent) error {
	cfg, ok := r.getSymbolConfig(firedEvt.Symbol)
	if !ok {
		return nil
	}

	timeout := time.Duration(cfg.FundingReversion.PostSettleTimeout)
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	settleTime := firedEvt.SettleTime
	startedAt := time.Now()
	applogger.WithCtx(ctx, r.log).Info("Reversion timeout guard started",
		slog.String("symbol", firedEvt.Symbol),
		slog.Duration("timeout", timeout),
	)

	target := startedAt.Add(timeout)
	if !settleTime.IsZero() {
		target = settleTime.Add(timeout)
	}

	if !r.WaitUntil(ctx, firedEvt.Symbol, target) {
		return ctx.Err()
	}

	// Dynamic exchange query to verify current position ground truth
	holdVol, err := r.getHoldVolume(ctx, firedEvt.Symbol)
	if err != nil {
		r.log.Error("Timeout guard failed to query position", slog.String("symbol", firedEvt.Symbol), slog.Any("error", err))
		r.abort(ctx, firedEvt.Symbol, firedEvt.ReqID, "position query failed: "+err.Error())
		return nil
	}

	if holdVol > 0 {
		r.forceCloseTimedOutPosition(ctx, firedEvt, holdVol, timeout, startedAt)
	} else {
		// Timeout without any open position, check if we need to publish timeout event
		evt := TimeoutEvent{
			BaseReversionEvent: BaseReversionEvent{
				Flow:      FlowReversion,
				ReqID:     firedEvt.ReqID,
				Symbol:    firedEvt.Symbol,
				Timestamp: r.deps.Clock.Now(),
			},
			Timeout:             timeout,
			Reason:              reversionReasonNoFill,
			ForceCloseAttempted: false,
		}
		_ = r.publishEvent(ctx, TopicReversionTimeout, evt)
		r.abort(ctx, firedEvt.Symbol, firedEvt.ReqID, reversionReasonNoFill)
	}

	return nil
}

func (r *StatelessRunner) forceCloseTimedOutPosition(
	ctx context.Context,
	firedEvt IOCFiredEvent,
	holdVol float64,
	timeout time.Duration,
	startedAt time.Time,
) {
	symbol := firedEvt.Symbol

	initEvt := ForceCloseInitiatedEvent{
		BaseReversionEvent: BaseReversionEvent{
			Flow:       FlowReversion,
			ReqID:      firedEvt.ReqID,
			Symbol:     symbol,
			Timestamp:  r.deps.Clock.Now(),
			SendNotify: true,
		},
		HoldVol:    holdVol,
		TimeoutSec: timeout.Seconds(),
	}
	_ = r.publishEvent(ctx, TopicReversionForceCloseInitiated, initEvt)

	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	retries, err := r.forceClosePosition(closeCtx, symbol, 3)
	if err != nil {
		r.publishReversionCritical(closeCtx, symbol, firedEvt.ReqID, "critical_timeout_close_failed: "+err.Error())
		return
	}

	now := time.Now()
	timeoutEvt := TimeoutEvent{
		BaseReversionEvent: BaseReversionEvent{
			Flow:      FlowReversion,
			ReqID:     firedEvt.ReqID,
			Symbol:    symbol,
			Timestamp: r.deps.Clock.Now(),
		},
		Timeout:             timeout,
		Reason:              "force_close",
		ForceCloseAttempted: true,
		ForceCloseSucceeded: true,
		CloseRetryCount:     retries,
	}
	_ = r.publishEvent(ctx, TopicReversionTimeout, timeoutEvt)

	closeEvt := PositionClosedEvent{
		BaseReversionEvent: BaseReversionEvent{
			Flow:      FlowReversion,
			ReqID:     firedEvt.ReqID,
			Symbol:    symbol,
			Timestamp: r.deps.Clock.Now(),
		},
		CloseVol:        holdVol,
		Reason:          "timeout_force_close",
		Method:          reversionMethodFallbackClose,
		HoldDurationMs:  now.Sub(startedAt).Milliseconds(),
		CloseRetryCount: retries,
		Direction:       firedEvt.Side,
	}
	_ = r.publishEvent(ctx, TopicReversionPositionClosed, closeEvt)
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

func (r *StatelessRunner) publishReversionCritical(ctx context.Context, symbol, reqID, reason string) {
	errEvt := ErrorEvent{
		BaseReversionEvent: BaseReversionEvent{
			Flow:       FlowReversion,
			ReqID:      reqID,
			Symbol:     symbol,
			Timestamp:  r.deps.Clock.Now(),
			SendNotify: true,
		},
		Error: reason,
	}
	_ = r.publishEvent(ctx, TopicReversionError, errEvt)

	abortEvt := AbortEvent{
		BaseReversionEvent: BaseReversionEvent{
			Flow:       FlowReversion,
			ReqID:      reqID,
			Symbol:     symbol,
			Timestamp:  r.deps.Clock.Now(),
			SendNotify: false,
		},
		Reason: reason,
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
