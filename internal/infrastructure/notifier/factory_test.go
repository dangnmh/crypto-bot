package notifier

import (
	"context"
	"log/slog"
	"testing"

	fundingconfig "crypto-bot/internal/bots/funding/config"
	sysconfig "crypto-bot/internal/infrastructure/config"

	"github.com/stretchr/testify/require"
)

func TestNewFromConfigDisabledReturnsNoop(t *testing.T) {
	t.Parallel()

	n, err := NewFromConfig(&fundingconfig.SystemConfig{}, slog.Default())
	require.NoError(t, err)
	require.IsType(t, &noopNotifier{}, n)
	require.NoError(t, n.Start(context.Background()))
	require.NoError(t, n.Send(context.Background(), Event{Level: LevelInfo, Message: "ignored"}))
	require.NoError(t, n.Stop(context.Background()))
}

func TestNewFromConfigEnabledRequiresChatID(t *testing.T) {
	t.Parallel()

	_, err := NewFromConfig(&fundingconfig.SystemConfig{
		SystemConfig: sysconfig.SystemConfig{
			NotiConfig: sysconfig.NotiConfig{Enabled: true},
		},
	}, slog.Default())
	require.ErrorContains(t, err, "chatId is required")
}

func TestNewFromConfigPropagatesTelegramProviderError(t *testing.T) {
	t.Parallel()

	_, err := NewFromConfig(&fundingconfig.SystemConfig{
		SystemConfig: sysconfig.SystemConfig{
			NotiConfig: sysconfig.NotiConfig{
				Enabled:        true,
				TelegramChatID: "123",
			},
		},
	}, slog.Default())
	require.ErrorContains(t, err, "telegram bot token is required")
}
