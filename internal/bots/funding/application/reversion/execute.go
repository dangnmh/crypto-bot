package reversion

import (
	"context"
	"encoding/json"
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
	registerEventSubscription(ctx, bus, runner, TopicReversionCandidate,
		func(e *CandidateFoundEvent) (string, string, string) { return e.Exchange, e.ReqID, e.Symbol },
		func(ctx context.Context, r *StatelessRunner, evt CandidateFoundEvent) error {
			return r.handleArm(ctx, evt)
		},
	)
	registerEventSubscription(ctx, bus, runner, TopicReversionArmMarketReady,
		func(e *ArmMarketReadyEvent) (string, string, string) { return e.Exchange, e.ReqID, e.Symbol },
		func(ctx context.Context, r *StatelessRunner, evt ArmMarketReadyEvent) error {
			return r.handleArmMarketReady(ctx, evt)
		},
	)
	registerEventSubscription(ctx, bus, runner, TopicReversionArmPlanCalculated,
		func(e *ArmPlanCalculatedEvent) (string, string, string) { return e.Exchange, e.ReqID, e.Symbol },
		func(ctx context.Context, r *StatelessRunner, evt ArmPlanCalculatedEvent) error {
			return r.handleArmPlanCalculated(ctx, evt)
		},
	)
	registerEventSubscription(ctx, bus, runner, TopicReversionSafetyChecked,
		func(e *SafetyCheckedEvent) (string, string, string) { return e.Exchange, e.ReqID, e.Symbol },
		func(ctx context.Context, r *StatelessRunner, evt SafetyCheckedEvent) error {
			return r.handleSafetyChecked(ctx, evt)
		},
	)
	registerEventSubscription(ctx, bus, runner, TopicReversionArmed,
		func(e *ArmedEvent) (string, string, string) { return e.Exchange, e.ReqID, e.Symbol },
		func(ctx context.Context, r *StatelessRunner, evt ArmedEvent) error {
			return r.handleWait(ctx, evt)
		},
	)
	registerEventSubscription(ctx, bus, runner, TopicReversionWaitComplete,
		func(e *WaitCompleteEvent) (string, string, string) { return e.Exchange, e.ReqID, e.Symbol },
		func(ctx context.Context, r *StatelessRunner, evt WaitCompleteEvent) error {
			return r.handleRecheck(ctx, evt)
		},
	)
	registerEventSubscription(ctx, bus, runner, TopicReversionConfirmed,
		func(e *ConfirmedEvent) (string, string, string) { return e.Exchange, e.ReqID, e.Symbol },
		func(ctx context.Context, r *StatelessRunner, evt ConfirmedEvent) error {
			return r.handleFireIOC(ctx, evt)
		},
	)
	registerEventSubscription(ctx, bus, runner, TopicReversionMarginModeReady,
		func(e *MarginModeReadyEvent) (string, string, string) { return e.Exchange, e.ReqID, e.Symbol },
		func(ctx context.Context, r *StatelessRunner, evt MarginModeReadyEvent) error {
			return r.handleMarginModeReady(ctx, evt)
		},
	)

	// Stage 2
	registerEventSubscription(ctx, bus, runner, TopicReversionFireTimingReady,
		func(e *FireTimingReadyEvent) (string, string, string) { return e.Exchange, e.ReqID, e.Symbol },
		func(ctx context.Context, r *StatelessRunner, evt FireTimingReadyEvent) error {
			return r.handleFireTimingReady(ctx, evt)
		},
	)
	registerEventSubscription(ctx, bus, runner, TopicReversionFirePlanChecked,
		func(e *FirePlanCheckedEvent) (string, string, string) { return e.Exchange, e.ReqID, e.Symbol },
		func(ctx context.Context, r *StatelessRunner, evt FirePlanCheckedEvent) error {
			return r.handleFirePlanChecked(ctx, evt)
		},
	)
	registerEventSubscription(ctx, bus, runner, TopicReversionFireWindowReached,
		func(e *FireWindowReachedEvent) (string, string, string) { return e.Exchange, e.ReqID, e.Symbol },
		func(ctx context.Context, r *StatelessRunner, evt FireWindowReachedEvent) error {
			return r.handleFireWindowReached(ctx, evt)
		},
	)
	registerEventSubscription(ctx, bus, runner, TopicReversionPositionWatchReady,
		func(e *PositionWatchReadyEvent) (string, string, string) { return e.Exchange, e.ReqID, e.Symbol },
		func(ctx context.Context, r *StatelessRunner, evt PositionWatchReadyEvent) error {
			return r.handlePositionWatchReady(ctx, evt)
		},
	)
	registerEventSubscription(ctx, bus, runner, TopicReversionIOCSubmitted,
		func(e *IOCSubmittedEvent) (string, string, string) { return e.Exchange, e.ReqID, e.Symbol },
		func(ctx context.Context, r *StatelessRunner, evt IOCSubmittedEvent) error {
			return r.handleIOCSubmitted(ctx, evt)
		},
	)
	registerEventSubscription(ctx, bus, runner, TopicReversionIOCOutcomeChecked,
		func(e *IOCOutcomeCheckedEvent) (string, string, string) { return e.Exchange, e.ReqID, e.Symbol },
		func(ctx context.Context, r *StatelessRunner, evt IOCOutcomeCheckedEvent) error {
			return r.handleIOCOutcomeChecked(ctx, evt)
		},
	)
	registerEventSubscription(ctx, bus, runner, TopicReversionTPSLRequired,
		func(e *TPSLRequiredEvent) (string, string, string) { return e.Exchange, e.ReqID, e.Symbol },
		func(ctx context.Context, r *StatelessRunner, evt TPSLRequiredEvent) error {
			return r.handleTPSLRequired(ctx, evt)
		},
	)

	// Stage 3
	registerEventSubscription(ctx, bus, runner, TopicReversionTimeoutGuardScheduled,
		func(e *TimeoutGuardScheduledEvent) (string, string, string) { return e.Exchange, e.ReqID, e.Symbol },
		func(ctx context.Context, r *StatelessRunner, evt TimeoutGuardScheduledEvent) error {
			return r.handleTimeoutGuardScheduled(ctx, evt)
		},
	)
	registerEventSubscription(ctx, bus, runner, TopicReversionTimeoutPositionChecked,
		func(e *TimeoutPositionCheckedEvent) (string, string, string) { return e.Exchange, e.ReqID, e.Symbol },
		func(ctx context.Context, r *StatelessRunner, evt TimeoutPositionCheckedEvent) error {
			return r.handleTimeoutPositionChecked(ctx, evt)
		},
	)
	registerEventSubscription(ctx, bus, runner, TopicReversionForceCloseInitiated,
		func(e *ForceCloseInitiatedEvent) (string, string, string) { return e.Exchange, e.ReqID, e.Symbol },
		func(ctx context.Context, r *StatelessRunner, evt ForceCloseInitiatedEvent) error {
			return r.handleForceCloseInitiated(ctx, evt)
		},
	)
	registerEventSubscription(ctx, bus, runner, TopicReversionForceCloseCompleted,
		func(e *ForceCloseCompletedEvent) (string, string, string) { return e.Exchange, e.ReqID, e.Symbol },
		func(ctx context.Context, r *StatelessRunner, evt ForceCloseCompletedEvent) error {
			return r.handleForceCloseCompleted(ctx, evt)
		},
	)
	registerEventSubscription(ctx, bus, runner, TopicReversionTimeout,
		func(e *TimeoutEvent) (string, string, string) { return e.Exchange, e.ReqID, e.Symbol },
		func(ctx context.Context, r *StatelessRunner, evt TimeoutEvent) error {
			return r.handleTimeout(ctx, evt)
		},
	)

	cleanupTopics := []string{
		TopicReversionPositionClosed,
		TopicReversionAbort,
		TopicReversionError,
	}
	for _, topic := range cleanupTopics {
		subscribeTopic(ctx, bus, runner.log, topic, func(ctx context.Context, msg *message.Message) error {
			var evt struct {
				BaseReversionEvent
			}
			if err := json.Unmarshal(msg.Payload, &evt); err != nil {
				return err
			}
			clonedRunner := runner.clone(evt.Exchange, evt.ReqID, evt.Symbol)
			clonedRunner.recordEventState(topic, evt)
			traceCtx := observability.WithRequestIDValue(ctx, evt.ReqID)
			return clonedRunner.handleCleanup(traceCtx, msg)
		})
	}

	// Database persistence subscriber
	if runner.reportRepo != nil {
		subscribeTopic(ctx, bus, runner.log, TopicReversionTradeReport, func(ctx context.Context, msg *message.Message) error {
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

			if err := runner.reportRepo.Save(ctx, report); err != nil {
				runner.log.ErrorContext(ctx, "Failed to persist trade report to database", slog.Any("error", err))
				return err
			}
			runner.log.InfoContext(ctx, "Successfully persisted trade report to database", slog.String("req_id", evt.ReqID))
			return nil
		})
	}
}

func registerEventSubscription[T any](
	ctx context.Context,
	bus *eventbus.Bus,
	runner *StatelessRunner,
	topic string,
	route func(*T) (string, string, string),
	action func(context.Context, *StatelessRunner, T) error,
) {
	subscribeTopic(ctx, bus, runner.log, topic, func(ctx context.Context, msg *message.Message) error {
		var evt T
		if err := json.Unmarshal(msg.Payload, &evt); err != nil {
			return err
		}
		exch, reqID, symbol := route(&evt)
		traceCtx := observability.WithRequestIDValue(ctx, reqID)
		clonedRunner := runner.clone(exch, reqID, symbol)
		clonedRunner.recordEventState(topic, evt)
		return action(traceCtx, clonedRunner, evt)
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
						logger.ErrorContext(ctx, "Handler execution failed", slog.String("topic", topic), slog.Any("error", err))
					}
					m.Ack()
				}(msg)
			}
		}
	}()
}
