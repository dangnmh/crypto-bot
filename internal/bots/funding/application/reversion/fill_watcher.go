package reversion

import (
	"context"
	"log/slog"

	"crypto-bot/internal/bots/funding/application/cycle"
	"crypto-bot/internal/bots/funding/application/events"
	applogger "crypto-bot/pkg/logger"

	"github.com/ThreeDotsLabs/watermill/message"
)

func subscribeFillWatcher(ctx context.Context, rt *cycle.Runtime) {
	rt.Subscribe(ctx, events.TopicReversionIOCFired, func(msg *message.Message) {
		evt, err := cycle.Unmarshal[events.IOCFiredEvent](msg.Payload)
		if err != nil {
			applogger.WithCtx(ctx, rt.Log()).Error("Unmarshal IOCFiredEvent failed", slog.Any("error", err))
			return
		}
		if evt.OrderID == "" || evt.Error != "" {
			return
		}
		setupFillWatcher(ctx, rt, evt)
	})
}

func setupFillWatcher(ctx context.Context, rt *cycle.Runtime, evt events.IOCFiredEvent) {
	applogger.WithCtx(ctx, rt.Log()).Debug("Reversion fill watcher armed for position update", slog.String("orderID", evt.OrderID))
	rt.MarkReversionOrderEvent(evt)
}
