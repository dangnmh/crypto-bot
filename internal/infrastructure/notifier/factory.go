package notifier

import (
	"context"
	"fmt"
	"log/slog"
)

// Config contains provider-agnostic notification settings.
type Config struct {
	Enabled                bool
	TelegramBotToken       string
	TelegramChatID         string
	TelegramCriticalChatID string
}

// NewFromConfig creates a Notifier based on system config and a bot token.
// Currently only Telegram is supported.
func NewFromConfig(cfg Config, logger *slog.Logger) (Notifier, error) {
	if !cfg.Enabled {
		return &noopNotifier{}, nil
	}

	chatID := cfg.TelegramChatID
	if chatID == "" {
		return nil, fmt.Errorf("notifier.chatId is required when notifier.enabled is true")
	}

	provider, err := NewTelegramProvider(cfg.TelegramBotToken, chatID, cfg.TelegramCriticalChatID, logger)
	if err != nil {
		return nil, err
	}

	return provider, nil
}

type noopNotifier struct{}

func (n *noopNotifier) Send(_ context.Context, _ Event) error        { return nil }
func (n *noopNotifier) SendRawMsg(_ context.Context, _ string) error { return nil }
func (n *noopNotifier) Start(_ context.Context) error                { return nil }
func (n *noopNotifier) Stop(_ context.Context) error                 { return nil }
