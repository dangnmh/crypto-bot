package notifier

import (
	"context"
	"time"
)

// Level defines notification severity.
type Level string

const (
	LevelCritical Level = "CRITICAL"
	LevelNormal   Level = "NORMAL"
	LevelInfo     Level = "INFO"
)

// Supported notification colors.
const (
	ColorYellow = "yellow"
	ColorGreen  = "green"
	ColorRed    = "red"
	ColorBlue   = "blue"
)

// Event represents a notification event.
type Event struct {
	Level     Level
	Strategy  string
	Exchange  string
	Symbol    string
	Message   string
	Color     string
	Data      map[string]any
	Timestamp time.Time
	IsRaw     bool
}

// Notifier defines the interface for sending notifications.
type Notifier interface {
	// Send sends a notification event.
	Send(ctx context.Context, evt Event) error

	// SendRawMsg sends a raw string message directly without automatic formatting headers.
	SendRawMsg(ctx context.Context, msg string) error

	// Start begins the notification worker (if async).
	Start(ctx context.Context) error

	// Stop gracefully shuts down the notifier.
	Stop(ctx context.Context) error
}
