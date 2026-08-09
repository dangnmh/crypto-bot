package reversion

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"

	"crypto-bot/internal/bots/funding/domain"
	"crypto-bot/internal/infrastructure/observability"
	"crypto-bot/pkg/eventbus"

	"github.com/ThreeDotsLabs/watermill/message"
)

var busRegistrations sync.Map

// InitGlobalSubscriptions registers all reversion topic handlers on the global event bus EXACTLY ONCE per Bus instance.
func InitGlobalSubscriptions(ctx context.Context, runner *StatelessRunner) {
	bus := runner.bus
	if bus == nil {
		return
	}

	onceVal, _ := busRegistrations.LoadOrStore(bus, &sync.Once{})
	once, ok := onceVal.(*sync.Once)
	if !ok {
		return
	}

	once.Do(func() {
		registerAllSubscriptions(ctx, bus, runner)
	})
}

func registerAllSubscriptions(ctx context.Context, bus *eventbus.Bus, runner *StatelessRunner) {
	// Stage 1
	registerEventSubscription(ctx, runner, TopicReversionCandidate, func(ctx context.Context, r *StatelessRunner, evt CandidateFoundEvent) error {
		return r.handleArm(ctx, evt)
	})
	registerEventSubscription(ctx, runner, TopicReversionArmMarketReady, func(ctx context.Context, r *StatelessRunner, evt ArmMarketReadyEvent) error {
		return r.handleArmMarketReady(ctx, evt)
	})
	registerEventSubscription(ctx, runner, TopicReversionArmPlanCalculated, func(ctx context.Context, r *StatelessRunner, evt ArmPlanCalculatedEvent) error {
		return r.handleArmPlanCalculated(ctx, evt)
	})
	registerEventSubscription(ctx, runner, TopicReversionSafetyChecked, func(ctx context.Context, r *StatelessRunner, evt SafetyCheckedEvent) error {
		return r.handleSafetyChecked(ctx, evt)
	})
	registerEventSubscription(ctx, runner, TopicReversionArmed, func(ctx context.Context, r *StatelessRunner, evt ArmedEvent) error {
		return r.handleWait(ctx, evt)
	})
	registerEventSubscription(ctx, runner, TopicReversionWaitComplete, func(ctx context.Context, r *StatelessRunner, evt WaitCompleteEvent) error {
		return r.handleRecheck(ctx, evt)
	})
	registerEventSubscription(ctx, runner, TopicReversionConfirmed, func(ctx context.Context, r *StatelessRunner, evt ConfirmedEvent) error {
		return r.handleFireIOC(ctx, evt)
	})
	registerEventSubscription(ctx, runner, TopicReversionMarginModeReady, func(ctx context.Context, r *StatelessRunner, evt MarginModeReadyEvent) error {
		return r.handleMarginModeReady(ctx, evt)
	})

	// Stage 2
	registerEventSubscription(ctx, runner, TopicReversionFireTimingReady, func(ctx context.Context, r *StatelessRunner, evt FireTimingReadyEvent) error {
		return r.handleFireTimingReady(ctx, evt)
	})
	registerEventSubscription(ctx, runner, TopicReversionFirePlanChecked, func(ctx context.Context, r *StatelessRunner, evt FirePlanCheckedEvent) error {
		return r.handleFirePlanChecked(ctx, evt)
	})
	registerEventSubscription(ctx, runner, TopicReversionFireWindowReached, func(ctx context.Context, r *StatelessRunner, evt FireWindowReachedEvent) error {
		return r.handleFireWindowReached(ctx, evt)
	})
	registerEventSubscription(ctx, runner, TopicReversionPositionWatchReady, func(ctx context.Context, r *StatelessRunner, evt PositionWatchReadyEvent) error {
		return r.handlePositionWatchReady(ctx, evt)
	})
	registerEventSubscription(ctx, runner, TopicReversionIOCSubmitted, func(ctx context.Context, r *StatelessRunner, evt IOCSubmittedEvent) error {
		return r.handleIOCSubmitted(ctx, evt)
	})
	registerEventSubscription(ctx, runner, TopicReversionIOCOutcomeChecked, func(ctx context.Context, r *StatelessRunner, evt IOCOutcomeCheckedEvent) error {
		return r.handleIOCOutcomeChecked(ctx, evt)
	})
	registerEventSubscription(ctx, runner, TopicReversionTPSLRequired, func(ctx context.Context, r *StatelessRunner, evt TPSLRequiredEvent) error {
		return r.handleTPSLRequired(ctx, evt)
	})
	registerEventSubscription(ctx, runner, TopicReversionOrderFilled, func(ctx context.Context, r *StatelessRunner, evt OrderFilledEvent) error {
		return nil
	})

	// Stage 3
	registerEventSubscription(ctx, runner, TopicReversionTimeoutGuardScheduled, func(ctx context.Context, r *StatelessRunner, evt TimeoutGuardScheduledEvent) error {
		return r.handleTimeoutGuardScheduled(ctx, evt)
	})
	registerEventSubscription(ctx, runner, TopicReversionTimeoutPositionChecked, func(ctx context.Context, r *StatelessRunner, evt TimeoutPositionCheckedEvent) error {
		return r.handleTimeoutPositionChecked(ctx, evt)
	})
	registerEventSubscription(ctx, runner, TopicReversionForceCloseInitiated, func(ctx context.Context, r *StatelessRunner, evt ForceCloseInitiatedEvent) error {
		return r.handleForceCloseInitiated(ctx, evt)
	})
	registerEventSubscription(ctx, runner, TopicReversionForceCloseCompleted, func(ctx context.Context, r *StatelessRunner, evt ForceCloseCompletedEvent) error {
		return r.handleForceCloseCompleted(ctx, evt)
	})
	registerEventSubscription(ctx, runner, TopicReversionTimeout, func(ctx context.Context, r *StatelessRunner, evt TimeoutEvent) error {
		return r.handleTimeout(ctx, evt)
	})

	// Cleanup subscriptions wrapped with report recording
	subscribeTopicWithReport(ctx, runner, TopicReversionPositionClosed, func(ctx context.Context, r *StatelessRunner, evt PositionClosedEvent, msg *message.Message) error {
		return r.handleCleanup(ctx, msg)
	})
	subscribeTopicWithReport(ctx, runner, TopicReversionAbort, func(ctx context.Context, r *StatelessRunner, evt AbortEvent, msg *message.Message) error {
		return r.handleCleanup(ctx, msg)
	})
	subscribeTopicWithReport(ctx, runner, TopicReversionError, func(ctx context.Context, r *StatelessRunner, evt ErrorEvent, msg *message.Message) error {
		return r.handleCleanup(ctx, msg)
	})

	// Database persistence subscriber
	subscribeTopic(ctx, bus, runner.log, TopicReversionTradeReport, runner.handleReportPersistence)
}

