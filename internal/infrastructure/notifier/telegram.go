package notifier

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"

	"crypto-bot/pkg/formatutil"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type TelegramProvider struct {
	bot    *tgbotapi.BotAPI
	chatID int64
	logger *slog.Logger
	queue  chan Event

	startOnce sync.Once
	stopOnce  sync.Once
	mu        sync.RWMutex
	stopped   bool
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
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.stopped {
		return nil
	}

	select {
	case p.queue <- evt:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return fmt.Errorf("notifier queue full, dropping message")
	}
}

func (p *TelegramProvider) Start(ctx context.Context) error {
	p.startOnce.Do(func() {
		_ = p.Send(ctx, Event{
			Level:   LevelInfo,
			Message: "🚀 Funding Bot started successfully",
		})

		go func() {
			for evt := range p.queue {
				p.sendTelegram(evt)
			}
		}()
	})
	return nil
}

func (p *TelegramProvider) Stop(ctx context.Context) error {
	p.stopOnce.Do(func() {
		p.sendTelegram(Event{
			Level:   LevelInfo,
			Message: "🛑 Funding Bot stopped",
		})

		p.mu.Lock()
		p.stopped = true
		close(p.queue)
		p.mu.Unlock()
	})
	return nil
}

func (p *TelegramProvider) sendTelegram(evt Event) {
	if p.bot == nil {
		return
	}
	msg := tgbotapi.NewMessage(p.chatID, p.formatMessage(evt))
	if _, err := p.bot.Send(msg); err != nil {
		p.logger.Error("Failed to send telegram message", slog.Any("error", err), slog.Any("event", evt))
	}
}

func (p *TelegramProvider) formatMessage(evt Event) string {
	color := evt.Color
	if color == "" {
		color = ColorYellow
	}

	var emoji string
	switch color {
	case ColorGreen:
		emoji = "🟢"
	case ColorRed:
		emoji = "🔴"
	case ColorBlue:
		emoji = "🔵"
	case ColorYellow:
		emoji = "🟡"
	default:
		emoji = "🟡"
	}

	prefix := fmt.Sprintf("%s [%s]", emoji, evt.Level)

	contextLabel := ""
	if evt.Exchange != "" {
		contextLabel = fmt.Sprintf(" [%s]", evt.Exchange)
	}
	if evt.Symbol != "" {
		contextLabel = fmt.Sprintf("%s [%s]", contextLabel, evt.Symbol)
	}

	data := ""
	for k, v := range evt.Data {
		valStr := fmt.Sprintf("%v", v)
		switch val := v.(type) {
		case float64:
			valStr = formatutil.FormatFloatMax4(val)
		case float32:
			valStr = formatutil.FormatFloatMax4(float64(val))
		}
		data = fmt.Sprintf("%s\n%s: %s", data, k, valStr)
	}

	return fmt.Sprintf("%s%s\n%s\n%s", prefix, contextLabel, evt.Message, data)
}
