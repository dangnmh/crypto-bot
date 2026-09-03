package futures

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"crypto-bot/internal/infrastructure/notifier"
	"crypto-bot/internal/trading/ordermanager/common"
	"crypto-bot/pkg/eventbus"
	"crypto-bot/pkg/tracectx"

	"github.com/ThreeDotsLabs/watermill/message"
)

var busRegistrations sync.Map

// InitGlobalSubscriptions registers all futures ordermanager micro-step topic handlers on the event bus EXACTLY ONCE per Bus instance.
func InitGlobalSubscriptions(ctx context.Context, mgr *OrderManager) {
	if mgr == nil || mgr.bus == nil {
		return
	}
	bus := mgr.bus

	onceVal, _ := busRegistrations.LoadOrStore(bus, &sync.Once{})
	once, ok := onceVal.(*sync.Once)
	if !ok {
		return
	}

	once.Do(func() {
		registerAllSubscriptions(ctx, mgr)
	})
}

func registerAllSubscriptions(ctx context.Context, mgr *OrderManager) {
	registerPreFlightSubscriptions(ctx, mgr)
	registerOrderExecutionSubscriptions(ctx, mgr)
	registerTimeoutSubscriptions(ctx, mgr)
	registerCompletionSubscriptions(ctx, mgr)
	registerNotificationSubscriptions(ctx, mgr)
}

func registerNotificationSubscriptions(ctx context.Context, mgr *OrderManager) {
	if mgr == nil || mgr.notifier == nil {
		return
	}
	registerNotificationHandler[OrderSubmittedEvent](ctx, mgr, TopicOrderSubmitted)
	registerNotificationHandler[OrderAbortedEvent](ctx, mgr, TopicOrderAborted)
	registerNotificationHandler[OrderCompletedEvent](ctx, mgr, TopicOrderCompleted)
}

func registerNotificationHandler[T common.OrderEvent](ctx context.Context, mgr *OrderManager, topic string) {
	subscribeTopic(ctx, mgr.bus, mgr.log, topic, func(msgCtx context.Context, msg *message.Message) error {
		var evt T
		if err := json.Unmarshal(msg.Payload, &evt); err != nil {
			return fmt.Errorf("unmarshal notification topic %s payload: %w", topic, err)
		}
		if evt.ShouldNotify() && mgr.notifier != nil {
			notifMsg := evt.GetNotifyMessage()
			if notifMsg != "" {
				orderCtx := tracectx.WithRequestIDValue(msgCtx, evt.GetReqID())
				level := notifier.LevelNormal
				if lvlProvider, ok := any(evt).(common.NotiLevelProvider); ok {
					level = lvlProvider.GetNotiLevel()
				}
				if err := mgr.notifier.Send(orderCtx, notifier.Event{
					Level:   level,
					Message: notifMsg,
				}); err != nil {
					mgr.log.ErrorContext(orderCtx, "Failed to send event notification",
						slog.String("topic", topic),
						slog.String("level", string(level)),
						slog.Any("error", err),
					)
				}
			}
		}
		return nil
	})
}

func (om *OrderManager) abortOrder(ctx context.Context, evt common.OrderEvent, preTopic, reason string, err error) error {
	om.CancelTimeoutGuard(evt.GetReqID())
	om.UnsubscribePositionWatch(ctx, evt.GetExchange(), string(evt.GetStrategyType()), evt.GetReqID())
	agg := om.GetAggregate(evt.GetReqID())
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	aborted := OrderAbortedEvent{
		ReqID:         evt.GetReqID(),
		ClientOrderID: evt.GetClientOrderID(),
		Symbol:        evt.GetSymbol(),
		Exchange:      evt.GetExchange(),
		MarketType:    evt.GetMarketType(),
		StrategyType:  evt.GetStrategyType(),
		PreTopic:      preTopic,
		NextTopic:     TopicOrderAborted,
		Timestamp:     time.Now(),
		Reason:        reason,
		Error:         errStr,
		AbortedAt:     time.Now(),
	}
	_ = agg.Record(aborted)
	_ = om.publishEvent(ctx, TopicOrderAborted, aborted)
	return err
}

