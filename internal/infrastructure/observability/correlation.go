package observability

import (
	"context"
	"log/slog"

	"crypto-bot/pkg/tracectx"
)

// WithRequestID creates a new context with a request ID attached.
func WithRequestID(ctx context.Context) context.Context {
	return tracectx.WithRequestID(ctx)
}

// WithRequestIDValue creates a new context with a specific request ID.
func WithRequestIDValue(ctx context.Context, id string) context.Context {
	return tracectx.WithRequestIDValue(ctx, id)
}

// RequestID extracts the request ID from the context.
// Returns empty string if not set.
func RequestID(ctx context.Context) string {
	return tracectx.RequestID(ctx)
}

// LoggerWithRequestID creates a slog.Logger with the request ID from context attached.
func LoggerWithRequestID(ctx context.Context, base *slog.Logger) *slog.Logger {
	rid := RequestID(ctx)
	if rid == "" {
		return base
	}
	return base.With("request_id", rid)
}
