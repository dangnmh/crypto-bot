package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"

	pkglogger "crypto-bot/pkg/logger"
)

// WithCorrelationID creates a new context with a correlation ID attached.
func WithCorrelationID(ctx context.Context) context.Context {
	return pkglogger.WithCorrelationIDValue(ctx, generateID())
}

// WithCorrelationIDValue creates a new context with a specific correlation ID.
func WithCorrelationIDValue(ctx context.Context, id string) context.Context {
	return pkglogger.WithCorrelationIDValue(ctx, id)
}

// CorrelationID extracts the correlation ID from the context.
// Returns empty string if not set.
func CorrelationID(ctx context.Context) string {
	return pkglogger.CorrelationID(ctx)
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