func registerPreFlightSubscriptions(ctx context.Context, mgr *OrderManager) {
	// 1. OrderIntentEvent -> HandlePreFlight -> TopicOrderPreFlightDone
	registerEventSubscription(ctx, mgr, TopicOrderIntent, func(ctx context.Context, om *OrderManager, evt OrderIntentEvent) error {
		res, err := om.HandlePreFlight(ctx, evt)
		if err != nil {
			return om.abortOrder(ctx, evt, TopicOrderIntent, "preflight_error", err)
		}
		agg := om.GetAggregate(evt.ReqID)
		if err := agg.Record(res); err != nil {
			om.log.ErrorContext(ctx, "Failed to record event to aggregate", slog.String("req_id", evt.ReqID), slog.Any("error", err))
		}
		return om.publishEvent(ctx, TopicOrderPreFlightDone, res)
	})

	// 2. OrderPreFlightCompletedEvent -> HandleFireTiming -> TopicOrderFireWindowReached
	registerEventSubscription(ctx, mgr, TopicOrderPreFlightDone, func(ctx context.Context, om *OrderManager, evt OrderPreFlightCompletedEvent) error {
		res, err := om.HandleFireTiming(ctx, evt)
		if err != nil {
			return om.abortOrder(ctx, evt, TopicOrderPreFlightDone, "fire_timing_error", err)
		}
		agg := om.GetAggregate(evt.ReqID)
		if err := agg.Record(res); err != nil {
			om.log.ErrorContext(ctx, "Failed to record event to aggregate", slog.String("req_id", evt.ReqID), slog.Any("error", err))
		}
		return om.publishEvent(ctx, TopicOrderFireWindowReached, res)
	})

	// 3. OrderFireWindowReachedEvent -> HandlePositionWatchReady -> TopicOrderPositionWatchReady
	registerEventSubscription(ctx, mgr, TopicOrderFireWindowReached, func(ctx context.Context, om *OrderManager, evt OrderFireWindowReachedEvent) error {
		res, err := om.HandlePositionWatchReady(ctx, evt)
		if err != nil {
			return om.abortOrder(ctx, evt, TopicOrderFireWindowReached, "position_watch_ready_error", err)
		}
		agg := om.GetAggregate(evt.ReqID)
		if err := agg.Record(res); err != nil {
			om.log.ErrorContext(ctx, "Failed to record event to aggregate", slog.String("req_id", evt.ReqID), slog.Any("error", err))
		}
		return om.publishEvent(ctx, TopicOrderPositionWatchReady, res)
	})
}

