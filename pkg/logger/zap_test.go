package logger_test

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crypto-bot/pkg/logger"
	"crypto-bot/pkg/tracectx"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTraceHandlerInjectsContextIDs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	handler := logger.NewTraceHandler(slog.NewTextHandler(&buf, nil))
	l := slog.New(handler)

	ctx := tracectx.WithRequestIDValue(context.Background(), "req-123")
	logger.WithCtx(ctx, l).Info("hello")

	out := buf.String()
	assert.True(t, handler.Enabled(ctx, slog.LevelInfo))
	assert.Contains(t, out, "request_id=req-123")
	assert.Contains(t, out, "msg=hello")
}

func TestTraceHandlerAttrsAndGroups(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	handler := logger.NewTraceHandler(slog.NewTextHandler(&buf, nil)).
		WithAttrs([]slog.Attr{slog.String("component", "test")}).
		WithGroup("group")

	require.NoError(t, handler.Handle(context.Background(), slog.NewRecord(
		time.Now(),
		slog.LevelInfo,
		"msg",
		0,
	)))

	assert.True(t, strings.Contains(buf.String(), "component=test"))
}

func TestCtxLoggerLevelMethodsAndDefaults(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	base := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := tracectx.WithRequestIDValue(context.Background(), "req-1")
	l := logger.WithCtx(ctx, base)

	l.Debug("debug message")
	l.Info("info message")
	l.Warn("warn message")
	l.Error("error message")

	out := buf.String()
	assert.Contains(t, out, "debug message")
	assert.Contains(t, out, "info message")
	assert.Contains(t, out, "warn message")
	assert.Contains(t, out, "error message")

	require.NotNil(t, logger.WithCtx(nil, nil)) //nolint:staticcheck // Verifies the logger's nil-context fallback.
}

func TestCtxLogger_DisabledLevel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	// Set log level to Info, so Debug is disabled
	base := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	l := logger.WithCtx(context.Background(), base)

	l.Debug("this should be ignored")
	assert.Empty(t, buf.String())
}

func TestCtxLoggerReportsCallerSource(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	base := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{AddSource: true}))

	logger.WithCtx(context.Background(), base).Info("source check")

	out := buf.String()
	assert.Contains(t, out, "zap_test.go")
	assert.NotContains(t, out, "zap.go")
}

//nolint:paralleltest // Mutates global slog default logger, cannot run in parallel
func TestInitLoggerCreatesLogFileAndCleanup(t *testing.T) {
	before, err := filepath.Glob(filepath.Join("logs", "app-*.jsonl"))
	require.NoError(t, err)
	seen := make(map[string]struct{}, len(before))
	for _, path := range before {
		seen[path] = struct{}{}
	}

	cleanup := logger.InitLogger("debug")
	slog.Info("test log")
	cleanup()

	files, err := filepath.Glob(filepath.Join("logs", "app-*.jsonl"))
	require.NoError(t, err)
	var created []string
	for _, path := range files {
		if _, ok := seen[path]; !ok {
			created = append(created, path)
		}
	}
	require.NotEmpty(t, created)
	for _, path := range created {
		require.NoError(t, os.Remove(path))
	}
}

//nolint:paralleltest // Mutates global slog default logger, cannot run in parallel
func TestInitLoggerLevelsAndSourceReplaceAttr(t *testing.T) {
	levels := []string{"info", "warn", "error", "unknown"}
	for _, l := range levels {
		cleanup := logger.InitLogger(l)
		cleanup()
	}
}
