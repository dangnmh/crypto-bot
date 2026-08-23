package notifier

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	"crypto-bot/pkg/version"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type TelegramProvider struct {
	bot            *tgbotapi.BotAPI
	chatID         int64
	criticalChatID int64
	logger         *slog.Logger
	queue          chan Event

	startOnce sync.Once
	stopOnce  sync.Once
	mu        sync.RWMutex
	stopped   bool
}

func NewTelegramProvider(token, chatID, criticalChatID string, logger *slog.Logger) (*TelegramProvider, error) {
	if token == "" {
		return nil, fmt.Errorf("telegram bot token is required")
	}

	// Convert chatID from string to int64
	chatIDInt, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid chat_id %q: %w", chatID, err)
	}

	var criticalChatIDInt int64
	if strings.TrimSpace(criticalChatID) != "" {
		criticalChatIDInt, err = strconv.ParseInt(strings.TrimSpace(criticalChatID), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid critical_chat_id %q: %w", criticalChatID, err)
		}
	}

	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to telegram bot: %w", err)
	}

	logger.Info("Telegram notifier initialized",
		slog.String("bot_username", bot.Self.UserName),
		slog.Int64("chat_id", chatIDInt),
		slog.Int64("critical_chat_id", criticalChatIDInt),
	)

	return &TelegramProvider{
		bot:            bot,
		chatID:         chatIDInt,
		criticalChatID: criticalChatIDInt,
		logger:         logger,
		queue:          make(chan Event, 100), // Buffered to avoid blocking
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
			Level:   LevelNormal,
			Message: fmt.Sprintf("🚀 Funding Bot started successfully (version: %s, commit: %s, built: %s)", version.Version, version.Commit, version.BuildTime),
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
			Level:   LevelNormal,
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
	targetChatID := p.chatID
	if evt.Level == LevelCritical && p.criticalChatID != 0 {
		targetChatID = p.criticalChatID
	}
	msg := tgbotapi.NewMessage(targetChatID, evt.Message)
	if _, err := p.bot.Send(msg); err != nil {
		p.logger.Error("Failed to send telegram message",
			slog.Int64("chat_id", targetChatID),
			slog.Any("error", err),
			slog.Any("event", evt),
		)
	}
}
