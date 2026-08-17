package notifier

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	"crypto-bot/pkg/formatutil"
	"crypto-bot/pkg/version"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const (
	keyVolUSDT24h  = "volusdt24h"
	keyFundingRate = "fundingRate"
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

func (p *TelegramProvider) SendRawMsg(ctx context.Context, msg string) error {
	return p.Send(ctx, Event{
		Message: msg,
		IsRaw:   true,
	})
}

func (p *TelegramProvider) Start(ctx context.Context) error {
	p.startOnce.Do(func() {
		_ = p.Send(ctx, Event{
			Level:   LevelInfo,
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
	targetChatID := p.chatID
	if evt.Level == LevelCritical && p.criticalChatID != 0 {
		targetChatID = p.criticalChatID
	}
	msg := tgbotapi.NewMessage(targetChatID, p.formatMessage(evt))
	if _, err := p.bot.Send(msg); err != nil {
		p.logger.Error("Failed to send telegram message",
			slog.Int64("chat_id", targetChatID),
			slog.Any("error", err),
			slog.Any("event", evt),
		)
	}
}

func getEmoji(color string) string {
	switch color {
	case ColorGreen:
		return "🟢"
	case ColorRed:
		return "🔴"
	case ColorBlue:
		return "🔵"
	default:
		return "🟡"
	}
}

//nolint:cyclop // Formats telegram message based on event attributes
func (p *TelegramProvider) formatMessage(evt Event) string {
	if evt.IsRaw {
		return evt.Message
	}

	color := evt.Color
	if color == "" {
		color = ColorYellow
	}

	prefix := fmt.Sprintf("%s [%s]", getEmoji(color), evt.Level)

	contextLabel := ""
	if evt.Strategy != "" {
		contextLabel = fmt.Sprintf(" [%s]", strings.ToUpper(evt.Strategy))
	}
	if evt.Exchange != "" {
		contextLabel = fmt.Sprintf("%s [%s]", contextLabel, strings.ToLower(evt.Exchange))
	}
	if evt.Symbol != "" {
		contextLabel = fmt.Sprintf("%s [%s]", contextLabel, evt.Symbol)
	}

	data := ""
	for k, v := range evt.Data {
		valStr := fmt.Sprintf("%v", v)
		switch val := v.(type) {
		case float64:
			switch k {
			case keyVolUSDT24h:
				valStr = formatutil.FormatCompactUSD(val)
			case keyFundingRate:
				valStr = formatutil.FormatFloatMax4(val*100) + "%"
			default:
				valStr = formatutil.FormatFloatMax4(val)
			}
		case float32:
			switch k {
			case keyVolUSDT24h:
				valStr = formatutil.FormatCompactUSD(float64(val))
			case keyFundingRate:
				valStr = formatutil.FormatFloatMax4(float64(val)*100) + "%"
			default:
				valStr = formatutil.FormatFloatMax4(float64(val))
			}
		}
		data = fmt.Sprintf("%s\n%s: %s", data, k, valStr)
	}

	msgStr := strings.TrimSpace(evt.Message)
	dataStr := strings.TrimSpace(data)
	if dataStr != "" {
		return fmt.Sprintf("%s%s\n%s\n%s", prefix, contextLabel, msgStr, dataStr)
	}
	return fmt.Sprintf("%s%s\n%s", prefix, contextLabel, msgStr)
}
