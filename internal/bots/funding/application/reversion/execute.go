package reversion

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

	"crypto-bot/internal/bots/funding/application"
	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/pkg/eventbus"

	"github.com/ThreeDotsLabs/watermill/message"
)

var busRegistrations sync.Map

// InitGlobalSubscriptions registers all reversion topic handlers on the global event bus EXACTLY ONCE per Bus instance.
func InitGlobalSubscriptions(ctx context.Context, deps application.Deps, globalCfg *config.Config) {
	bus := deps.EventBus
	if bus == nil {
		return
	}

	onceVal, _ := busRegistrations.LoadOrStore(bus, &sync.Once{})
	once, ok := onceVal.(*sync.Once)
	if !ok {
		return
	}

	once.Do(func() {
		runner := &StatelessRunner{
			deps:      deps,
			globalCfg: globalCfg,
			bus:       bus,
			log:       deps.Log.With("flow", FlowReversion),
		}

		subscribeTopic(ctx, bus, deps.Log, TopicReversionCandidate, runner.handleArmMessage)
		subscribeTopic(ctx, bus, deps.Log, TopicReversionArmMarketReady, runner.handleArmMarketReadyMessage)
		subscribeTopic(ctx, bus, deps.Log, TopicReversionArmPlanCalculated, runner.handleArmPlanCalculatedMessage)
		subscribeTopic(ctx, bus, deps.Log, TopicReversionSafetyChecked, runner.handleSafetyCheckedMessage)
		subscribeTopic(ctx, bus, deps.Log, TopicReversionArmed, runner.handleWaitMessage)
		subscribeTopic(ctx, bus, deps.Log, TopicReversionWaitComplete, runner.handleRecheckMessage)
		subscribeTopic(ctx, bus, deps.Log, TopicReversionConfirmed, runner.handleFireIOCMessage)
		subscribeTopic(ctx, bus, deps.Log, TopicReversionFireTimingReady, runner.handleFireTimingReadyMessage)
		subscribeTopic(ctx, bus, deps.Log, TopicReversionFirePlanChecked, runner.handleFirePlanCheckedMessage)
		subscribeTopic(ctx, bus, deps.Log, TopicReversionFireWindowReached, runner.handleFireWindowReachedMessage)
		subscribeTopic(ctx, bus, deps.Log, TopicReversionPositionWatchReady, runner.handlePositionWatchReadyMessage)
		subscribeTopic(ctx, bus, deps.Log, TopicReversionIOCSubmitted, runner.handleIOCSubmittedMessage)
		subscribeTopic(ctx, bus, deps.Log, TopicReversionIOCOutcomeChecked, runner.handleIOCOutcomeCheckedMessage)
		subscribeTopic(ctx, bus, deps.Log, TopicReversionTimeoutGuardScheduled, runner.handleTimeoutGuardScheduledMessage)
		subscribeTopic(ctx, bus, deps.Log, TopicReversionTimeoutPositionChecked, runner.handleTimeoutPositionCheckedMessage)
		subscribeTopic(ctx, bus, deps.Log, TopicReversionForceCloseInitiated, runner.handleForceCloseInitiatedMessage)
		subscribeTopic(ctx, bus, deps.Log, TopicReversionForceCloseCompleted, runner.handleForceCloseCompletedMessage)
		subscribeTopic(ctx, bus, deps.Log, TopicReversionTimeout, runner.handleTimeoutMessage)
		subscribeTopic(ctx, bus, deps.Log, TopicReversionPositionClosed, runner.handleCleanupMessage)
		subscribeTopic(ctx, bus, deps.Log, TopicReversionAbort, runner.handleCleanupMessage)
		subscribeTopic(ctx, bus, deps.Log, TopicReversionError, runner.handleCleanupMessage)
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
				if err := handler(ctx, msg); err != nil {
					logger.ErrorContext(ctx, "Handler execution failed", slog.String("topic", topic), slog.Any("error", err))
				}
				msg.Ack()
			}
		}
	}()
}

func (r *StatelessRunner) handleArmMessage(ctx context.Context, msg *message.Message) error {
	var evt CandidateFoundEvent
	if err := json.Unmarshal(msg.Payload, &evt); err != nil {
		return err
	}
	return r.handleArm(ctx, evt)
}

func (r *StatelessRunner) handleArmMarketReadyMessage(ctx context.Context, msg *message.Message) error {
	var evt ArmMarketReadyEvent
	if err := json.Unmarshal(msg.Payload, &evt); err != nil {
		return err
	}
	return r.handleArmMarketReady(ctx, evt)
}

func (r *StatelessRunner) handleArmPlanCalculatedMessage(ctx context.Context, msg *message.Message) error {
	var evt ArmPlanCalculatedEvent
	if err := json.Unmarshal(msg.Payload, &evt); err != nil {
		return err
	}
	return r.handleArmPlanCalculated(ctx, evt)
}

