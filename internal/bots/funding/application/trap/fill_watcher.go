package trap

import (
	"context"
	"log/slog"

	shared "crypto-bot/internal/domain"
	applogger "crypto-bot/pkg/logger"

	"crypto-bot/internal/bots/funding/application/cycle"
	"crypto-bot/internal/bots/funding/application/events"

	"github.com/ThreeDotsLabs/watermill/message"
)

const (
	trapRetryCount          = 3
	trapMethodFallbackClose = "fallback_close"
)

func subscribeFillWatcher(ctx context.Context, rt *cycle.Runtime) {
	rt.Subscribe(ctx, events.TopicTrapOrderPlaced, func(msg *message.Message) {
		evt, err := cycle.Unmarshal[events.TrapFiredEvent](msg.Payload)
		if err != nil {
			applogger.WithCtx(ctx, rt.Log()).Error("Unmarshal TrapFiredEvent failed", slog.Any("error", err))
			return
		}
		rt.MarkTrapOrder(evt)
		setupFillWatcher(ctx, rt, evt.OrderID, evt.Side, evt.CloseSide)
	})
}

func setupFillWatcher(ctx context.Context, rt *cycle.Runtime, orderID string, side, closeSide shared.Side) {
	if orderID == "" {
		return
	}
	applogger.WithCtx(ctx, rt.Log()).Debug("Trap fill watcher armed for position update",
		slog.String("orderID", orderID),
		slog.Any("side", side),
		slog.Any("closeSide", closeSide),
	)
}
