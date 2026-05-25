package observability_test

import (
	"context"
	"log/slog"
	"testing"

	"crypto-bot/internal/infrastructure/observability"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTraceHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		fn   func(t *testing.T)
	}{
		{
			name: "Enabled delegates to inner",
			fn: func(t *testing.T) {
				th := observability.NewTraceHandler(slog.Default().Handler())
				assert.True(t, th.Enabled(context.Background(), slog.LevelInfo))
			},
		},
		{
			name: "Handle without span succeeds",
			fn: func(t *testing.T) {
				th := observability.NewTraceHandler(slog.Default().Handler())
				r := slog.NewRecord(time.Now(), slog.LevelInfo, "test", 0)
				assert.NoError(t, th.Handle(context.Background(), r))
			},
		},
		{
			name: "Handle with trace IDs succeeds",
			fn: func(t *testing.T) {
				th := observability.NewTraceHandler(slog.Default().Handler())
				ctx := observability.WithCorrelationIDValue(context.Background(), "req-123")
				ctx = observability.WithReversionIDValue(ctx, "rev-123")
				r := slog.NewRecord(time.Now(), slog.LevelInfo, "test", 0)
				assert.NoError(t, th.Handle(ctx, r))
				assert.Equal(t, "rev-123", observability.ReversionID(ctx))
			},
		},
		{
			name: "WithAttrs returns non-nil handler",
			fn: func(t *testing.T) {
				th := observability.NewTraceHandler(slog.Default().Handler())
				h2 := th.WithAttrs([]slog.Attr{slog.String("key", "val")})
				require.NotNil(t, h2)
				_, ok := h2.(*observability.TraceHandler)
				assert.True(t, ok, "should return *observability.TraceHandler")
			},
		},
		{
			name: "WithGroup returns non-nil handler",
			fn: func(t *testing.T) {
				th := observability.NewTraceHandler(slog.Default().Handler())
				h2 := th.WithGroup("grp")
				require.NotNil(t, h2)
				_, ok := h2.(*observability.TraceHandler)
				assert.True(t, ok, "should return *observability.TraceHandler")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.fn(t)
		})
	}
}
