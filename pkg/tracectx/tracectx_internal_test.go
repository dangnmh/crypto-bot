package tracectx

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIDAccessorsIgnoreNonStringValues(t *testing.T) {
	t.Parallel()

	ctx := context.WithValue(context.Background(), cycleIDKey, 123)
	ctx = context.WithValue(ctx, reversionIDKey, struct{}{})

	require.Empty(t, CycleID(ctx))
	require.Empty(t, ReversionID(ctx))
}
