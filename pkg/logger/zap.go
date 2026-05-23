package logger

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"crypto-bot/pkg/tracectx"
)

// multiHandler fans out log records to multiple slog handlers.
type multiHandler struct {
	handlers []slog.Handler
}

// TraceHandler wraps an existing slog.Handler and injects req_id from context.
type TraceHandler struct {
	inner slog.Handler
}

// NewTraceHandler wraps an existing slog.Handler with correlation context injection.
func NewTraceHandler(inner slog.Handler) *TraceHandler {
	return &TraceHandler{inner: inner}
}

func (h *TraceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *TraceHandler) Handle(ctx context.Context, r slog.Record) error {
	if reqID := tracectx.ReqID(ctx); reqID != "" {
		r.AddAttrs(slog.String("req_id", reqID))
	}
	if cycleID := tracectx.CycleID(ctx); cycleID != "" {
		r.AddAttrs(slog.String("cycle_id", cycleID))
	}
	if reversionID := tracectx.ReversionID(ctx); reversionID != "" {
		r.AddAttrs(slog.String("reversion_id", reversionID))
	}
	return h.inner.Handle(ctx, r)
}

func (h *TraceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &TraceHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h *TraceHandler) WithGroup(name string) slog.Handler {
	return &TraceHandler{inner: h.inner.WithGroup(name)}
}

// CtxLogger binds a slog logger to a context for log records.
//
//nolint:containedctx // This is a short-lived logging adapter created per call site, not a long-lived service struct.
type CtxLogger struct {
	ctx  context.Context
	base *slog.Logger
}

// WithCtx returns a context-bound logger. If base is nil, slog.Default is used.
//
//nolint:contextcheck // This adapter preserves the caller-provided context for immediate log calls.
func WithCtx(ctx context.Context, base *slog.Logger) *CtxLogger {
	if ctx == nil {
		ctx = context.Background()
	}
	if base == nil {
		base = slog.Default()
	}
	return &CtxLogger{ctx: ctx, base: base}
}

func (l *CtxLogger) Debug(msg string, args ...any) {
	l.base.DebugContext(l.ctx, msg, args...)
}

func (l *CtxLogger) Info(msg string, args ...any) {
	l.base.InfoContext(l.ctx, msg, args...)
}

func (l *CtxLogger) Warn(msg string, args ...any) {
	l.base.WarnContext(l.ctx, msg, args...)
}

func (l *CtxLogger) Error(msg string, args ...any) {
	l.base.ErrorContext(l.ctx, msg, args...)
}

func (m *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (m *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, h := range m.handlers {
		if h.Enabled(ctx, r.Level) {
			if err := h.Handle(ctx, r.Clone()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		handlers[i] = h.WithAttrs(attrs)
	}
	return &multiHandler{handlers: handlers}
}

func (m *multiHandler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		handlers[i] = h.WithGroup(name)
	}
	return &multiHandler{handlers: handlers}
}

func sourceReplaceAttr(groups []string, a slog.Attr) slog.Attr {
	if a.Key == slog.SourceKey {
		switch v := a.Value.Any().(type) {
		case slog.Source:
			return slog.String(a.Key, fmt.Sprintf("%s:%d", v.File, v.Line))
		case *slog.Source:
			return slog.String(a.Key, fmt.Sprintf("%s:%d", v.File, v.Line))
		}
	}
	return a
}

// InitLogger initializes the global slog logger with console (JSON) + file (JSON) output.
// All handlers are wrapped with TraceHandler to auto-inject req_id.
// Returns a cleanup function to close the log file.
func InitLogger(level string) func() {
	var slogLevel slog.Level
	switch level {
	case "debug":
		slogLevel = slog.LevelDebug
	case "info":
		slogLevel = slog.LevelInfo
	case "warn":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: slogLevel, AddSource: true, ReplaceAttr: sourceReplaceAttr}
	consoleHandler := slog.NewJSONHandler(os.Stdout, opts)

	handlers := []slog.Handler{NewTraceHandler(consoleHandler)}

	var file *os.File
	if err := os.MkdirAll("logs", 0o755); err == nil {
		file, err = os.OpenFile(
			fmt.Sprintf("logs/app-%s.jsonl", time.Now().Format("2006-01-02_15-04-05")),
			os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o666,
		)
		if err == nil {
			fileHandler := slog.NewJSONHandler(file, opts)
			handlers = append(handlers, NewTraceHandler(fileHandler))
		}
	}

	handler := &multiHandler{handlers: handlers}

	slog.SetDefault(slog.New(handler))

	if file != nil {
		return func() { _ = file.Close() }
	}
	return func() {}
}
