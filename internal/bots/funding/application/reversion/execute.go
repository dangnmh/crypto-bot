package reversion

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

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
		registerStage1Subscriptions(ctx, bus, runner)
		registerStage2Subscriptions(ctx, bus, runner)
		registerStage3Subscriptions(ctx, bus, runner)
	})
}

func registerStage1Subscriptions(ctx context.Context, bus *eventbus.Bus, runner *StatelessRunner) {
	subscribeTopic(ctx, bus, runner.log, TopicReversionCandidate, func(ctx context.Context, msg *message.Message) error {
		var evt CandidateFoundEvent
		if err := json.Unmarshal(msg.Payload, &evt); err != nil {
			return err
		}
		return runner.clone(evt.Exchange, evt.ReqID).handleArm(ctx, evt)
	})
	subscribeTopic(ctx, bus, runner.log, TopicReversionArmMarketReady, func(ctx context.Context, msg *message.Message) error {
		var evt ArmMarketReadyEvent
		if err := json.Unmarshal(msg.Payload, &evt); err != nil {
			return err
		}
		return runner.clone(evt.Exchange, evt.ReqID).handleArmMarketReady(ctx, evt)
	})
	subscribeTopic(ctx, bus, runner.log, TopicReversionArmPlanCalculated, func(ctx context.Context, msg *message.Message) error {
		var evt ArmPlanCalculatedEvent
		if err := json.Unmarshal(msg.Payload, &evt); err != nil {
			return err
		}
		return runner.clone(evt.Exchange, evt.ReqID).handleArmPlanCalculated(ctx, evt)
	})
	subscribeTopic(ctx, bus, runner.log, TopicReversionSafetyChecked, func(ctx context.Context, msg *message.Message) error {
		var evt SafetyCheckedEvent
		if err := json.Unmarshal(msg.Payload, &evt); err != nil {
			return err
		}
		return runner.clone(evt.Exchange, evt.ReqID).handleSafetyChecked(ctx, evt)
	})
	subscribeTopic(ctx, bus, runner.log, TopicReversionArmed, func(ctx context.Context, msg *message.Message) error {
		var evt ArmedEvent
		if err := json.Unmarshal(msg.Payload, &evt); err != nil {
			return err
		}
		return runner.clone(evt.Exchange, evt.ReqID).handleWait(ctx, evt)
	})
	subscribeTopic(ctx, bus, runner.log, TopicReversionWaitComplete, func(ctx context.Context, msg *message.Message) error {
		var evt WaitCompleteEvent
		if err := json.Unmarshal(msg.Payload, &evt); err != nil {
			return err
		}
		return runner.clone(evt.Exchange, evt.ReqID).handleRecheck(ctx, evt)
	})
	subscribeTopic(ctx, bus, runner.log, TopicReversionConfirmed, func(ctx context.Context, msg *message.Message) error {
		var evt ConfirmedEvent
		if err := json.Unmarshal(msg.Payload, &evt); err != nil {
			return err
		}
		return runner.clone(evt.Exchange, evt.ReqID).handleFireIOC(ctx, evt)
	})
}

func registerStage2Subscriptions(ctx context.Context, bus *eventbus.Bus, runner *StatelessRunner) {
	subscribeTopic(ctx, bus, runner.log, TopicReversionFireTimingReady, func(ctx context.Context, msg *message.Message) error {
		var evt FireTimingReadyEvent
		if err := json.Unmarshal(msg.Payload, &evt); err != nil {
			return err
		}
		return runner.clone(evt.Exchange, evt.ReqID).handleFireTimingReady(ctx, evt)
	})
	subscribeTopic(ctx, bus, runner.log, TopicReversionFirePlanChecked, func(ctx context.Context, msg *message.Message) error {
		var evt FirePlanCheckedEvent
		if err := json.Unmarshal(msg.Payload, &evt); err != nil {
			return err
		}
		return runner.clone(evt.Exchange, evt.ReqID).handleFirePlanChecked(ctx, evt)
	})
	subscribeTopic(ctx, bus, runner.log, TopicReversionFireWindowReached, func(ctx context.Context, msg *message.Message) error {
		var evt FireWindowReachedEvent
		if err := json.Unmarshal(msg.Payload, &evt); err != nil {
			return err
		}
		return runner.clone(evt.Exchange, evt.ReqID).handleFireWindowReached(ctx, evt)
	})
	subscribeTopic(ctx, bus, runner.log, TopicReversionPositionWatchReady, func(ctx context.Context, msg *message.Message) error {
		var evt PositionWatchReadyEvent
		if err := json.Unmarshal(msg.Payload, &evt); err != nil {
			return err
		}
		return runner.clone(evt.Exchange, evt.ReqID).handlePositionWatchReady(ctx, evt)
	})
	subscribeTopic(ctx, bus, runner.log, TopicReversionIOCSubmitted, func(ctx context.Context, msg *message.Message) error {
		var evt IOCSubmittedEvent
		if err := json.Unmarshal(msg.Payload, &evt); err != nil {
			return err
		}
		return runner.clone(evt.Exchange, evt.ReqID).handleIOCSubmitted(ctx, evt)
	})
	subscribeTopic(ctx, bus, runner.log, TopicReversionIOCOutcomeChecked, func(ctx context.Context, msg *message.Message) error {
		var evt IOCOutcomeCheckedEvent
		if err := json.Unmarshal(msg.Payload, &evt); err != nil {
			return err
		}
		return runner.clone(evt.Exchange, evt.ReqID).handleIOCOutcomeChecked(ctx, evt)
	})
}