func registerOrderExecutionSubscriptions(ctx context.Context, mgr *OrderManager) {
	// 4. OrderPositionWatchReadyEvent -> HandleExecuteOrder -> TopicOrderSubmitted
	registerEventSubscription(ctx, mgr, TopicOrderPositionWatchReady, func(ctx context.Context, om *OrderManager, evt OrderPositionWatchReadyEvent) error {
		res, err := om.HandleExecuteOrder(ctx, evt)
		if err != nil {
			return om.abortOrder(ctx, evt, TopicOrderPositionWatchReady, "submit_error", err)
		}
		agg := om.GetAggregate(evt.ReqID)
		if err := agg.Record(res); err != nil {
			om.log.ErrorContext(ctx, "Failed to record event to aggregate", slog.String("req_id", evt.ReqID), slog.Any("error", err))
		}
		return om.publishEvent(ctx, TopicOrderSubmitted, res)
	})

	// 5A. OrderSubmittedEvent -> HandleTPSLSubmission -> TopicOrderTPSLDispatched
	registerEventSubscription(ctx, mgr, TopicOrderSubmitted, func(ctx context.Context, om *OrderManager, evt OrderSubmittedEvent) error {
		res, err := om.HandleTPSLSubmission(ctx, evt)
		if err != nil {
			return err
		}
		if res != nil {
			agg := om.GetAggregate(evt.ReqID)
			if err := agg.Record(*res); err != nil {
				om.log.ErrorContext(ctx, "Failed to record event to aggregate", slog.String("req_id", evt.ReqID), slog.Any("error", err))
			}
			return om.publishEvent(ctx, TopicOrderTPSLDispatched, *res)
		}
		return nil
	})

	// 5B. OrderSubmittedEvent -> HandleOutcomeWatcher -> TopicOrderOutcomeResolved
	registerEventSubscription(ctx, mgr, TopicOrderSubmitted, func(ctx context.Context, om *OrderManager, evt OrderSubmittedEvent) error {
		res, err := om.HandleOutcomeWatcher(ctx, evt)
		if err != nil {
			return err
		}
		agg := om.GetAggregate(evt.ReqID)
		if err := agg.Record(res); err != nil {
			om.log.ErrorContext(ctx, "Failed to record event to aggregate", slog.String("req_id", evt.ReqID), slog.Any("error", err))
		}
		return om.publishEvent(ctx, TopicOrderOutcomeResolved, res)
	})

	// 5C. OrderSubmittedEvent -> HandleScheduleUnfilledCancelTimeout -> Pre-fill resting order watchdog / taker timeout
	registerEventSubscription(ctx, mgr, TopicOrderSubmitted, func(ctx context.Context, om *OrderManager, evt OrderSubmittedEvent) error {
		return om.HandleScheduleUnfilledCancelTimeout(ctx, evt)
	})

	// 5D. OrderFilledEvent -> HandleSchedulePositionCloseTimeout -> Post-fill position hold watchdog
	registerEventSubscription(ctx, mgr, TopicOrderFilled, func(ctx context.Context, om *OrderManager, evt OrderFilledEvent) error {
		return om.HandleSchedulePositionCloseTimeout(ctx, evt)
	})
}

func registerTimeoutSubscriptions(ctx context.Context, mgr *OrderManager) {
	// 5. OrderTimeoutScheduledEvent -> HandleWaitTimeoutDeadline -> TopicOrderTimeoutPositionChecked
	registerEventSubscription(ctx, mgr, TopicOrderTimeoutScheduled, func(ctx context.Context, om *OrderManager, evt OrderTimeoutScheduledEvent) error {
		res, err := om.HandleWaitTimeoutDeadline(ctx, evt)
		if err != nil {
			return err
		}
		agg := om.GetAggregate(evt.ReqID)
		if err := agg.Record(res); err != nil {
			om.log.ErrorContext(ctx, "Failed to record event to aggregate", slog.String("req_id", evt.ReqID), slog.Any("error", err))
		}
		return om.publishEvent(ctx, TopicOrderTimeoutPositionChecked, res)
	})

	// 6. OrderTimeoutPositionCheckedEvent -> HandleExecuteBailout (if open) -> TopicOrderBailoutExecuted
	registerEventSubscription(ctx, mgr, TopicOrderTimeoutPositionChecked, func(ctx context.Context, om *OrderManager, evt OrderTimeoutPositionCheckedEvent) error {
		if evt.HoldVol <= 0 {
			return nil
		}
		agg := om.GetAggregate(evt.ReqID)
		res, err := om.HandleExecuteBailout(ctx, evt.ReqID, evt.Exchange, evt.Symbol, agg.Side(), evt.HoldVol, "timeout_position_open")
		if err != nil {
			return om.abortOrder(ctx, evt, TopicOrderTimeoutPositionChecked, "bailout_error", err)
		}
		if err := agg.Record(res); err != nil {
			om.log.ErrorContext(ctx, "Failed to record event to aggregate", slog.String("req_id", evt.ReqID), slog.Any("error", err))
		}
		return om.publishEvent(ctx, TopicOrderBailoutExecuted, res)
	})
}