func (r *StatelessRunner) handleReportPersistence(ctx context.Context, msg *message.Message) error {
	var evt ReversionTradeReportEvent
	if err := json.Unmarshal(msg.Payload, &evt); err != nil {
		return err
	}

	// Map to domain entity TradeReport
	report := &domain.TradeReport{
		ReqID:               evt.ReqID,
		EventID:             evt.EventID,
		Timestamp:           evt.Timestamp,
		SettleTime:          evt.SettleTime,
		Exchange:            evt.Exchange,
		Symbol:              evt.Symbol,
		NormalizedSymbol:    evt.NormalizedSymbol,
		Side:                evt.Side,
		FundingRate:         evt.FundingRate,
		CandidateFoundTime:  evt.CandidateFoundTime,
		MarginUSDT:          evt.MarginUSDT,
		Leverage:            evt.Leverage,
		BufferTimeMs:        evt.BufferTimeMs,
		LatencyRTTMs:        evt.LatencyRTTMs,
		ActualSlippage:      evt.ActualSlippage,
		FireOffsetMs:        evt.FireOffsetMs,
		IOCOrderID:          evt.IOCOrderID,
		IOCOutcome:          evt.IOCOutcome,
		IOCReason:           evt.IOCReason,
		FireIOCTime:         evt.FireIOCTime,
		LocalFireIOCTime:    evt.LocalFireIOCTime,
		OrderFilled:         evt.OrderFilled,
		FillPrice:           evt.FillPrice,
		ClosePrice:          evt.ClosePrice,
		VolumeUSDT:          evt.VolumeUSDT,
		GrossProfit:         evt.GrossProfit,
		NetProfit:           evt.NetProfit,
		PnLPct:              evt.PnLPct,
		Fee:                 evt.Fee,
		HoldFee:             evt.HoldFee,
		HoldDurationMs:      evt.HoldDurationMs,
		ExitReason:          evt.ExitReason,
		CloseRetryCount:     evt.CloseRetryCount,
		ForceCloseAttempted: evt.ForceCloseAttempted,
		ForceCloseSucceeded: evt.ForceCloseSucceeded,
		Status:              evt.Status,
		ErrorMsg:            evt.ErrorMsg,
	}

	if err := r.reportRepo.Save(ctx, report); err != nil {
		r.log.ErrorContext(ctx, "Failed to persist trade report to database", slog.Any("error", err))
		return err
	}
	r.log.InfoContext(ctx, "Successfully persisted trade report to database", slog.String("req_id", evt.ReqID))
	return nil
}

func registerEventSubscription[T ReversionEvent](
	ctx context.Context,
	runner *StatelessRunner,
	topic string,
	action func(context.Context, *StatelessRunner, T) error,
) {
	subscribeTopicWithReport(ctx, runner, topic, func(ctx context.Context, r *StatelessRunner, evt T, msg *message.Message) error {
		return action(ctx, r, evt)
	})
}

func subscribeTopicWithReport[T ReversionEvent](
	ctx context.Context,
	runner *StatelessRunner,
	topic string,
	handler func(context.Context, *StatelessRunner, T, *message.Message) error,
) {
	subscribeTopic(ctx, runner.bus, runner.log, topic, func(ctx context.Context, msg *message.Message) error {
		var evt T
		if err := json.Unmarshal(msg.Payload, &evt); err != nil {
			return err
		}
		exch := evt.GetExchange()
		reqID := evt.GetReqID()
		symbol := evt.GetSymbol()
		traceCtx := observability.WithRequestIDValue(ctx, reqID)
		clonedRunner := runner.clone(exch, reqID, symbol)
		clonedRunner.recordEventState(topic, evt)
		return handler(traceCtx, clonedRunner, evt, msg)
	})
}

func subscribeTopic(ctx context.Context, bus *eventbus.Bus, logger *slog.Logger, topic string, handler func(context.Context, *message.Message) error) {
	ch, err := bus.Subscribe(ctx, topic)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to subscribe to topic", slog.String("topic", topic), slog.Any("error", err))
		return
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				go func(m *message.Message) {
					if err := handler(ctx, m); err != nil {
						if errors.Is(err, ErrFRBelowThreshold) || errors.Is(err, ErrFRSignFlip) {
							logger.InfoContext(ctx, "Handler execution skipped (expected abort)", slog.String("topic", topic), slog.Any("error", err))
						} else {
							logger.ErrorContext(ctx, "Handler execution failed", slog.String("topic", topic), slog.Any("error", err))
						}
					}
					m.Ack()
				}(msg)
			}
		}
	}()
}
