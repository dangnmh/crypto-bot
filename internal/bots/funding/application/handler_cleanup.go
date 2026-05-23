package application

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"crypto-bot/internal/bots/funding/application/events"
	applogger "crypto-bot/pkg/logger"

	"github.com/ThreeDotsLabs/watermill/message"
)

func (o *CycleOrchestrator) subscribeCleanup(ctx context.Context, done chan struct{}) {
	closedTopics := []string{events.TopicReversionPositionClosed}
	timeoutTopics := []string{events.TopicReversionTimeout}
	abortTopics := []string{events.TopicReversionAbort, events.TopicTrapAbort}

	cleanup := o.makeCleanupFn(ctx, done)

	for _, topic := range closedTopics {
		o.watchTerminalEvent(ctx, topic, func(msg *message.Message) {
			evt, parseErr := unmarshal[events.PositionClosedEvent](msg.Payload)
			flow := terminalFlow(topic)
			if parseErr == nil {
				flow = evt.Flow
				cleanup(topic, flow, evt.Reason)
			} else {
				cleanup(topic, flow, "position_closed")
			}
		})
	}

	for _, topic := range timeoutTopics {
		o.watchTerminalEvent(ctx, topic, func(msg *message.Message) {
			flow := terminalFlow(topic)
			reason := "timeout"
			if evt, parseErr := unmarshal[events.CycleTimeoutEvent](msg.Payload); parseErr == nil {
				flow = evt.Flow
				if evt.Reason != "" {
					reason = evt.Reason
				}
			}
			cleanup(topic, flow, reason)
		})
	}

	for _, topic := range abortTopics {
		o.watchTerminalEvent(ctx, topic, func(msg *message.Message) {
			evt, parseErr := unmarshal[events.CycleAbortEvent](msg.Payload)
			flow := terminalFlow(topic)
			if parseErr == nil {
				flow = evt.Flow
			}
			cleanup(topic, flow, "abort")
		})
	}
}

func (o *CycleOrchestrator) watchTerminalEvent(
	ctx context.Context,
	topic string,
	handler func(*message.Message),
) {
	o.rt.Subscribe(ctx, topic, handler)
}

func (o *CycleOrchestrator) makeCleanupFn(ctx context.Context, done chan struct{}) func(string, string, string) {
	var once sync.Once
	return func(topic, flow, reason string) {
		once.Do(func() {
			startedAt := time.Now()
			applogger.WithCtx(ctx, o.rt.Log()).Info("Cleanup", slog.String("reason", reason), slog.String("topic", topic), slog.String("flow", flow))
			reqID := o.rt.GetReqID()
			o.rt.RecordAndPublishCtx(ctx, reqID, events.TopicCleanupStarted, events.CleanupStartedEvent{
				TerminalFlow: flow,
				TerminalType: topic,
				Reason:       reason,
				StartedAt:    startedAt,
			})
			if flow == events.FlowReversion {
				o.settleOpenTrapBeforeCycleCleanup(ctx)
			}
			o.rt.UnsubscribeAll(ctx)
			unsubscribed := true
			o.rt.StopExcursionPriceStream()

			excursionFinalized := o.rt.FinalizeExcursion(ctx, o.rt.GetReqID())
			completedAt := time.Now()
			o.rt.RecordAndPublishCtx(ctx, reqID, events.TopicCleanupCompleted, events.CleanupCompletedEvent{
				TerminalFlow:       flow,
				TerminalType:       topic,
				Reason:             reason,
				StartedAt:          startedAt,
				CompletedAt:        completedAt,
				Unsubscribed:       unsubscribed,
				ExcursionFinalized: excursionFinalized,
			})
			o.rt.RecordAndPublishCtx(ctx, reqID, events.TopicCycleCompleted, events.CycleCompletedEvent{
				Reason: reason,
			})
			o.rt.PublishFinalPnLCtx(ctx, reqID)

			select {
			case done <- struct{}{}:
			default:
			}
		})
	}
}

// Manual Intervention Runbook:
// ========================================
// When a symbol gets disabled due to critical close failure:
//
// 1. Check the cycle logs and event timeline for this symbol/settle_time.
//
// 2. Look for these error indicators in the journal:
//    - abort_reason containing "critical_close_failed"
//    - abort_reason containing "critical_timeout_close_failed"
//    - abort_reason containing "critical_trap_cancel_failed"
//    - abort_reason containing "critical_trap_close_failed"
//
// 3. Manual recovery steps:
//    a) Verify symbol position status on exchange manually
//    b) If position still open, close it manually via exchange UI/API
//    c) Remove symbol from in-memory disabled map (restart bot if needed)
//    d) Investigate root cause: exchange API issue, network timeout, position state mismatch
//
// 4. Prevention:
//    - Increase closeOpTimeout if timeouts are frequent
//    - Reduce position size if liquidity is insufficient
//    - Enable Hedge mode to avoid position conflicts
//    - Monitor latency_rtt_ms in journal - high latency may cause timeouts

func (o *CycleOrchestrator) settleOpenTrapBeforeCycleCleanup(ctx context.Context) {
	order, hasOrder, fill, hasFill, terminal := o.rt.TrapSnapshot()
	if !hasOrder || terminal {
		return
	}

	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	if !hasFill {
		o.cancelOpenTrapOrder(cleanupCtx, order)
		return
	}

	o.closeFilledTrapPosition(cleanupCtx, fill)
}