func registerCompletionSubscriptions(ctx context.Context, mgr *OrderManager) {
	registerOutcomeResolvedSubscription(ctx, mgr)
	registerPositionClosedSubscription(ctx, mgr)
	registerOrderCompletedSubscription(ctx, mgr)
	registerTradeRecordSubscription(ctx, mgr)
}

func registerOutcomeResolvedSubscription(ctx context.Context, mgr *OrderManager) {
	// 8. OrderOutcomeResolvedEvent -> Only complete if canceled with no fill or if closing order filled; resting orders await fill; filled opening orders remain open awaiting position close or timeout.
	registerEventSubscription(ctx, mgr, TopicOrderOutcomeResolved, func(ctx context.Context, om *OrderManager, evt OrderOutcomeResolvedEvent) error {
		if evt.Outcome == common.OutcomeResting {
			om.log.InfoContext(ctx, "Order resting on order book awaiting stream fill or cancel", slog.String("req_id", evt.GetReqID()))
			return nil
		}

		agg := om.GetAggregate(evt.GetReqID())
		isCloseOrder := agg.Side().IsClose()
		if evt.Outcome == common.OutcomeCanceledNoFill || (evt.Outcome == "unknown" && evt.FilledVol == 0) || (isCloseOrder && evt.Outcome == common.OutcomeFilled) {
			om.CancelTimeoutGuard(evt.GetReqID())
			strategy := agg.StrategyType()
			reason := evt.Reason
			if reason == "" && isCloseOrder {
				reason = "close_order_filled"
			}
			completed, err := om.HandleEnrichAndComplete(ctx, evt.Exchange, evt.GetReqID(), evt.GetClientOrderID(), evt.Symbol, strategy, evt.Outcome, reason)
			if err != nil {
				return err
			}

			if err := agg.Record(completed); err != nil {
				om.log.ErrorContext(ctx, "Failed to record event to aggregate", slog.String("req_id", evt.GetReqID()), slog.Any("error", err))
			}
			return om.publishEvent(ctx, TopicOrderCompleted, completed)
		}
		om.log.InfoContext(ctx, "Order filled; position now open, awaiting position close update or timeout", slog.String("req_id", evt.GetReqID()), slog.String("outcome", string(evt.Outcome)))
		return nil
	})
}

func registerPositionClosedSubscription(ctx context.Context, mgr *OrderManager) {
	// 8B. OrderPositionClosedEvent -> HandleEnrichAndComplete -> TopicOrderCompleted
	registerEventSubscription(ctx, mgr, TopicOrderPositionClosed, func(ctx context.Context, om *OrderManager, evt OrderPositionClosedEvent) error {
		om.CancelTimeoutGuard(evt.GetReqID())
		agg := om.GetAggregate(evt.GetReqID())
		completed, err := om.HandleEnrichAndComplete(ctx, evt.Exchange, evt.GetReqID(), evt.GetClientOrderID(), evt.Symbol, agg.StrategyType(), common.OutcomeFilled, evt.Reason)
		if err != nil {
			return err
		}
		if err := agg.Record(completed); err != nil {
			om.log.ErrorContext(ctx, "Failed to record event to aggregate", slog.String("req_id", evt.GetReqID()), slog.Any("error", err))
		}
		return om.publishEvent(ctx, TopicOrderCompleted, completed)
	})
}

