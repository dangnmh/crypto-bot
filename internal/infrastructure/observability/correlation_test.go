package observability_test

import (
	"context"
	"log/slog"
	"testing"

	"crypto-bot/internal/infrastructure/observability"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithCorrelationID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ctx = observability.WithCorrelationID(ctx)

	id := observability.CorrelationID(ctx)
	require.NotEmpty(t, id)
	assert.Len(t, id, 8)
}

func TestWithCorrelationIDValue(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ctx = observability.WithCorrelationIDValue(ctx, "custom-id")

	id := observability.CorrelationID(ctx)
	assert.Equal(t, "custom-id", id)
}

func TestCorrelationID_Empty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	id := observability.CorrelationID(ctx)
	assert.Empty(t, id)
}

func TestLoggerWithCorrelation_WithID(t *testing.T) {
	t.Parallel()
	ctx := observability.WithCorrelationIDValue(context.Background(), "abc123")
	logger := observability.LoggerWithCorrelation(ctx, slog.Default())
	require.NotNil(t, logger)
}

func TestLoggerWithCorrelation_WithoutID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	base := slog.Default()
	logger := observability.LoggerWithCorrelation(ctx, base)
	// Should return the base logger unchanged.
	assert.Equal(t, base, logger)
}

func TestGenerateID_Unique(t *testing.T) {
	t.Parallel()
	ids := make(map[string]struct{}, 100)
	for i := 0; i < 100; i++ {
		id := observability.GenerateID()
		assert.NotContains(t, ids, id, "duplicate ID after %d iterations", i)
		ids[id] = struct{}{}
	}
}
