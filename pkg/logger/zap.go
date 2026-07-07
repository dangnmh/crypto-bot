package logger

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"slices"
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
	if rid := tracectx.RequestID(ctx); rid != "" {
		r.AddAttrs(slog.String("request_id", rid))
	}
	return h.inner.Handle(ctx, r)
}

func (h *TraceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &TraceHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h *TraceHandler) WithGroup(name string) slog.Handler {
	return &TraceHandler{inner: h.inner.WithGroup(name)}
}

// DedupHandler wraps an existing slog.Handler and deduplicates record & bound attributes.
type DedupHandler struct {
	inner slog.Handler
	attrs []slog.Attr
}

func NewDedupHandler(inner slog.Handler) *DedupHandler {
	return &DedupHandler{inner: inner}
}

func (h *DedupHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *DedupHandler) Handle(ctx context.Context, r slog.Record) error {
	var allAttrs []slog.Attr
	allAttrs = append(allAttrs, h.attrs...)
	r.Attrs(func(a slog.Attr) bool {
		allAttrs = append(allAttrs, a)
		return true
	})

	seen := make(map[string]bool)
	var deduped []slog.Attr
	for _, a := range slices.Backward(allAttrs) {
		if a.Key == "" {
			continue
		}
		if !seen[a.Key] {
			seen[a.Key] = true
			deduped = append(deduped, a)
		}
	}

	newRecord := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	for _, d := range slices.Backward(deduped) {
		newRecord.AddAttrs(d)
	}

	return h.inner.Handle(ctx, newRecord)
}

func (h *DedupHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := make([]slog.Attr, len(h.attrs)+len(attrs))
	copy(newAttrs, h.attrs)
	copy(newAttrs[len(h.attrs):], attrs)
	return &DedupHandler{
		inner: h.inner,
		attrs: newAttrs,
	}
}

func (h *DedupHandler) WithGroup(name string) slog.Handler {
	return &DedupHandler{
		inner: h.inner.WithGroup(name),
		attrs: h.attrs,
	}
}

// CtxLogger binds a slog logger to a context for log records.
type CtxLogger struct {
	ctxVal any
	base   *slog.Logger
}

// WithCtx returns a context-bound logger. If base is nil, slog.Default is used.
func WithCtx(ctx context.Context, base *slog.Logger) *CtxLogger {
	if base == nil {
		base = slog.Default()
	}
	return &CtxLogger{ctxVal: ctx, base: base}
}

func (l *CtxLogger) Debug(msg string, args ...any) {
	l.log(slog.LevelDebug, msg, args...)
}

func (l *CtxLogger) Info(msg string, args ...any) {
	l.log(slog.LevelInfo, msg, args...)
}

func (l *CtxLogger) Warn(msg string, args ...any) {
	l.log(slog.LevelWarn, msg, args...)
}

func (l *CtxLogger) Error(msg string, args ...any) {
	l.log(slog.LevelError, msg, args...)
}

func (l *CtxLogger) log(level slog.Level, msg string, args ...any) {
	ctx, _ := l.ctxVal.(context.Context)
	if ctx == nil {
		ctx = context.Background()
	}
	if !l.base.Enabled(ctx, level) {
		return
	}
	var pcs [1]uintptr
	runtime.Callers(3, pcs[:])
	record := slog.NewRecord(time.Now(), level, msg, pcs[0])
	record.Add(args...)
	_ = l.base.Handler().Handle(ctx, record)
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
func InitLogger(level, env string) func() {
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
	consoleHandler := NewDedupHandler(slog.NewJSONHandler(os.Stdout, opts))

	handlers := []slog.Handler{NewTraceHandler(consoleHandler)}

	var file *os.File
	var err error
	if env != "prod" && env != "production" {
		if err = os.MkdirAll("logs", 0o755); err == nil {
			file, err = os.OpenFile(
				fmt.Sprintf("logs/app-%s.jsonl", time.Now().Format("2006-01-02_15-04-05")),
				os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o666,
			)
			if err == nil {
				fileHandler := NewDedupHandler(slog.NewJSONHandler(file, opts))
				handlers = append(handlers, NewTraceHandler(fileHandler))
			}
		}
	}

	handler := &multiHandler{handlers: handlers}

	slog.SetDefault(slog.New(handler))

	if file != nil {
		return func() { _ = file.Close() }
	}
	return func() {}
}
