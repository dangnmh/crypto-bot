package application

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"crypto-bot/internal/bots/funding/application/events"
	"crypto-bot/internal/bots/funding/application/trap"
	"crypto-bot/internal/bots/funding/domain"
	shared "crypto-bot/internal/domain"

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
			if evt, parseErr := unmarshal[events.CycleTimeoutEvent](msg.Payload); parseErr == nil {
				flow = evt.Flow
			}
			cleanup(topic, flow, "timeout")
		})
	}

	for _, topic := range abortTopics {
		o.watchTerminalEvent(ctx, topic, func(msg *message.Message) {
			evt, parseErr := unmarshal[events.CycleAbortEvent](msg.Payload)
			flow := terminalFlow(topic)
			if parseErr == nil {
				flow = evt.Flow
				o.rt.MutateRecorder(func(b *domain.CycleRecordBuilder) {
					b.AbortReason = evt.Reason
					b.AbortFlow = flow
					b.AbortTopic = topic
				})
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
			o.rt.Log().Info("🧹 Cleanup", slog.String("reason", reason), slog.String("topic", topic), slog.String("flow", flow))
			if flow == events.FlowReversion {
				o.settleOpenTrapBeforeCycleCleanup(ctx)
			}
			o.rt.UnsubscribeAll(ctx)
			unsubscribed := true
			o.rt.StopExcursionPriceStream()

			o.rt.MutateRecorder(func(b *domain.CycleRecordBuilder) {
				b.ExitReason = reason
				b.ExitTime = time.Now()
				switch topic {
				case events.TopicTrapPositionClosed:
					b.TrapOutcome = domain.TrapOutcomeClosed
				case events.TopicTrapTimeout:
					b.TrapOutcome = domain.TrapOutcomeTimeout
				case events.TopicTrapAbort:
					b.TrapOutcome = domain.TrapOutcomeAborted
				}
			})

			excursionFinalized := o.rt.FinalizeExcursion(ctx)
			completedAt := time.Now()
			o.rt.MutateRecorder(func(b *domain.CycleRecordBuilder) {
				b.Cleanup = domain.CleanupSnapshot{
					TerminalFlow:       flow,
					TerminalTopic:      topic,
					Reason:             reason,
					StartedAt:          startedAt,
					CompletedAt:        completedAt,
					Unsubscribed:       unsubscribed,
					ExcursionFinalized: excursionFinalized,
				}
			})

			select {
			case done <- struct{}{}:
			default:
			}
		})
	}
}

type trapCleanupAction int

const (
	trapCleanupNone trapCleanupAction = iota
	trapCleanupCancelOrder
	trapCleanupClosePosition
)

func (o *CycleOrchestrator) settleOpenTrapBeforeCycleCleanup(ctx context.Context) {
	var action trapCleanupAction
	var orderID string
	var fillVol float64
	var closeSide shared.Side
	positionMode := o.rt.Config().ParsedPositionMode

	o.rt.MutateRecorder(func(b *domain.CycleRecordBuilder) {
		if b.TrapOrderID == "" || trap.OutcomeTerminal(b.TrapOutcome) {
			return
		}
		orderID = b.TrapOrderID
		if b.TrapFilled {
			action = trapCleanupClosePosition
			fillVol = b.TrapFillVol
			closeSide = shared.CloseSideFor(b.TrapSide())
			b.TrapOutcome = domain.TrapOutcomeClosed
			return
		}
		action = trapCleanupCancelOrder
		b.TrapOutcome = domain.TrapOutcomeTimeout
	})

	switch action {
	case trapCleanupNone:
		return
	case trapCleanupCancelOrder:
		o.cancelOpenTrapOrderForCleanup(ctx, orderID)
	case trapCleanupClosePosition:
		o.closeOpenTrapPositionForCleanup(ctx, closeSide, fillVol, positionMode)
	}
}

func (o *CycleOrchestrator) cancelOpenTrapOrderForCleanup(ctx context.Context, orderID string) {
	cfg := o.rt.Config()
	cancelCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	if err := o.rt.Deps().Client.CancelOrder(cancelCtx, cfg.Symbol, orderID); err != nil {
		o.rt.Log().Error("🔴 Trap cleanup cancel failed - canceling all open orders", slog.Any("error", err))
		if allErr := o.rt.Deps().Client.CancelAllOpenOrders(cancelCtx, cfg.Symbol); allErr != nil {
			o.recordTrapCleanupFailure("critical_trap_cancel_failed: " + allErr.Error())
			o.publishOrLog(events.TopicTrapError, events.CycleErrorEvent{
				Flow:   events.FlowTrap,
				Symbol: cfg.Symbol,
				Error:  "critical_trap_cancel_failed: " + allErr.Error(),
			})
			o.publishOrLog(events.TopicTrapAbort, events.CycleAbortEvent{
				Flow:   events.FlowTrap,
				Symbol: cfg.Symbol,
				Reason: "critical_trap_cancel_failed: " + allErr.Error(),
			})
			return
		}
	}

	o.publishOrLog(events.TopicTrapTimeout, events.CycleTimeoutEvent{
		Flow:    events.FlowTrap,
		Symbol:  cfg.Symbol,
		Timeout: 0,
	})
}

func (o *CycleOrchestrator) closeOpenTrapPositionForCleanup(ctx context.Context, closeSide shared.Side, fillVol float64, positionMode int) {
	cfg := o.rt.Config()
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	if err := o.rt.Deps().Client.ClosePosition(closeCtx, cfg.Symbol, closeSide, fillVol, positionMode); err != nil {
		o.rt.Log().Error("🔴 Trap cleanup close failed - closing all positions", slog.Any("error", err))
		if allErr := o.rt.Deps().Client.CloseAllPositions(closeCtx, cfg.Symbol); allErr != nil {
			o.recordTrapCleanupFailure("critical_trap_close_failed: " + allErr.Error())
			o.publishOrLog(events.TopicTrapError, events.CycleErrorEvent{
				Flow:   events.FlowTrap,
				Symbol: cfg.Symbol,
				Error:  "critical_trap_close_failed: " + allErr.Error(),
			})
			o.publishOrLog(events.TopicTrapAbort, events.CycleAbortEvent{
				Flow:   events.FlowTrap,
				Symbol: cfg.Symbol,
				Reason: "critical_trap_close_failed: " + allErr.Error(),
			})
			return
		}
	}

	o.publishOrLog(events.TopicTrapPositionClosed, events.PositionClosedEvent{
		Flow:   events.FlowTrap,
		Symbol: cfg.Symbol,
		Reason: "cycle_cleanup",
	})
}

func (o *CycleOrchestrator) recordTrapCleanupFailure(reason string) {
	o.rt.MutateRecorder(func(b *domain.CycleRecordBuilder) {
		b.AbortReason = reason
		b.AbortFlow = events.FlowTrap
		b.AbortTopic = events.TopicTrapAbort
		b.ErrorFlow = events.FlowTrap
		b.ErrorTopic = events.TopicTrapError
		b.TrapOutcome = domain.TrapOutcomeAborted
		b.TrapError = reason
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
		events.TopicReversionTrailingPlaced,
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
			o.rt.Log().Info("📋 Event", slog.String("topic", topic), slog.String("msg_id", msg.UUID))
		})
	}
}
