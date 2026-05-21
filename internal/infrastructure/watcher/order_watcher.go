package watcher

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/eventbus"
)

// OrderNotifier handles order fill callbacks.
type OrderNotifier interface {
	OnOrderUpdate(ctx context.Context, orderID string, timeout time.Duration, callback func(exchange.WsOrderDeal))
	OnOrderDeal(ctx context.Context, orderID string, timeout time.Duration, callback func(exchange.PersonalOrderDeal))
	OnOrderDealBySymbolSide(
		ctx context.Context,
		symbol string,
		side int,
		timeout time.Duration,
		callback func(exchange.PersonalOrderDeal),
	)
	OnTrackOrderUpdate(
		ctx context.Context,
		trackID string,
		orderID string,
		timeout time.Duration,
		callback func(exchange.PersonalTrackOrderUpdate),
	)
	OnPositionUpdate(ctx context.Context, symbol string, timeout time.Duration, callback func(exchange.PersonalPositionUpdate))
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

// Publish broadcasts an order lifecycle update to all subscribers of the specific OrderID.
func (w *OrderWatcher) Publish(deal exchange.WsOrderDeal) {
	w.PublishOrder(deal)
}

// PublishOrder broadcasts an order lifecycle update.
func (w *OrderWatcher) PublishOrder(deal exchange.WsOrderDeal) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	topic := deal.GetOrderID()
	w.logger.Debug("📢 Publishing order lifecycle", "orderID", topic, "state", deal.State)
	if err := w.broker.Publish(topic, deal); err != nil {
		w.logger.Error("Failed to publish order lifecycle", "orderID", topic, "error", err)
	}
	if err := w.broker.Publish(orderTopic(topic), deal); err != nil {
		w.logger.Error("Failed to publish order lifecycle", "orderID", topic, "error", err)
	}
}

// PublishDeal broadcasts an execution deal by order ID and by symbol+side.
func (w *OrderWatcher) PublishDeal(deal exchange.PersonalOrderDeal) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	orderID := deal.GetOrderID()
	w.logger.Debug("📢 Publishing order execution deal", "orderID", orderID, "symbol", deal.Symbol, "side", deal.Side)
	if orderID != "" {
		if err := w.broker.Publish(orderDealTopic(orderID), deal); err != nil {
			w.logger.Error("Failed to publish order execution deal", "orderID", orderID, "error", err)
		}
	}
	if deal.Symbol != "" && deal.Side != 0 {
		if err := w.broker.Publish(symbolSideDealTopic(deal.Symbol, deal.Side), deal); err != nil {
			w.logger.Error("Failed to publish symbol-side execution deal",
				"symbol", deal.Symbol,
				"side", deal.Side,
				"error", err,
			)
		}
	}
}

// PublishTrackOrder broadcasts a trailing order update by track ID and child order ID.
func (w *OrderWatcher) PublishTrackOrder(update exchange.PersonalTrackOrderUpdate) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	trackID := update.GetID()
	if trackID != "" {
		if err := w.broker.Publish(trackTopic(trackID), update); err != nil {
			w.logger.Error("Failed to publish track order update", "trackID", trackID, "error", err)
		}
	}
	orderID := update.GetOrderID()
	if orderID != "" {
		if err := w.broker.Publish(trackOrderTopic(orderID), update); err != nil {
			w.logger.Error("Failed to publish track order update", "orderID", orderID, "error", err)
		}
	}
}

// PublishPosition broadcasts a position update by symbol.
func (w *OrderWatcher) PublishPosition(update exchange.PersonalPositionUpdate) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if update.Symbol == "" {
		return
	}
	if err := w.broker.Publish(positionTopic(update.Symbol), update); err != nil {
		w.logger.Error("Failed to publish position update", "symbol", update.Symbol, "error", err)
	}
}

// OnOrderUpdate registers a callback for a specific order ID (implements OrderNotifier).
// The callback will be removed automatically after the specified timeout.
func (w *OrderWatcher) OnOrderUpdate(parent context.Context, orderID string, timeout time.Duration, callback func(exchange.WsOrderDeal)) {
	subscribe(parent, w, orderID, timeout, "order lifecycle", callback)
}

func (w *OrderWatcher) OnOrderDeal(
	parent context.Context,
	orderID string,
	timeout time.Duration,
	callback func(exchange.PersonalOrderDeal),
) {
	subscribe(parent, w, orderDealTopic(orderID), timeout, "order deal", callback)
}

func (w *OrderWatcher) OnOrderDealBySymbolSide(
	parent context.Context,
	symbol string,
	side int,
	timeout time.Duration,
	callback func(exchange.PersonalOrderDeal),
) {
	subscribe(parent, w, symbolSideDealTopic(symbol, side), timeout, "symbol-side order deal", callback)
}

func (w *OrderWatcher) OnTrackOrderUpdate(
	parent context.Context,
	trackID string,
	orderID string,
	timeout time.Duration,
	callback func(exchange.PersonalTrackOrderUpdate),
) {
	if trackID != "" {
		subscribe(parent, w, trackTopic(trackID), timeout, "track order", callback)
	}
	if orderID != "" {
		subscribe(parent, w, trackOrderTopic(orderID), timeout, "track order", callback)
	}
}

func (w *OrderWatcher) OnPositionUpdate(
	parent context.Context,
	symbol string,
	timeout time.Duration,
	callback func(exchange.PersonalPositionUpdate),
) {
	subscribe(parent, w, positionTopic(symbol), timeout, "position", callback)
}

// RemoveOrderCallback is essentially a no-op since OnOrderUpdate cleans itself up.
func (w *OrderWatcher) RemoveOrderCallback(orderID string) {
	// Not strictly needed with the pubsub architecture where OnOrderUpdate manages its own lifecycle.
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
		w.logger.Error("Failed to subscribe to watcher topic", "topic", topic, "label", label, "error", err)
		cancel()
		return
	}

	w.logger.Debug("📥 Subscribing to watcher topic", "topic", topic, "label", label)

	go func() {
		defer cancel()
		defer w.logger.Debug("📤 Unsubscribed from watcher topic", "topic", topic, "label", label)

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
					w.logger.Error("Failed to unmarshal watcher payload", "topic", topic, "label", label, "error", err)
				}
				msg.Ack()
			}
		}
	}()
}

func orderTopic(orderID string) string {
	return "order:" + orderID
}

func orderDealTopic(orderID string) string {
	return "deal:order:" + orderID
}

func symbolSideDealTopic(symbol string, side int) string {
	return fmt.Sprintf("deal:symbol_side:%s:%d", symbol, side)
}

func trackTopic(trackID string) string {
	return "track:" + trackID
}

func trackOrderTopic(orderID string) string {
	return "track:order:" + orderID
}

func positionTopic(symbol string) string {
	return "position:" + symbol
}
