package notifier

import (
	"context"
	"fmt"
	"log/slog"

	"crypto-bot/internal/bots/funding/config"
)

// NewFromConfig creates a Notifier based on system config and a bot token.
// Currently only Telegram is supported.
func NewFromConfig(cfg *config.SystemConfig, logger *slog.Logger) (Notifier, error) {
	if !cfg.NotiConfig.Enabled {
		return &noopNotifier{}, nil
	}

	chatID := cfg.NotiConfig.TelegramChatID
	if chatID == "" {
		return nil, fmt.Errorf("notifier.chatId is required when notifier.enabled is true")
	}

	provider, err := NewTelegramProvider(cfg.NotiConfig.TelegramBotToken, chatID, logger)
	if err != nil {
		return nil, err
	}

	return provider, nil
}

type noopNotifier struct{}

func (n *noopNotifier) Send(_ context.Context, _ Event) error { return nil }
func (n *noopNotifier) Start(_ context.Context) error         { return nil }
func (n *noopNotifier) Stop(_ context.Context) error          { return nil }
