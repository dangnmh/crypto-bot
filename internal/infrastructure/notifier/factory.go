package notifier

import (
	"context"
	"fmt"
	"log/slog"

	"crypto-bot/internal/bots/funding/config"
)

// NewFromConfig creates a Notifier based on system config and a bot token.
// Currently only Telegram is supported.
func NewFromConfig(cfg *config.SystemConfig, token string, logger *slog.Logger) (Notifier, error) {
	if !cfg.Notifier.Enabled {
		return &noopNotifier{}, nil
	}

	chatID := cfg.Notifier.ChatID
	if chatID == "" {
		return nil, fmt.Errorf("notifier.chatId is required when notifier.enabled is true")
	}

	provider, err := NewTelegramProvider(token, chatID, logger)
	if err != nil {
		return nil, err
	}

	return provider, nil
}

type noopNotifier struct{}

func (n *noopNotifier) Send(_ context.Context, _ Event) error { return nil }
func (n *noopNotifier) Start() error                          { return nil }
func (n *noopNotifier) Stop() error                           { return nil }
