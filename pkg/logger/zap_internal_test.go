package logger

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type mockHandler struct {
	enabled bool
	err     error
}

func (m *mockHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return m.enabled
}

func (m *mockHandler) Handle(ctx context.Context, r slog.Record) error {
	return m.err
}

func (m *mockHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return m
}

func (m *mockHandler) WithGroup(name string) slog.Handler {
	return m
}

func TestMultiHandler_Internal(t *testing.T) {
	t.Parallel()

	mh := &multiHandler{
		handlers: []slog.Handler{
			&mockHandler{enabled: true, err: errors.New("mock error")},
		},
	}

	assert.True(t, mh.Enabled(context.Background(), slog.LevelInfo))

	// Test multiHandler Handle error branch
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "test", 0)
	err := mh.Handle(context.Background(), record)
	assert.Error(t, err)

	// Test multiHandler with disabled sub-handlers
	mhDisabled := &multiHandler{
		handlers: []slog.Handler{
			&mockHandler{enabled: false},
		},
	}
	assert.False(t, mhDisabled.Enabled(context.Background(), slog.LevelInfo))
	assert.NoError(t, mhDisabled.Handle(context.Background(), record))

	// Test multiHandler WithAttrs and WithGroup
	mh2 := mh.WithAttrs(nil)
	assert.NotNil(t, mh2)

	mh3 := mh.WithGroup("test")
	assert.NotNil(t, mh3)
}

func TestSourceReplaceAttr_Internal(t *testing.T) {
	t.Parallel()

	// 1. Test non-source key
	a := slog.String("key", "val")
	res := sourceReplaceAttr(nil, a)
	assert.Equal(t, a, res)

	// 2. Test source key with slog.Source value
	src := slog.Source{File: "main.go", Line: 10}
	aSrc := slog.Any(slog.SourceKey, src)
	resSrc := sourceReplaceAttr(nil, aSrc)
	assert.Equal(t, slog.String(slog.SourceKey, "main.go:10"), resSrc)

	// 3. Test source key with *slog.Source value
	resSrcPtr := sourceReplaceAttr(nil, slog.Any(slog.SourceKey, &src))
	assert.Equal(t, slog.String(slog.SourceKey, "main.go:10"), resSrcPtr)

	// 4. Test source key with non-source value (fallback)
	aNotSrc := slog.Any(slog.SourceKey, "not-a-source")
	resNotSrc := sourceReplaceAttr(nil, aNotSrc)
	assert.Equal(t, aNotSrc, resNotSrc)

	// 5. Test duration key formatting
	aDur := slog.Duration("latency", 600224543*time.Nanosecond)
	resDur := sourceReplaceAttr(nil, aDur)
	assert.Equal(t, slog.String("latency", "600.224543ms"), resDur)
}
