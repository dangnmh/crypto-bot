package watcher

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/eventbus"
	applogger "crypto-bot/pkg/logger"
)

// OrderNotifier handles position lifecycle callbacks.
type OrderNotifier interface {
	OnPositionUpdate(ctx context.Context, symbol string, timeout time.Duration, callback func(exchange.PersonalPositionUpdate))
}

// OrderWatcher provides a thread-safe publish-subscribe mechanism for personal position updates.
type OrderWatcher struct {
	mu           sync.RWMutex
	broker       *eventbus.Bus
	logger       *slog.Logger
	exchangeName string
}

// NewOrderWatcher creates a new OrderWatcher wrapping a shared event bus.
func NewOrderWatcher(bus *eventbus.Bus, exchangeName string, logger *slog.Logger) *OrderWatcher {
	return &OrderWatcher{
		broker:       bus,
		exchangeName: exchangeName,
		logger:       logger,
	}
}

// PublishPosition broadcasts a position update by symbol.
func (w *OrderWatcher) PublishPosition(update exchange.PersonalPositionUpdate) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if update.Symbol == "" {
		return
	}
	if err := w.broker.Publish(w.positionTopic(update.Symbol), update); err != nil {
		w.logger.Error("Failed to publish position update", slog.String("symbol", update.Symbol), slog.Any("error", err))
	}
}

func (w *OrderWatcher) OnPositionUpdate(
	parent context.Context,
	symbol string,
	timeout time.Duration,
	callback func(exchange.PersonalPositionUpdate),
) {
	subscribe(parent, w, w.positionTopic(symbol), timeout, "position", callback)
}

func subscribe[T any](
	parent context.Context,
	w *OrderWatcher,
	topic string,
	timeout time.Duration,
	label string,
	callback func(T),
) {
	if topic == "" {
		return
	}
	ctx, cancel := context.WithTimeout(parent, timeout)

	ch, err := w.broker.Subscribe(ctx, topic)
	if err != nil {
		applogger.WithCtx(ctx, w.logger).Error("Failed to subscribe to watcher topic", slog.String("topic", topic), slog.String("label", label), slog.Any("error", err))
		cancel()
		return
	}

	applogger.WithCtx(ctx, w.logger).Debug("📥 Subscribing to watcher topic", slog.String("topic", topic), slog.String("label", label))

	go func() {
		defer cancel()
		defer applogger.WithCtx(ctx, w.logger).Debug("📤 Unsubscribed from watcher topic", slog.String("topic", topic), slog.String("label", label))

		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}

				var value T
				if err := json.Unmarshal(msg.Payload, &value); err == nil {
					callback(value)
				} else {
					applogger.WithCtx(ctx, w.logger).Error("Failed to unmarshal watcher payload", slog.String("topic", topic), slog.String("label", label), slog.Any("error", err))
				}
				msg.Ack()
			}
		}
	}()
}

func (w *OrderWatcher) positionTopic(symbol string) string {
	if w.exchangeName != "" {
		return "position:" + w.exchangeName + ":" + symbol
	}
	return "position:" + symbol
}
