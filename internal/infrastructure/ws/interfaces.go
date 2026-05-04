package ws

import "time"

// Subscriber handles channel subscription/unsubscription.
// Satisfied by *Client. Enables mock-based testing of executor.
type Subscriber interface {
	Subscribe(symbol, channel string) error
	Unsubscribe(symbol, channel string) error
	SubscribeDepth(symbol, step string) error
	UnsubscribeDepth(symbol, step string) error
}

// OrderNotifier handles order fill callbacks.
// Satisfied by *Client. Enables mock-based testing of executor.
type OrderNotifier interface {
	OnOrderUpdate(orderID string, timeout time.Duration, callback func(WsOrderDeal))
	RemoveOrderCallback(orderID string)
}

// Compile-time interface compliance checks.
var (
	_ Subscriber    = (*Client)(nil)
	_ OrderNotifier = (*Client)(nil)
)
