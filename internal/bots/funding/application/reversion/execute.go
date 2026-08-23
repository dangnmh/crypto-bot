package reversion

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"

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
}

func registerEventSubscription[T ReversionEvent](
	ctx context.Context,
	runner *StatelessRunner,
	topic string,
	action func(context.Context, *StatelessRunner, T) error,
) {
	subscribeTopic(ctx, runner.bus, runner.log, topic, func(msgCtx context.Context, msg *message.Message) error {
		var evt T
		if err := json.Unmarshal(msg.Payload, &evt); err != nil {
			return err
		}
		exch := evt.GetExchange()
		reqID := evt.GetReqID()
		symbol := evt.GetSymbol()
		traceCtx := observability.WithRequestIDValue(msgCtx, reqID)
		clonedRunner := runner.clone(exch, reqID, symbol)
		return action(traceCtx, clonedRunner, evt)
	})
}

func subscribeTopic(subCtx context.Context, bus *eventbus.Bus, logger *slog.Logger, topic string, handler func(context.Context, *message.Message) error) {
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
	if err := handler(msgCtx, m); err != nil {
		if errors.Is(err, ErrFRBelowThreshold) || errors.Is(err, ErrFRSignFlip) {
			logger.InfoContext(msgCtx, "Handler execution skipped (expected abort)", slog.String("topic", topic), slog.Any("error", err))
		} else {
			logger.ErrorContext(msgCtx, "Handler execution failed", slog.String("topic", topic), slog.Any("error", err))
		}
	}
	m.Ack()
}