func (o *CycleOrchestrator) cancelOpenTrapOrder(ctx context.Context, order events.TrapFiredEvent) {
	retries, err := o.rt.RetryWithBackoff(ctx, 3, func() error {
		return o.rt.Deps().Client.CancelOrder(ctx, order.Symbol, order.OrderID)
	})
	if err != nil {
		applogger.WithCtx(ctx, o.rt.Log()).Error("Trap cleanup cancel failed - canceling all open orders", slog.Any("error", err))
		allRetries, allErr := o.rt.RetryWithBackoff(ctx, 3, func() error {
			return o.rt.Deps().Client.CancelAllOpenOrders(ctx, order.Symbol)
		})
		retries += allRetries
		if allErr != nil {
			o.publishTrapCleanupCritical(ctx, order.Symbol, "critical_trap_cancel_failed: "+allErr.Error())
			return
		}
	}

	o.rt.MarkTrapTerminal()
	reqID := o.rt.GetReqID()
	o.rt.RecordAndPublishCtx(ctx, reqID, events.TopicTrapTimedOut, events.TimeoutFiredEvent{
		Flow:            events.FlowTrap,
		Symbol:          order.Symbol,
		FiredAt:         time.Now(),
		CloseRetryCount: retries,
	})
	o.rt.RecordAndPublishCtx(ctx, reqID, events.TopicTrapTimeout, events.CycleTimeoutEvent{
		Flow:            events.FlowTrap,
		Symbol:          order.Symbol,
		Reason:          "reversion_cleanup_cancel",
		CloseRetryCount: retries,
	})
}

func (o *CycleOrchestrator) closeFilledTrapPosition(ctx context.Context, fill events.OrderFilledEvent) {
	positionMode := o.rt.CandidateCopy().Config.ParsedPositionMode
	retries, err := o.rt.RetryWithBackoff(ctx, 3, func() error {
		return o.rt.Deps().Client.ClosePosition(ctx, fill.Symbol, fill.CloseSide, fill.FillVol, positionMode)
	})
	if err != nil {
		applogger.WithCtx(ctx, o.rt.Log()).Error("Trap cleanup exact close failed - closing all positions", slog.Any("error", err))
		allRetries, allErr := o.rt.RetryWithBackoff(ctx, 3, func() error {
			return o.rt.Deps().Client.CloseAllPositions(ctx, fill.Symbol)
		})
		retries += allRetries
		if allErr != nil {
			o.publishTrapCleanupCritical(ctx, fill.Symbol, "critical_trap_close_failed: "+allErr.Error())
			return
		}
	}

	o.rt.MarkTrapTerminal()
	reqID := o.rt.GetReqID()
	o.rt.RecordAndPublishCtx(ctx, reqID, events.TopicTrapPositionClosed, events.PositionClosedEvent{
		Flow:            events.FlowTrap,
		Symbol:          fill.Symbol,
		CloseVol:        fill.FillVol,
		Reason:          "reversion_cleanup_close",
		Method:          "fallback_close",
		CloseRetryCount: retries,
	})
}

func (o *CycleOrchestrator) publishTrapCleanupCritical(ctx context.Context, symbol, reason string) {
	o.rt.MarkTrapTerminal()
	reqID := o.rt.GetReqID()
	o.rt.RecordAndPublishCtx(ctx, reqID, events.TopicTrapError, events.CycleErrorEvent{
		Flow:   events.FlowTrap,
		Symbol: symbol,
		Error:  reason,
	})
	o.rt.RecordAndPublishCtx(ctx, reqID, events.TopicTrapAbort, events.CycleAbortEvent{
		Flow:   events.FlowTrap,
		Symbol: symbol,
		Reason: reason,
	})
}

func terminalFlow(topic string) string {
	switch topic {
	case events.TopicTrapPositionClosed, events.TopicTrapTimeout, events.TopicTrapAbort:
		return events.FlowTrap
	case events.TopicReversionPositionClosed, events.TopicReversionTimeout, events.TopicReversionAbort:
		return events.FlowReversion
	default:
		return ""
	}
}

func (o *CycleOrchestrator) subscribeEventLog(ctx context.Context) {
	topics := []string{
		events.TopicScanStart,
		events.TopicScanCandidateFound,
		events.TopicScanAbort,
		events.TopicReversionCandidate,
		events.TopicReversionArmed,
		events.TopicReversionWaitComplete,
		events.TopicReversionConfirmed,
		events.TopicReversionIOCFired,
		events.TopicReversionOrderFilled,
		events.TopicReversionPositionClosed,
		events.TopicReversionTimeout,
		events.TopicReversionAbort,
		events.TopicReversionError,
		events.TopicTrapCandidate,
		events.TopicTrapOBWallFound,
		events.TopicTrapSkipped,
		events.TopicTrapOrderPlaced,
		events.TopicTrapOrderFilled,
		events.TopicTrapTrailingPlaced,
		events.TopicTrapPositionClosed,
		events.TopicTrapTimeout,
		events.TopicTrapAbort,
		events.TopicTrapError,
	}

	for _, topic := range topics {
		o.rt.Subscribe(ctx, topic, func(msg *message.Message) {
			applogger.WithCtx(ctx, o.rt.Log()).Info("Event", slog.String("topic", topic), slog.String("msg_id", msg.UUID))
		})
	}
}
