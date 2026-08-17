package notifier_test

import (
	"context"
	"log/slog"
	"testing"

	"crypto-bot/internal/infrastructure/notifier"

	"github.com/stretchr/testify/require"
)

func TestNewFromConfigDisabledReturnsNoop(t *testing.T) {
	t.Parallel()

	n, err := notifier.NewFromConfig(notifier.Config{}, slog.Default())
	require.NoError(t, err)
	require.Implements(t, (*notifier.Notifier)(nil), n)
	require.NoError(t, n.Start(context.Background()))
	require.NoError(t, n.Send(context.Background(), notifier.Event{Level: notifier.LevelInfo, Message: "ignored"}))
	require.NoError(t, n.Stop(context.Background()))
}

func TestNewFromConfigEnabledRequiresChatID(t *testing.T) {
	t.Parallel()

	_, err := notifier.NewFromConfig(notifier.Config{Enabled: true}, slog.Default())
	require.ErrorContains(t, err, "chatId is required")
}

func TestNewFromConfigPropagatesTelegramProviderError(t *testing.T) {
	t.Parallel()

	_, err := notifier.NewFromConfig(notifier.Config{
		Enabled:        true,
		TelegramChatID: "123",
	}, slog.Default())
	require.ErrorContains(t, err, "telegram bot token is required")

	_, err = notifier.NewFromConfig(notifier.Config{
		Enabled:                true,
		TelegramChatID:         "123",
		TelegramCriticalChatID: "invalid",
		TelegramBotToken:       "token",
	}, slog.Default())
	require.ErrorContains(t, err, "invalid critical_chat_id")
}
