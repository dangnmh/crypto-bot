package tracectx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

type contextKey string

const (
	correlationIDKey contextKey = "correlation_id"
	reversionIDKey   contextKey = "reversion_id"
)

// WithCorrelationID creates a new context with a correlation ID attached.
func WithCorrelationID(ctx context.Context) context.Context {
	return WithCorrelationIDValue(ctx, NewID())
}

// WithCorrelationIDValue creates a new context with a specific correlation ID.
func WithCorrelationIDValue(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationIDKey, id)
}

// CorrelationID extracts the correlation ID from the context.
func CorrelationID(ctx context.Context) string {
	if id, ok := ctx.Value(correlationIDKey).(string); ok {
		return id
	}
	return ""
}

// WithReversionID returns a child context with reversion_id attached.
func WithReversionID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, reversionIDKey, id)
}

// ReversionID extracts reversion_id from the context.
func ReversionID(ctx context.Context) string {
	if id, ok := ctx.Value(reversionIDKey).(string); ok {
		return id
	}
	return ""
}

// NewID creates a short random hex ID for log correlation fields.
func NewID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