func registerStage3Subscriptions(ctx context.Context, bus *eventbus.Bus, runner *StatelessRunner) {
	subscribeTopic(ctx, bus, runner.log, TopicReversionTimeoutGuardScheduled, func(ctx context.Context, msg *message.Message) error {
		var evt TimeoutGuardScheduledEvent
		if err := json.Unmarshal(msg.Payload, &evt); err != nil {
			return err
		}
		return runner.clone(evt.Exchange, evt.ReqID).handleTimeoutGuardScheduled(ctx, evt)
	})
	subscribeTopic(ctx, bus, runner.log, TopicReversionTimeoutPositionChecked, func(ctx context.Context, msg *message.Message) error {
		var evt TimeoutPositionCheckedEvent
		if err := json.Unmarshal(msg.Payload, &evt); err != nil {
			return err
		}
		return runner.clone(evt.Exchange, evt.ReqID).handleTimeoutPositionChecked(ctx, evt)
	})
	subscribeTopic(ctx, bus, runner.log, TopicReversionForceCloseInitiated, func(ctx context.Context, msg *message.Message) error {
		var evt ForceCloseInitiatedEvent
		if err := json.Unmarshal(msg.Payload, &evt); err != nil {
			return err
		}
		return runner.clone(evt.Exchange, evt.ReqID).handleForceCloseInitiated(ctx, evt)
	})
	subscribeTopic(ctx, bus, runner.log, TopicReversionForceCloseCompleted, func(ctx context.Context, msg *message.Message) error {
		var evt ForceCloseCompletedEvent
		if err := json.Unmarshal(msg.Payload, &evt); err != nil {
			return err
		}
		return runner.clone(evt.Exchange, evt.ReqID).handleForceCloseCompleted(ctx, evt)
	})
	subscribeTopic(ctx, bus, runner.log, TopicReversionTimeout, func(ctx context.Context, msg *message.Message) error {
		var evt TimeoutEvent
		if err := json.Unmarshal(msg.Payload, &evt); err != nil {
			return err
		}
		return runner.clone(evt.Exchange, evt.ReqID).handleTimeout(ctx, evt)
	})
	subscribeTopic(ctx, bus, runner.log, TopicReversionPositionClosed, func(ctx context.Context, msg *message.Message) error {
		var evt struct {
			BaseReversionEvent
		}
		if err := json.Unmarshal(msg.Payload, &evt); err != nil {
			return err
		}
		return runner.clone(evt.Exchange, evt.ReqID).handleCleanup(ctx, msg)
	})
	subscribeTopic(ctx, bus, runner.log, TopicReversionAbort, func(ctx context.Context, msg *message.Message) error {
		var evt struct {
			BaseReversionEvent
		}
		if err := json.Unmarshal(msg.Payload, &evt); err != nil {
			return err
		}
		return runner.clone(evt.Exchange, evt.ReqID).handleCleanup(ctx, msg)
	})
	subscribeTopic(ctx, bus, runner.log, TopicReversionError, func(ctx context.Context, msg *message.Message) error {
		var evt struct {
			BaseReversionEvent
		}
		if err := json.Unmarshal(msg.Payload, &evt); err != nil {
			return err
		}
		return runner.clone(evt.Exchange, evt.ReqID).handleCleanup(ctx, msg)
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
