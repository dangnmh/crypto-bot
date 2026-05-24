package reversion

import (
	"context"
	"log/slog"

	"crypto-bot/internal/bots/funding/application/cycle"
	"crypto-bot/internal/bots/funding/application/events"
	shared "crypto-bot/internal/domain"
	applogger "crypto-bot/pkg/logger"
)

const reversionRetryCount = 3

func forceClosePosition(
	ctx context.Context,
	rt *cycle.Runtime,
	symbol string,
	closeSide shared.Side,
	vol float64,
	positionMode int,
) (int, error) {
	exactRetries, err := rt.RetryWithBackoff(ctx, reversionRetryCount, func() error {
		return rt.Deps().Client.ClosePosition(ctx, symbol, closeSide, vol, positionMode)
	})
	if err == nil {
		return exactRetries, nil
	}

	applogger.WithCtx(ctx, rt.Log()).Error("Exact-leg reversion close failed - fallback close all",
		slog.Any("error", err),
		slog.String("symbol", symbol),
		slog.Any("closeSide", closeSide),
		slog.Float64("vol", vol),
	)
	allRetries, allErr := rt.RetryWithBackoff(ctx, reversionRetryCount, func() error {
		return rt.Deps().Client.CloseAllPositions(ctx, symbol)
	})
	exactRetries += allRetries
	if allErr != nil {
		return exactRetries, allErr
	}
	return exactRetries, nil
}

func publishReversionCritical(ctx context.Context, rt *cycle.Runtime, symbol, reason string) {
	if !rt.TryMarkFlowTerminal(events.FlowReversion) {
		return
	}
	reqID := rt.GetReqID()
	rt.RecordAndPublishCtx(ctx, reqID, events.TopicReversionError, events.CycleErrorEvent{
		Flow:   events.FlowReversion,
		Symbol: symbol,
		Error:  reason,
	})
	rt.RecordAndPublishCtx(ctx, reqID, events.TopicReversionAbort, events.CycleAbortEvent{
		Flow:   events.FlowReversion,
		Symbol: symbol,
		Reason: reason,
	})
}
