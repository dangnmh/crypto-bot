package reversion

import (
	"context"

	"github.com/ThreeDotsLabs/watermill/message"

	"crypto-bot/internal/bots/funding/application/cycle"
	"crypto-bot/internal/bots/funding/application/events"
	"crypto-bot/internal/infrastructure/observability"
)

const (
	reversionReasonNoFill        = "no_fill"
	reversionMethodFallbackClose = "fallback_close"
)

func Register(ctx context.Context, rt *cycle.Runtime) {
	ctx = observability.WithReversionID(ctx)
	subscribeArm(ctx, rt)
	subscribeWait(ctx, rt)
	subscribeRecheck(ctx, rt)
	subscribeFireIOC(ctx, rt)
	subscribeFillWatcher(ctx, rt)
	subscribeTimeoutGuard(ctx, rt)
	subscribeRecover(ctx, rt)
}

func subscribeRecover(ctx context.Context, rt *cycle.Runtime) {
	rt.Subscribe(ctx, events.TopicReversionOrderFilled, func(msg *message.Message) {
		_, err := cycle.Unmarshal[events.OrderFilledEvent](msg.Payload)
		if err != nil {
			return
		}
	})
}
