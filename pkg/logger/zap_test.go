package logger_test

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"crypto-bot/pkg/logger"

	"github.com/stretchr/testify/assert"
)

type dummyHandler struct {
	handled bool
	err     error
}

func (d *dummyHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return true
}

func (d *dummyHandler) Handle(ctx context.Context, r slog.Record) error {
	d.handled = true
	return d.err
}

func (d *dummyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return d
}

func (d *dummyHandler) WithGroup(name string) slog.Handler {
	return d
}

func TestMultiHandler(t *testing.T) {
	t.Parallel()
	h1 := &dummyHandler{}
	h2 := &dummyHandler{}

	mh := logger.NewMultiHandlerForTest(h1, h2)

	// Test Enabled
	assert.True(t, mh.Enabled(context.Background(), slog.LevelInfo))

	// Test Handle
	r := slog.Record{Level: slog.LevelInfo}
	err := mh.Handle(context.Background(), r)
	assert.NoError(t, err)
	assert.True(t, h1.handled)
	assert.True(t, h2.handled)

	// Test WithAttrs
	mhAttrs := mh.WithAttrs([]slog.Attr{slog.String("k", "v")})
	assert.NotNil(t, mhAttrs)

	// Test WithGroup
	mhGroup := mh.WithGroup("g")
	assert.NotNil(t, mhGroup)
}

func TestInitLogger(t *testing.T) {
	t.Parallel()
	// Clean up logs dir just in case
	_ = os.RemoveAll("logs")

	levels := []string{"debug", "info", "warn", "error", "unknown"}
	for _, lvl := range levels {
		cleanup := logger.InitLogger(lvl)
		assert.NotNil(t, cleanup)
		cleanup() // close file
	}
}
