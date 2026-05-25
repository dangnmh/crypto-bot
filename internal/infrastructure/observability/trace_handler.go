package observability

import (
	"context"
	"log/slog"

	"crypto-bot/pkg/tracectx"
)

// ──────────────────────────────────────────────────────────────────────
// TraceHandler — slog handler that injects correlation context into logs
// ──────────────────────────────────────────────────────────────────────.

// TraceHandler wraps an existing slog.Handler and automatically adds
// tracing attributes to every log record if the context carries them.
//
// Example log output:
//
//	{"time":"...","level":"INFO","msg":"cycle start","req_id":"a1b2c3d4"}
type TraceHandler struct {
	inner slog.Handler
}

// NewTraceHandler wraps an existing slog.Handler with trace context injection.
func NewTraceHandler(inner slog.Handler) *TraceHandler {
	return &TraceHandler{inner: inner}
}

// Enabled delegates to the inner handler.
func (h *TraceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *TraceHandler) Handle(ctx context.Context, r slog.Record) error {
	if cid := tracectx.CorrelationID(ctx); cid != "" {
		r.AddAttrs(slog.String("correlation_id", cid))
	}
	if reversionID := tracectx.ReversionID(ctx); reversionID != "" {
		r.AddAttrs(slog.String("reversion_id", reversionID))
	}

	return h.inner.Handle(ctx, r)
}

// WithAttrs delegates to the inner handler.
func (h *TraceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &TraceHandler{inner: h.inner.WithAttrs(attrs)}
}

// WithGroup delegates to the inner handler.
func (h *TraceHandler) WithGroup(name string) slog.Handler {
	return &TraceHandler{inner: h.inner.WithGroup(name)}
}
