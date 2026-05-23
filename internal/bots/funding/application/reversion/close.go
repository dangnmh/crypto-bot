package reversion

import (
	"context"
	"log/slog"
	"time"

	"crypto-bot/internal/bots/funding/application/cycle"
	"crypto-bot/internal/bots/funding/application/events"
	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	applogger "crypto-bot/pkg/logger"
)

const reversionRetryCount = 3

func watchStaticCloseDeal(ctx context.Context, rt *cycle.Runtime, evt events.OrderFilledEvent) {
	timeout := time.Duration(rt.Config().FundingReversion.PostSettleTimeout)
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	rt.Deps().OrderNotifier.OnOrderDealBySymbolSide(ctx, evt.Symbol, evt.CloseSide.String(), timeout, func(deal exchange.PersonalOrderDeal) {
		if deal.Vol <= 0 {
			return
		}
		if !rt.TryMarkFlowTerminal(events.FlowReversion) {
			return
		}

		reqID := rt.GetReqID()
		rt.RecordAndPublishCtx(ctx, reqID, events.TopicReversionPositionClosed, events.PositionClosedEvent{
			Flow:       events.FlowReversion,
			Symbol:     evt.Symbol,
			ClosePrice: deal.Price,
			CloseVol:   deal.Vol,
			Reason:     staticExitReason(deal.Profit),
			Profit:     deal.Profit,
			Fee:        deal.Fee,
			Method:     "static_tp_sl",
		})
	})
}

func staticExitReason(profit float64) string {
	if profit >= 0 {
		return "tp"
	}
	return "sl"
}

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
