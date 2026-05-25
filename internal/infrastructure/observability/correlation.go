package observability

import (
	"context"
	"log/slog"

	"crypto-bot/pkg/tracectx"
)

// WithCorrelationID creates a new context with a correlation ID attached.
func WithCorrelationID(ctx context.Context) context.Context {
	return tracectx.WithCorrelationID(ctx)
}

// WithCorrelationIDValue creates a new context with a specific correlation ID.
func WithCorrelationIDValue(ctx context.Context, id string) context.Context {
	return tracectx.WithCorrelationIDValue(ctx, id)
}

// CorrelationID extracts the correlation ID from the context.
// Returns empty string if not set.
func CorrelationID(ctx context.Context) string {
	return tracectx.CorrelationID(ctx)
}

func WithReversionID(ctx context.Context) context.Context {
	return WithReversionIDValue(ctx, tracectx.NewID())
}

func WithReversionIDValue(ctx context.Context, id string) context.Context {
	return tracectx.WithReversionID(ctx, id)
}

func ReversionID(ctx context.Context) string {
	return tracectx.ReversionID(ctx)
}

// LoggerWithCorrelation creates a slog.Logger with the correlation ID from context attached.
func LoggerWithCorrelation(ctx context.Context, base *slog.Logger) *slog.Logger {
	cid := CorrelationID(ctx)
	if cid == "" {
		return base
	}
	return base.With("cid", cid)
}
