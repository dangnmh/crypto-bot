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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCorrelationID(t *testing.T) {
	t.Parallel()

	ctx := logger.WithCorrelationID(context.Background())
	id := logger.CorrelationID(ctx)

	require.NotEmpty(t, id)
	assert.Len(t, id, 8)
	assert.Empty(t, logger.CorrelationID(context.Background()))
	assert.Equal(t, "fixed", logger.CorrelationID(logger.WithCorrelationIDValue(ctx, "fixed")))
}

func TestTraceHandlerInjectsReqID(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	handler := logger.NewTraceHandler(slog.NewTextHandler(&buf, nil))
	l := slog.New(handler)

	ctx := logger.WithCorrelationIDValue(context.Background(), "req-123")
	l.InfoContext(ctx, "hello")

	out := buf.String()
	assert.True(t, handler.Enabled(ctx, slog.LevelInfo))
	assert.Contains(t, out, "req_id=req-123")
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

func TestInitLoggerCreatesLogFileAndCleanup(t *testing.T) {
	t.Parallel()

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
