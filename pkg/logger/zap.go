package logger

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"crypto-bot/internal/infrastructure/observability"
)

// multiHandler fans out log records to multiple slog handlers.
type multiHandler struct {
	handlers []slog.Handler
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

// InitLogger initializes the global slog logger with console (JSON) + file (JSON) output.
// All handlers are wrapped with TraceHandler to auto-inject trace_id, span_id, req_id.
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

	opts := &slog.HandlerOptions{Level: slogLevel, AddSource: true}
	consoleHandler := slog.NewJSONHandler(os.Stdout, opts)

	handlers := []slog.Handler{observability.NewTraceHandler(consoleHandler)}

	var file *os.File
	if err := os.MkdirAll("logs", 0o755); err == nil {
		file, err = os.OpenFile(
			fmt.Sprintf("logs/app-%s.jsonl", time.Now().Format("2006-01-02_15-04-05")),
			os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o666,
		)
		if err == nil {
			fileHandler := slog.NewJSONHandler(file, opts)
			handlers = append(handlers, observability.NewTraceHandler(fileHandler))
		}
	}

	handler := &multiHandler{handlers: handlers}

	slog.SetDefault(slog.New(handler))

	if file != nil {
		return func() { _ = file.Close() }
	}
	return func() {}
}
