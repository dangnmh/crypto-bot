package observability_test

import (
	"context"
	"testing"

	"crypto-bot/internal/infrastructure/observability"

	"github.com/stretchr/testify/require"
	"go.uber.org/fx/fxtest"
)

func TestInitMetrics(t *testing.T) {
	t.Parallel()

	lc := fxtest.NewLifecycle(t)
	handler, err := observability.InitMetrics(lc)
	require.NoError(t, err)
	require.NotNil(t, handler)

	ctx := context.Background()
	require.NoError(t, lc.Start(ctx))
	require.NoError(t, lc.Stop(ctx))
}
