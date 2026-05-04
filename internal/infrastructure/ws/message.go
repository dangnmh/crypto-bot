package ws

import "encoding/json"

// Message represents a generic WebSocket message envelope.
// While fields match common API structures, it acts purely as a transport envelope.
type Message struct {
	Channel string          `json:"channel"`
	Symbol  string          `json:"symbol,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	Ts      int64           `json:"ts,omitempty"`
}
