package tracectx_test

import (
	"context"
	"testing"

	"crypto-bot/pkg/tracectx"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestID(t *testing.T) {
	t.Parallel()

	ctx := tracectx.WithRequestID(context.Background())
	id := tracectx.RequestID(ctx)

	require.NotEmpty(t, id)
	assert.Len(t, id, 8)
	assert.Empty(t, tracectx.RequestID(context.Background()))
	assert.Equal(t, "fixed", tracectx.RequestID(tracectx.WithRequestIDValue(ctx, "fixed")))
}

func TestContextIDs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctx = tracectx.WithRequestIDValue(ctx, "req-123")

	assert.Equal(t, "req-123", tracectx.RequestID(ctx))
	assert.NotEmpty(t, tracectx.NewID())
}
