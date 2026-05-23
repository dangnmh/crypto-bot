package reversion

import (
	"context"
	"log/slog"
	"time"

	"crypto-bot/internal/bots/funding/application/cycle"
	"crypto-bot/internal/bots/funding/application/events"
	"crypto-bot/internal/infrastructure/exchange"
	applogger "crypto-bot/pkg/logger"

	"github.com/ThreeDotsLabs/watermill/message"
)

func subscribeTimeoutGuard(ctx context.Context, rt *cycle.Runtime) {
	rt.Subscribe(ctx, events.TopicReversionIOCFired, func(msg *message.Message) {
		evt, err := cycle.Unmarshal[events.IOCFiredEvent](msg.Payload)
		if err != nil {
			applogger.WithCtx(ctx, rt.Log()).Error("Unmarshal IOCFiredEvent failed", slog.Any("error", err))
			return
		}
		if evt.OrderID == "" || evt.Error != "" {
			return
		}
		go handleTimeout(ctx, rt, evt)
	})
}

func handleTimeout(ctx context.Context, rt *cycle.Runtime, evt events.IOCFiredEvent) {
	timeout := time.Duration(rt.Config().FundingReversion.PostSettleTimeout)
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	settleTime := evt.SettleTime
	if settleTime.IsZero() {
		settleTime = rt.SettleTime()
	}

	startedAt := time.Now()
	applogger.WithCtx(ctx, rt.Log()).Info("Reversion timeout guard started",
		slog.String("orderID", evt.OrderID),
		slog.Duration("timeout", timeout),
	)

	target := startedAt.Add(timeout)
	if !settleTime.IsZero() {
		target = settleTime.Add(timeout)
	}
	if !rt.WaitUntil(ctx, target) {
		return
	}
	if rt.IsFlowTerminal(events.FlowReversion) {
		return
	}

	fill, filled := rt.ReversionFill()
	if filled {
		forceCloseTimedOutPosition(ctx, rt, fill, timeout, startedAt)
		return
	}

	cancelTimedOutOrder(ctx, rt, evt, timeout, startedAt)
}

func forceCloseTimedOutPosition(
	ctx context.Context,
	rt *cycle.Runtime,
	fill events.OrderFilledEvent,
	timeout time.Duration,
	startedAt time.Time,
) {
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	positionMode := rt.CandidateCopy().Config.ParsedPositionMode
	retries, err := forceClosePosition(closeCtx, rt, fill.Symbol, fill.CloseSide, fill.FillVol, positionMode)
	if err != nil {
		publishReversionCritical(closeCtx, rt, fill.Symbol, "critical_timeout_close_failed: "+err.Error())
		return
	}
	if !rt.TryMarkFlowTerminal(events.FlowReversion) {
		return
	}

	now := time.Now()
	reqID := rt.GetReqID()
	rt.RecordAndPublishCtx(ctx, reqID, events.TopicReversionTimeout, events.CycleTimeoutEvent{
		Flow:                events.FlowReversion,
		Symbol:              fill.Symbol,
		Timeout:             timeout,
		Reason:              "force_close",
		ForceCloseAttempted: true,
		ForceCloseSucceeded: true,
		CloseRetryCount:     retries,
	})
	rt.RecordAndPublishCtx(ctx, reqID, events.TopicReversionPositionClosed, events.PositionClosedEvent{
		Flow:            events.FlowReversion,
		Symbol:          fill.Symbol,
		CloseVol:        fill.FillVol,
		Reason:          "timeout_force_close",
		Method:          reversionMethodFallbackClose,
		HoldDurationMs:  now.Sub(startedAt).Milliseconds(),
		CloseRetryCount: retries,
	})
}

func cancelTimedOutOrder(
	ctx context.Context,
	rt *cycle.Runtime,
	evt events.IOCFiredEvent,
	timeout time.Duration,
	startedAt time.Time,
) {
	if evt.OrderType == exchange.OrderTypeIOC {
		publishNoFillTimeout(ctx, rt, evt, timeout, startedAt, 0)
		return
	}

	cancelCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	cancelRetries, err := rt.RetryWithBackoff(cancelCtx, reversionRetryCount, func() error {
		return rt.Deps().Client.CancelOrder(cancelCtx, evt.Symbol, evt.OrderID)
	})
	if err != nil {
		applogger.WithCtx(ctx, rt.Log()).Error("IOC cancel failed - canceling all open orders", slog.Any("error", err))
		allRetries, allErr := rt.RetryWithBackoff(cancelCtx, reversionRetryCount, func() error {
			return rt.Deps().Client.CancelAllOpenOrders(cancelCtx, evt.Symbol)
		})
		cancelRetries += allRetries
		if allErr != nil {
			publishReversionCritical(cancelCtx, rt, evt.Symbol, "critical_timeout_cancel_failed: "+allErr.Error())
			return
		}
	}
	publishNoFillTimeout(ctx, rt, evt, timeout, startedAt, cancelRetries)
}

func publishNoFillTimeout(
	ctx context.Context,
	rt *cycle.Runtime,
	evt events.IOCFiredEvent,
	timeout time.Duration,
	startedAt time.Time,
	cancelRetries int,
) {
	if !rt.TryMarkFlowTerminal(events.FlowReversion) {
		return
	}

	reqID := rt.GetReqID()
	rt.RecordAndPublishCtx(ctx, reqID, events.TopicReversionTimeout, events.CycleTimeoutEvent{
		Flow:            events.FlowReversion,
		Symbol:          evt.Symbol,
		Timeout:         timeout,
		Reason:          reversionReasonNoFill,
		CloseRetryCount: cancelRetries,
	})
	applogger.WithCtx(ctx, rt.Log()).Warn("Reversion order timed out without fill",
		slog.String("orderID", evt.OrderID),
		slog.Int("order_type", evt.OrderType),
		slog.Duration("elapsed", time.Since(startedAt)),
	)
}
