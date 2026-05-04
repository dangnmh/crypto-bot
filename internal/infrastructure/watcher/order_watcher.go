package watcher

import (
	"log/slog"
	"sync"
	"time"

	"crypto-bot/internal/infrastructure/exchange"

	"github.com/cskr/pubsub"
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
	broker *pubsub.PubSub
	logger *slog.Logger
}

// NewOrderWatcher creates a new OrderWatcher wrapping a shared event bus.
func NewOrderWatcher(bus *pubsub.PubSub, logger *slog.Logger) *OrderWatcher {
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
	w.broker.Pub(deal, topic)
}

// Subscribe returns a channel that receives updates for a specific OrderID.
func (w *OrderWatcher) Subscribe(orderID string) chan interface{} {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.logger.Debug("📥 Subscribing to order updates", "orderID", orderID)
	return w.broker.Sub(orderID)
}

// Unsubscribe removes the subscription for a specific OrderID.
func (w *OrderWatcher) Unsubscribe(ch chan interface{}, orderID string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.logger.Debug("📤 Unsubscribing from order updates", "orderID", orderID)
	w.broker.Unsub(ch, orderID)
}

// OnOrderUpdate registers a callback for a specific order ID (implements OrderNotifier).
// The callback will be removed automatically after the specified timeout.
func (w *OrderWatcher) OnOrderUpdate(orderID string, timeout time.Duration, callback func(exchange.WsOrderDeal)) {
	ch := w.Subscribe(orderID)

	go func() {
		defer w.Unsubscribe(ch, orderID)
		select {
		case msg := <-ch:
			if deal, ok := msg.(exchange.WsOrderDeal); ok {
				callback(deal)
			}
		case <-time.After(timeout):
			// Timeout, do nothing
		}
	}()
}

// RemoveOrderCallback is essentially a no-op since OnOrderUpdate cleans itself up.
func (w *OrderWatcher) RemoveOrderCallback(orderID string) {
	// Not strictly needed with the pubsub architecture where OnOrderUpdate manages its own lifecycle.
}
