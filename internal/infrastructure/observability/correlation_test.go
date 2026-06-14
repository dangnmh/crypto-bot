package observability_test

import (
	"context"
	"log/slog"
	"testing"

	"crypto-bot/internal/infrastructure/observability"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithRequestID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ctx = observability.WithRequestID(ctx)

	id := observability.RequestID(ctx)
	require.NotEmpty(t, id)
	assert.Len(t, id, 8)
}

func TestWithRequestIDValue(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ctx = observability.WithRequestIDValue(ctx, "custom-id")

	id := observability.RequestID(ctx)
	assert.Equal(t, "custom-id", id)
}

func TestRequestID_Empty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	id := observability.RequestID(ctx)
	assert.Empty(t, id)
}

func TestLoggerWithRequestID_WithID(t *testing.T) {
	t.Parallel()
	ctx := observability.WithRequestIDValue(context.Background(), "abc123")
	logger := observability.LoggerWithRequestID(ctx, slog.Default())
	require.NotNil(t, logger)
}

func TestLoggerWithRequestID_WithoutID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	base := slog.Default()
	logger := observability.LoggerWithRequestID(ctx, base)
	// Should return the base logger unchanged.
	assert.Equal(t, base, logger)
}

func TestGenerateID_Unique(t *testing.T) {
	t.Parallel()
	ids := make(map[string]struct{}, 100)
	for i := range 100 {
		id := observability.GenerateID()
		assert.NotContains(t, ids, id, "duplicate ID after %d iterations", i)
		ids[id] = struct{}{}
	}
}
