package logger_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"crypto-bot/pkg/logger"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMultiHandler_Enabled(t *testing.T) {
	t.Parallel()

	debugHandler := slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelDebug})
	errorHandler := slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelError})
	mh := logger.NewMultiHandlerForTest(debugHandler, errorHandler)

	// Debug should be enabled because debugHandler accepts it.
	assert.True(t, mh.Enabled(context.Background(), slog.LevelDebug))
	// Info also enabled via debugHandler.
	assert.True(t, mh.Enabled(context.Background(), slog.LevelInfo))
}

func TestMultiHandler_Enabled_NoneMatch(t *testing.T) {
	t.Parallel()

	errorOnly := slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelError})
	mh := logger.NewMultiHandlerForTest(errorOnly)

	assert.False(t, mh.Enabled(context.Background(), slog.LevelDebug))
	assert.False(t, mh.Enabled(context.Background(), slog.LevelInfo))
	assert.True(t, mh.Enabled(context.Background(), slog.LevelError))
}

func TestMultiHandler_Handle(t *testing.T) {
	t.Parallel()

	var buf1, buf2 bytes.Buffer
	h1 := slog.NewTextHandler(&buf1, &slog.HandlerOptions{Level: slog.LevelInfo})
	h2 := slog.NewTextHandler(&buf2, &slog.HandlerOptions{Level: slog.LevelInfo})
	mh := logger.NewMultiHandlerForTest(h1, h2)

	lg := slog.New(mh)
	lg.Info("test message", "key", "value")

	assert.Contains(t, buf1.String(), "test message")
	assert.Contains(t, buf2.String(), "test message")
}

func TestMultiHandler_WithAttrs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	mh := logger.NewMultiHandlerForTest(h)

	withAttrs := mh.WithAttrs([]slog.Attr{slog.String("env", "test")})
	require.NotNil(t, withAttrs)

	lg := slog.New(withAttrs)
	lg.Info("attrs test")

	assert.Contains(t, buf.String(), "env=test")
}

func TestMultiHandler_WithGroup(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	mh := logger.NewMultiHandlerForTest(h)

	withGroup := mh.WithGroup("mygroup")
	require.NotNil(t, withGroup)

	lg := slog.New(withGroup)
	lg.Info("group test", "key", "val")

	assert.Contains(t, buf.String(), "mygroup.key=val")
}

func TestInitLogger_ReturnsCleanup(t *testing.T) {
	t.Parallel()
	// Not parallel — modifies global logger.
	cleanup := logger.InitLogger("debug")
	require.NotNil(t, cleanup)
	cleanup()
}