func (r *StatelessRunner) handleSafetyCheckedMessage(ctx context.Context, msg *message.Message) error {
	var evt SafetyCheckedEvent
	if err := json.Unmarshal(msg.Payload, &evt); err != nil {
		return err
	}
	return r.handleSafetyChecked(ctx, evt)
}

func (r *StatelessRunner) handleWaitMessage(ctx context.Context, msg *message.Message) error {
	var evt ArmedEvent
	if err := json.Unmarshal(msg.Payload, &evt); err != nil {
		return err
	}
	return r.handleWait(ctx, evt)
}

func (r *StatelessRunner) handleRecheckMessage(ctx context.Context, msg *message.Message) error {
	var evt WaitCompleteEvent
	if err := json.Unmarshal(msg.Payload, &evt); err != nil {
		return err
	}
	return r.handleRecheck(ctx, evt)
}

func (r *StatelessRunner) handleFireIOCMessage(ctx context.Context, msg *message.Message) error {
	var evt ConfirmedEvent
	if err := json.Unmarshal(msg.Payload, &evt); err != nil {
		return err
	}
	return r.handleFireIOC(ctx, evt)
}

func (r *StatelessRunner) handleFireTimingReadyMessage(ctx context.Context, msg *message.Message) error {
	var evt FireTimingReadyEvent
	if err := json.Unmarshal(msg.Payload, &evt); err != nil {
		return err
	}
	return r.handleFireTimingReady(ctx, evt)
}

func (r *StatelessRunner) handleFirePlanCheckedMessage(ctx context.Context, msg *message.Message) error {
	var evt FirePlanCheckedEvent
	if err := json.Unmarshal(msg.Payload, &evt); err != nil {
		return err
	}
	return r.handleFirePlanChecked(ctx, evt)
}

func (r *StatelessRunner) handleFireWindowReachedMessage(ctx context.Context, msg *message.Message) error {
	var evt FireWindowReachedEvent
	if err := json.Unmarshal(msg.Payload, &evt); err != nil {
		return err
	}
	return r.handleFireWindowReached(ctx, evt)
}

func (r *StatelessRunner) handlePositionWatchReadyMessage(ctx context.Context, msg *message.Message) error {
	var evt PositionWatchReadyEvent
	if err := json.Unmarshal(msg.Payload, &evt); err != nil {
		return err
	}
	return r.handlePositionWatchReady(ctx, evt)
}

func (r *StatelessRunner) handleIOCSubmittedMessage(ctx context.Context, msg *message.Message) error {
	var evt IOCSubmittedEvent
	if err := json.Unmarshal(msg.Payload, &evt); err != nil {
		return err
	}
	return r.handleIOCSubmitted(ctx, evt)
}

func (r *StatelessRunner) handleIOCOutcomeCheckedMessage(ctx context.Context, msg *message.Message) error {
	var evt IOCOutcomeCheckedEvent
	if err := json.Unmarshal(msg.Payload, &evt); err != nil {
		return err
	}
	return r.handleIOCOutcomeChecked(ctx, evt)
}

func (r *StatelessRunner) handleCleanupMessage(ctx context.Context, msg *message.Message) error {
	return r.handleCleanup(ctx, msg)
}

func (r *StatelessRunner) handleTimeoutGuardScheduledMessage(ctx context.Context, msg *message.Message) error {
	var evt TimeoutGuardScheduledEvent
	if err := json.Unmarshal(msg.Payload, &evt); err != nil {
		return err
	}
	return r.handleTimeoutGuardScheduled(ctx, evt)
}

func (r *StatelessRunner) handleTimeoutPositionCheckedMessage(ctx context.Context, msg *message.Message) error {
	var evt TimeoutPositionCheckedEvent
	if err := json.Unmarshal(msg.Payload, &evt); err != nil {
		return err
	}
	return r.handleTimeoutPositionChecked(ctx, evt)
}

func (r *StatelessRunner) handleForceCloseInitiatedMessage(ctx context.Context, msg *message.Message) error {
	var evt ForceCloseInitiatedEvent
	if err := json.Unmarshal(msg.Payload, &evt); err != nil {
		return err
	}
	return r.handleForceCloseInitiated(ctx, evt)
}

func (r *StatelessRunner) handleForceCloseCompletedMessage(ctx context.Context, msg *message.Message) error {
	var evt ForceCloseCompletedEvent
	if err := json.Unmarshal(msg.Payload, &evt); err != nil {
		return err
	}
	return r.handleForceCloseCompleted(ctx, evt)
}

func (r *StatelessRunner) handleTimeoutMessage(ctx context.Context, msg *message.Message) error {
	var evt TimeoutEvent
	if err := json.Unmarshal(msg.Payload, &evt); err != nil {
		return err
	}
	return r.handleTimeout(ctx, evt)
}
