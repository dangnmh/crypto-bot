package logger

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMultiHandlerFanoutAndTransforms(t *testing.T) {
	t.Parallel()

	first := &recordingHandler{enabled: true}
	second := &recordingHandler{enabled: false}
	handler := &multiHandler{handlers: []slog.Handler{first, second}}

	assert.True(t, handler.Enabled(context.Background(), slog.LevelInfo))

	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "hello", 0)
	require.NoError(t, handler.Handle(context.Background(), rec))
	assert.Equal(t, 1, first.handled)
	assert.Zero(t, second.handled)

	withAttrs := handler.WithAttrs([]slog.Attr{slog.String("component", "test")})
	require.IsType(t, &multiHandler{}, withAttrs)
	withGroup := handler.WithGroup("group")
	require.IsType(t, &multiHandler{}, withGroup)
}

func TestMultiHandlerHandleReturnsFirstError(t *testing.T) {
	t.Parallel()

	expected := errors.New("handler failed")
	handler := &multiHandler{handlers: []slog.Handler{
		&recordingHandler{enabled: true, err: expected},
		&recordingHandler{enabled: true},
	}}

	err := handler.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelWarn, "warn", 0))

	assert.ErrorIs(t, err, expected)
}

func TestMultiHandlerDisabledWhenAllChildrenDisabled(t *testing.T) {
	t.Parallel()

	handler := &multiHandler{handlers: []slog.Handler{
		&recordingHandler{},
		&recordingHandler{},
	}}

	assert.False(t, handler.Enabled(context.Background(), slog.LevelInfo))
}

type recordingHandler struct {
	enabled bool
	handled int
	err     error
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool {
	return h.enabled
}

func (h *recordingHandler) Handle(context.Context, slog.Record) error {
	h.handled++
	return h.err
}

func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler {
	return &recordingHandler{enabled: h.enabled, err: h.err}
}

func (h *recordingHandler) WithGroup(string) slog.Handler {
	return &recordingHandler{enabled: h.enabled, err: h.err}
}
