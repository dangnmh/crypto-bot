package watcher

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/eventbus"
)

// OrderNotifier handles order fill callbacks.
type OrderNotifier interface {
	OnOrderUpdate(orderID string, timeout time.Duration, callback func(exchange.WsOrderDeal))
	RemoveOrderCallback(orderID string)
}

// OrderWatcher provides a thread-safe publish-subscribe mechanism for personal order updates.
// It allows independent bots or FSMs to subscribe to specific order executions.
type OrderWatcher struct {
	mu     sync.RWMutex
	broker *eventbus.Bus
	logger *slog.Logger
}

// NewOrderWatcher creates a new OrderWatcher wrapping a shared event bus.
func NewOrderWatcher(bus *eventbus.Bus, logger *slog.Logger) *OrderWatcher {
	return &OrderWatcher{
		broker: bus,
		logger: logger,
	}
}

// Publish broadcasts an order deal to all subscribers of the specific OrderID.
func (w *OrderWatcher) Publish(deal exchange.WsOrderDeal) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	topic := deal.GetOrderID()
	w.logger.Debug("📢 Publishing order deal", "orderID", topic, "state", deal.State)
	if err := w.broker.Publish(topic, deal); err != nil {
		w.logger.Error("Failed to publish order deal", "orderID", topic, "error", err)
	}
}

// OnOrderUpdate registers a callback for a specific order ID (implements OrderNotifier).
// The callback will be removed automatically after the specified timeout.
func (w *OrderWatcher) OnOrderUpdate(orderID string, timeout time.Duration, callback func(exchange.WsOrderDeal)) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)

	ch, err := w.broker.Subscribe(ctx, orderID)
	if err != nil {
		w.logger.Error("Failed to subscribe to order updates", "orderID", orderID, "error", err)
		cancel()
		return
	}

	w.logger.Debug("📥 Subscribing to order updates", "orderID", orderID)

	go func() {
		defer cancel()
		defer w.logger.Debug("📤 Unsubscribed from order updates", "orderID", orderID)

		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}

				var deal exchange.WsOrderDeal
				if err := json.Unmarshal(msg.Payload, &deal); err == nil {
					callback(deal)
				} else {
					w.logger.Error("Failed to unmarshal order deal", "orderID", orderID, "error", err)
				}
				msg.Ack()
			}
		}
	}()
}

// RemoveOrderCallback is essentially a no-op since OnOrderUpdate cleans itself up.
func (w *OrderWatcher) RemoveOrderCallback(orderID string) {
	// Not strictly needed with the pubsub architecture where OnOrderUpdate manages its own lifecycle.
}
