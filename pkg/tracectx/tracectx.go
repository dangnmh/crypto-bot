package tracectx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

type contextKey string

const (
	requestIDKey contextKey = "request_id"
)

// WithRequestID creates a new context with a request ID attached.
func WithRequestID(ctx context.Context) context.Context {
	return WithRequestIDValue(ctx, NewID())
}

// WithRequestIDValue creates a new context with a specific request ID.
func WithRequestIDValue(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestID extracts the request ID from the context.
func RequestID(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// NewID creates a short random hex ID for log request fields.
func NewID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
