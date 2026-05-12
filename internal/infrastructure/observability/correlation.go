package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
)

type contextKey string

const correlationIDKey contextKey = "correlation_id"

// WithCorrelationID creates a new context with a correlation ID attached.
func WithCorrelationID(ctx context.Context) context.Context {
	return context.WithValue(ctx, correlationIDKey, generateID())
}

// WithCorrelationIDValue creates a new context with a specific correlation ID.
func WithCorrelationIDValue(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationIDKey, id)
}

// CorrelationID extracts the correlation ID from the context.
// Returns empty string if not set.
func CorrelationID(ctx context.Context) string {
	if id, ok := ctx.Value(correlationIDKey).(string); ok {
		return id
	}
	return ""
}

// LoggerWithCorrelation creates a slog.Logger with the correlation ID from context attached.
func LoggerWithCorrelation(ctx context.Context, base *slog.Logger) *slog.Logger {
	cid := CorrelationID(ctx)
	if cid == "" {
		return base
	}
	return base.With("cid", cid)
}

// generateID creates a short random hex ID (8 chars = 4 bytes).
func generateID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
