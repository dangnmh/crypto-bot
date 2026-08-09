package ordermanager

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"crypto-bot/internal/infrastructure/notifier"
	"crypto-bot/pkg/eventbus"

	"github.com/ThreeDotsLabs/watermill/message"
)

var busRegistrations sync.Map

// InitGlobalSubscriptions registers all ordermanager micro-step topic handlers on the event bus EXACTLY ONCE per Bus instance.
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
		time.Sleep(10 * time.Millisecond)
	})
}

func registerAllSubscriptions(ctx context.Context, mgr *OrderManager) {
	// 1. OrderIntentEvent -> HandlePreFlight -> TopicOrderPreFlightDone
	registerEventSubscription(ctx, mgr, TopicOrderIntent, func(ctx context.Context, om *OrderManager, evt OrderIntentEvent) error {
		res, err := om.HandlePreFlight(ctx, evt)
		if err != nil {
			return err
		}
		agg := om.GetAggregate(evt.ReqID)
		_ = agg.Record(res)
		return om.publishEvent(ctx, TopicOrderPreFlightDone, res)
	})

	// 2. OrderPreFlightCompletedEvent -> HandleFireTiming -> TopicOrderFireWindowReached
	registerEventSubscription(ctx, mgr, TopicOrderPreFlightDone, func(ctx context.Context, om *OrderManager, evt OrderPreFlightCompletedEvent) error {
		res, err := om.HandleFireTiming(ctx, evt)
		if err != nil {
			return err
		}
		agg := om.GetAggregate(evt.ReqID)
		_ = agg.Record(res)
		return om.publishEvent(ctx, TopicOrderFireWindowReached, res)
	})

	// 3. OrderFireWindowReachedEvent -> HandleExecuteOrder -> TopicOrderSubmitted
	registerEventSubscription(ctx, mgr, TopicOrderFireWindowReached, func(ctx context.Context, om *OrderManager, evt OrderFireWindowReachedEvent) error {
		res, err := om.HandleExecuteOrder(ctx, evt)
		if err != nil {
			return err
		}
		agg := om.GetAggregate(evt.ReqID)
		_ = agg.Record(res)
		return om.publishEvent(ctx, TopicOrderSubmitted, res)
	})

	// 4. OrderSubmittedEvent -> HandleOutcomeWatcher -> TopicOrderOutcomeResolved
	registerEventSubscription(ctx, mgr, TopicOrderSubmitted, func(ctx context.Context, om *OrderManager, evt OrderSubmittedEvent) error {
		res, err := om.HandleOutcomeWatcher(ctx, evt)
		if err != nil {
			return err
		}
		agg := om.GetAggregate(evt.ReqID)
		_ = agg.Record(res)
		return om.publishEvent(ctx, TopicOrderOutcomeResolved, res)
	})

	// 5. OrderOutcomeResolvedEvent -> HandleEnrichAndComplete -> TopicOrderCompleted
	registerEventSubscription(ctx, mgr, TopicOrderOutcomeResolved, func(ctx context.Context, om *OrderManager, evt OrderOutcomeResolvedEvent) error {
		om.CancelTimeoutGuard(evt.GetReqID())
		agg := om.GetAggregate(evt.GetReqID())
		completed := om.HandleEnrichAndComplete(ctx, evt.GetReqID(), evt.GetClientOrderID(), evt.Symbol, agg.StrategyType(), string(evt.Outcome), evt.Reason)
		_ = agg.Record(completed)
		return om.publishEvent(ctx, TopicOrderCompleted, completed)
	})

	// 6. OrderCompletedEvent -> Build Trade Record from event array & Publish to TopicOrderTradeRecord
	registerEventSubscription(ctx, mgr, TopicOrderCompleted, func(ctx context.Context, om *OrderManager, evt OrderCompletedEvent) error {
		om.CancelTimeoutGuard(evt.GetReqID())
		agg := om.GetAggregate(evt.GetReqID())
		record := agg.BuildTradeRecord()
		_ = agg.Record(record)
		return om.publishEvent(ctx, TopicOrderTradeRecord, record)
	})

	// 7. OrderTradeRecordEvent -> Persistence into DB Repository
	registerEventSubscription(ctx, mgr, TopicOrderTradeRecord, func(ctx context.Context, om *OrderManager, evt OrderTradeRecordEvent) error {
		if om.repo == nil {
			return nil
		}

		if err := om.repo.Save(ctx, evt); err != nil {
			om.log.ErrorContext(ctx, "Failed to persist trade record to DB trades table", slog.Any("error", err))
			return err
		}
		om.log.InfoContext(ctx, "Successfully persisted trade record to DB trades table", slog.String("req_id", evt.ReqID))
		return nil
	})
}

func registerEventSubscription[T OrderEvent](
	ctx context.Context,
	mgr *OrderManager,
	topic string,
	action func(context.Context, *OrderManager, T) error,
) {
	subscribeTopic(ctx, mgr.bus, mgr.log, topic, func(ctx context.Context, msg *message.Message) error {
		var evt T
		if err := json.Unmarshal(msg.Payload, &evt); err != nil {
			return fmt.Errorf("unmarshal topic %s payload: %w", topic, err)
		}
		mgr.log.InfoContext(ctx, "OrderManager: Handled micro-event topic", slog.String("topic", topic), slog.String("req_id", evt.GetReqID()))
		reqID := evt.GetReqID()
		agg := mgr.GetAggregate(reqID)
		_ = agg.Record(evt)

		if evt.ShouldNotify() && mgr.notifier != nil {
			notifyEvt := notifier.Event{
				Level:     notifier.LevelTrading,
				Exchange:  evt.GetExchange(),
				Symbol:    evt.GetSymbol(),
				Message:   evt.GetNotifyMessage(),
				Timestamp: evt.GetTimestamp(),
			}
			if err := mgr.notifier.Send(ctx, notifyEvt); err != nil {
				mgr.log.ErrorContext(ctx, "Failed to send event notification", slog.String("topic", topic), slog.Any("error", err))
			}
		}

		return action(ctx, mgr, evt)
	})
}

func subscribeTopic(ctx context.Context, bus *eventbus.Bus, logger *slog.Logger, topic string, handler func(context.Context, *message.Message) error) {
	if bus == nil {
		return
	}
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
					defer func() {
						if r := recover(); r != nil {
							logger.ErrorContext(ctx, "Panic recovered in OrderManager topic handler", slog.String("topic", topic), slog.Any("panic", r))
						}
						m.Ack()
					}()
					if err := handler(ctx, m); err != nil {
						logger.ErrorContext(ctx, "OrderManager handler execution failed", slog.String("topic", topic), slog.Any("error", err))
					}
				}(msg)
			}
		}
	}()
}
