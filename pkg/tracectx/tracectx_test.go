package tracectx_test

import (
	"context"
	"testing"

	"crypto-bot/pkg/tracectx"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCorrelationID(t *testing.T) {
	t.Parallel()

	ctx := tracectx.WithCorrelationID(context.Background())
	id := tracectx.CorrelationID(ctx)

	require.NotEmpty(t, id)
	assert.Len(t, id, 8)
	assert.Empty(t, tracectx.CorrelationID(context.Background()))
	assert.Equal(t, "fixed", tracectx.CorrelationID(tracectx.WithCorrelationIDValue(ctx, "fixed")))
}

func TestContextIDs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctx = tracectx.WithReqID(ctx, "req-123")
	ctx = tracectx.WithCycleID(ctx, "cyc-123")
	ctx = tracectx.WithReversionID(ctx, "rev-123")

	assert.Equal(t, "req-123", tracectx.ReqID(ctx))
	assert.Equal(t, "req-123", tracectx.CorrelationID(ctx))
	assert.Equal(t, "cyc-123", tracectx.CycleID(ctx))
	assert.Equal(t, "rev-123", tracectx.ReversionID(ctx))
	assert.Empty(t, tracectx.ReqID(context.Background()))
	assert.NotEmpty(t, tracectx.NewID())
}
