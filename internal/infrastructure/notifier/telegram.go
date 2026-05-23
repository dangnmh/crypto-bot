package notifier

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const telegramInfoPrefix = "🔵 [INFO]"

type TelegramProvider struct {
	bot    *tgbotapi.BotAPI
	chatID int64
	logger *slog.Logger
	queue  chan Event
}

func NewTelegramProvider(token, chatID string, logger *slog.Logger) (*TelegramProvider, error) {
	if token == "" {
		return nil, fmt.Errorf("telegram bot token is required")
	}

	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to telegram bot: %w", err)
	}

	// Convert chatID from string to int64
	chatIDInt, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid chat_id %q: %w", chatID, err)
	}

	logger.Info("Telegram notifier initialized", slog.String("bot_username", bot.Self.UserName))

	return &TelegramProvider{
		bot:    bot,
		chatID: chatIDInt,
		logger: logger,
		queue:  make(chan Event, 100), // Buffered to avoid blocking
	}, nil
}

func (p *TelegramProvider) Send(ctx context.Context, evt Event) error {
	select {
	case p.queue <- evt:
		return nil
	default:
		return fmt.Errorf("notifier queue full, dropping message")
	}
}

func (p *TelegramProvider) Start(ctx context.Context) error {
	go func() {
		_ = p.Send(ctx, Event{
			Level:   LevelInfo,
			Message: "🚀 Funding Bot started successfully",
		})
	}()
	go func() {
		for evt := range p.queue {
			msg := tgbotapi.NewMessage(p.chatID, p.formatMessage(evt))
			if _, err := p.bot.Send(msg); err != nil {
				p.logger.Error("Failed to send telegram message", slog.Any("error", err), slog.Any("event", evt))
			}
		}
	}()
	return nil
}

func (p *TelegramProvider) Stop(ctx context.Context) error {
	go func() {
		_ = p.Send(ctx, Event{
			Level:   LevelInfo,
			Message: "🛑 Funding Bot stopped",
		})
	}()
	close(p.queue)
	return nil
}

func (p *TelegramProvider) formatMessage(evt Event) string {
	prefix := telegramInfoPrefix
	switch evt.Level {
	case LevelCritical:
		prefix = "🔴 [CRITICAL]"
	case LevelTrading:
		prefix = "🟡 [TRADING]"
	case LevelInfo:
		prefix = telegramInfoPrefix
	}

	symbol := ""
	if evt.Symbol != "" {
		symbol = fmt.Sprintf(" [%s]", evt.Symbol)
	}

	return fmt.Sprintf("%s%s\n%s", prefix, symbol, evt.Message)
}
