package notifier_test

import (
	"log/slog"
	"testing"

	"crypto-bot/internal/infrastructure/notifier"

	"github.com/stretchr/testify/require"
)

func TestNewTelegramProviderValidatesInputs(t *testing.T) {
	t.Parallel()

	_, err := notifier.NewTelegramProvider("", "123", slog.Default())
	require.ErrorContains(t, err, "telegram bot token is required")
}
