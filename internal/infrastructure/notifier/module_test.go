package notifier_test

import (
	"context"
	"log/slog"
	"testing"

	"crypto-bot/internal/infrastructure/notifier"

	"github.com/stretchr/testify/require"
	"go.uber.org/fx/fxtest"
)

func TestProvideNotifier(t *testing.T) {
	t.Parallel()

	lc := fxtest.NewLifecycle(t)
	n, err := notifier.ProvideNotifier(lc, notifier.Config{Enabled: false}, slog.Default())
	require.NoError(t, err)
	require.NotNil(t, n)

	require.NoError(t, lc.Start(context.Background()))
	require.NoError(t, lc.Stop(context.Background()))
}
