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
)

// Event represents a notification event.
type Event struct {
	Level     Level
	Message   string
	Timestamp time.Time
}

// Notifier defines the interface for sending notifications.
type Notifier interface {
	// Send sends a notification event.
	Send(ctx context.Context, evt Event) error

	// Start begins the notification worker (if async).
	Start(ctx context.Context) error

	// Stop gracefully shuts down the notifier.
	Stop(ctx context.Context) error
}
