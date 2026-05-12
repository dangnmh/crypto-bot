package eventbus

import (
	"encoding/json"
	"time"
)

// LogEntry represents a single event recorded in the bus timeline.
type LogEntry struct {
	Time    time.Time       `json:"time"`
	Topic   string          `json:"topic"`
	MsgID   string          `json:"msg_id"`
	Payload json.RawMessage `json:"payload"`
}