func registerOrderCompletedSubscription(ctx context.Context, mgr *OrderManager) {
	// 9. OrderCompletedEvent -> Build Trade Record from event array & Publish to TopicOrderTradeRecord
	registerEventSubscription(ctx, mgr, TopicOrderCompleted, func(ctx context.Context, om *OrderManager, evt OrderCompletedEvent) error {
		om.invokeOnCompletedCallbacks(ctx, evt)
		om.CancelTimeoutGuard(evt.GetReqID())
		om.UnsubscribePositionWatch(ctx, evt.GetExchange(), string(evt.GetStrategyType()), evt.GetReqID())
		agg := om.GetAggregate(evt.GetReqID())
		record := agg.BuildTradeRecord()
		if record.ExchangeOrderID == "" {
			if exOID, ok := om.GetExchangeOrderIDByReqID(evt.GetReqID()); ok && exOID != "" {
				record.ExchangeOrderID = exOID
			}
		}
		if err := agg.Record(record); err != nil {
			om.log.ErrorContext(ctx, "Failed to record event to aggregate", slog.String("req_id", evt.GetReqID()), slog.Any("error", err))
		}
		return om.publishEvent(ctx, common.TopicOrderTradeRecord, record)
	})
}

func registerTradeRecordSubscription(ctx context.Context, mgr *OrderManager) {
	// 10. OrderTradeRecordEvent -> Persistence into DB Repository
	registerEventSubscription(ctx, mgr, common.TopicOrderTradeRecord, func(ctx context.Context, om *OrderManager, evt common.OrderTradeRecordEvent) error {
		if err := om.repo.Save(ctx, evt); err != nil {
			om.log.ErrorContext(ctx, "Failed to persist trade record to DB trades table", slog.Any("error", err))
			return err
		}
		om.log.InfoContext(ctx, "Successfully persisted trade record to DB trades table", slog.String("req_id", evt.ReqID))
		return nil
	})
}

func registerEventSubscription[T common.OrderEvent](
	ctx context.Context,
	mgr *OrderManager,
	topic string,
	action func(context.Context, *OrderManager, T) error,
) {
	subscribeTopic(ctx, mgr.bus, mgr.log, topic, func(msgCtx context.Context, msg *message.Message) error {
		var evt T
		if err := json.Unmarshal(msg.Payload, &evt); err != nil {
			return fmt.Errorf("unmarshal topic %s payload: %w", topic, err)
		}
		reqID := evt.GetReqID()
		orderCtx := tracectx.WithRequestIDValue(msgCtx, reqID)
		mgr.log.InfoContext(orderCtx, "Futures OrderManager: Handled micro-event topic", slog.String("topic", topic), slog.String("req_id", reqID))
		agg := mgr.GetAggregate(reqID)
		_ = agg.Record(evt)

		return action(orderCtx, mgr, evt)
	})
}

func subscribeTopic(subCtx context.Context, bus *eventbus.Bus, logger *slog.Logger, topic string, handler func(context.Context, *message.Message) error) {
	if bus == nil {
		return
	}
	ch, err := bus.Subscribe(subCtx, topic)
	if err != nil {
		logger.ErrorContext(subCtx, "Failed to subscribe to topic", slog.String("topic", topic), slog.Any("error", err))
		return
	}

	go processTopicMessages(subCtx, ch, topic, logger, handler)
}

func processTopicMessages(subCtx context.Context, ch <-chan *message.Message, topic string, logger *slog.Logger, handler func(context.Context, *message.Message) error) {
	for {
		select {
		case <-subCtx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			msgCtx := context.WithoutCancel(subCtx)
			go dispatchMessage(msgCtx, msg, topic, logger, handler)
		}
	}
}

func dispatchMessage(msgCtx context.Context, m *message.Message, topic string, logger *slog.Logger, handler func(context.Context, *message.Message) error) {
	defer func() {
		if r := recover(); r != nil {
			logger.ErrorContext(msgCtx, "Panic recovered in Futures OrderManager topic handler", slog.String("topic", topic), slog.Any("panic", r))
		}
		m.Ack()
	}()

	if err := handler(msgCtx, m); err != nil {
		logger.ErrorContext(msgCtx, "Futures OrderManager handler execution failed", slog.String("topic", topic), slog.Any("error", err))
	}
}
