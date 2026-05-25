package reversion

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

	"crypto-bot/internal/bots/funding/application"
	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/pkg/eventbus"
	applogger "crypto-bot/pkg/logger"

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
		subscribeTopic(ctx, bus, deps.Log, TopicReversionArmed, runner.handleWaitMessage)
		subscribeTopic(ctx, bus, deps.Log, TopicReversionWaitComplete, runner.handleRecheckMessage)
		subscribeTopic(ctx, bus, deps.Log, TopicReversionConfirmed, runner.handleFireIOCMessage)
		subscribeTopic(ctx, bus, deps.Log, TopicReversionIOCFired, runner.handleIOCFiredMessage)
		subscribeTopic(ctx, bus, deps.Log, TopicReversionCheckTimeout, runner.handleCheckTimeoutMessage)
		subscribeTopic(ctx, bus, deps.Log, TopicReversionPositionClosed, runner.handleCleanupMessage)
		subscribeTopic(ctx, bus, deps.Log, TopicReversionAbort, runner.handleCleanupMessage)
		subscribeTopic(ctx, bus, deps.Log, TopicReversionError, runner.handleCleanupMessage)
	})
}

func subscribeTopic(ctx context.Context, bus *eventbus.Bus, logger *slog.Logger, topic string, handler func(context.Context, *message.Message) error) {
	ch, err := bus.Subscribe(ctx, topic)
	if err != nil {
		applogger.WithCtx(ctx, logger).Error("Failed to subscribe to topic", slog.String("topic", topic), slog.Any("error", err))
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
					applogger.WithCtx(ctx, logger).Error("Handler execution failed", slog.String("topic", topic), slog.Any("error", err))
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

func (r *StatelessRunner) handleIOCFiredMessage(ctx context.Context, msg *message.Message) error {
	var evt IOCFiredEvent
	if err := json.Unmarshal(msg.Payload, &evt); err != nil {
		return err
	}
	return r.handleIOCFired(ctx, evt)
}

func (r *StatelessRunner) handleCleanupMessage(ctx context.Context, msg *message.Message) error {
	return r.handleCleanup(ctx, msg)
}

func (r *StatelessRunner) handleCheckTimeoutMessage(ctx context.Context, msg *message.Message) error {
	var evt CheckTimeoutEvent
	if err := json.Unmarshal(msg.Payload, &evt); err != nil {
		return err
	}
	return r.handleCheckTimeout(ctx, evt)
}
