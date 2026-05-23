package tracectx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

type contextKey string

const (
	reqIDKey       contextKey = "req_id"
	cycleIDKey     contextKey = "cycle_id"
	reversionIDKey contextKey = "reversion_id"
)

// WithCorrelationID creates a new context with a correlation ID attached.
func WithCorrelationID(ctx context.Context) context.Context {
	return WithCorrelationIDValue(ctx, NewID())
}

// WithCorrelationIDValue creates a new context with a specific correlation ID.
func WithCorrelationIDValue(ctx context.Context, id string) context.Context {
	return WithReqID(ctx, id)
}

// CorrelationID extracts the correlation ID from the context.
func CorrelationID(ctx context.Context) string {
	return ReqID(ctx)
}

// WithReqID returns a child context with req_id attached.
func WithReqID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, reqIDKey, id)
}

// ReqID extracts req_id from the context.
func ReqID(ctx context.Context) string {
	if id, ok := ctx.Value(reqIDKey).(string); ok {
		return id
	}
	return ""
}

// WithCycleID returns a child context with cycle_id attached.
func WithCycleID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, cycleIDKey, id)
}

// CycleID extracts cycle_id from the context.
func CycleID(ctx context.Context) string {
	if id, ok := ctx.Value(cycleIDKey).(string); ok {
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
