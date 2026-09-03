package watcher

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/eventbus"
)

// OrderNotifier handles position and trade lifecycle callbacks.
type OrderNotifier interface {
	OnPositionUpdate(ctx context.Context, symbol string, timeout time.Duration, callback func(exchange.PersonalPositionUpdate))
	OnTradeUpdate(ctx context.Context, symbol string, timeout time.Duration, callback func([]domain.PublicTrade))
}

// OrderWatcher provides a thread-safe publish-subscribe mechanism for personal position and trade updates.
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

// PublishTrades broadcasts public trades by symbol.
func (w *OrderWatcher) PublishTrades(symbol string, trades []domain.PublicTrade) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if symbol == "" || len(trades) == 0 {
		return
	}
	if err := w.broker.Publish(w.tradeTopic(symbol), trades); err != nil {
		w.logger.Error("Failed to publish trade update", slog.String("symbol", symbol), slog.Any("error", err))
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

func (w *OrderWatcher) OnTradeUpdate(
	parent context.Context,
	symbol string,
	timeout time.Duration,
	callback func([]domain.PublicTrade),
) {
	subscribe(parent, w, w.tradeTopic(symbol), timeout, "trade", callback)
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
		w.logger.ErrorContext(ctx, "Failed to subscribe to watcher topic", slog.String("topic", topic), slog.String("label", label), slog.Any("error", err))
		cancel()
		return
	}

	w.logger.DebugContext(ctx, "📥 Subscribing to watcher topic", slog.String("topic", topic), slog.String("label", label))

	go func() {
		defer cancel()
		defer w.logger.DebugContext(ctx, "📤 Unsubscribed from watcher topic", slog.String("topic", topic), slog.String("label", label))

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
					w.logger.ErrorContext(ctx, "Failed to unmarshal watcher payload", slog.String("topic", topic), slog.String("label", label), slog.Any("error", err))
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

func (w *OrderWatcher) tradeTopic(symbol string) string {
	if w.exchangeName != "" {
		return "trade:" + w.exchangeName + ":" + symbol
	}
	return "trade:" + symbol
}
