package observability

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// ──────────────────────────────────────────────────────────────────────
// TraceHandler — slog handler that injects OTel trace context into logs
// ──────────────────────────────────────────────────────────────────────.

// TraceHandler wraps an existing slog.Handler and automatically adds
// trace_id, span_id, and req_id attributes to every log record if
// the context carries an active OTel span or a correlation ID.
//
// This means ALL log lines within a traced span automatically get
// the trace identifiers without any manual effort.
//
// Example log output:
//
//	{"time":"...","level":"INFO","msg":"cycle start","trace_id":"abc123","span_id":"def456","req_id":"a1b2c3d4"}
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

// Handle injects trace_id, span_id, and req_id into the log record before
// delegating to the inner handler.
func (h *TraceHandler) Handle(ctx context.Context, r slog.Record) error {
	// Inject OTel trace context if present.
	span := trace.SpanFromContext(ctx)
	if span.SpanContext().IsValid() {
		sc := span.SpanContext()
		r.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}

	// Inject req_id (our correlation ID) if present.
	if reqID := CorrelationID(ctx); reqID != "" {
		r.AddAttrs(slog.String("req_id", reqID))
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
