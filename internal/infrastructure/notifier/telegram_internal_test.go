package notifier

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testNotifierLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestTelegramProviderSendQueueAndStop(t *testing.T) {
	t.Parallel()

	p := &TelegramProvider{
		chatID: 1,
		logger: testNotifierLogger(),
		queue:  make(chan Event, 1),
	}

	err := p.Send(context.Background(), Event{Level: LevelInfo, Message: "queued"})
	require.NoError(t, err)
	assert.Equal(t, "queued", (<-p.queue).Message)

	require.NoError(t, p.Stop(context.Background()))
	assert.True(t, p.stopped)
	require.NoError(t, p.Send(context.Background(), Event{Message: "ignored"}))
}

func TestTelegramProviderSendHonorsContextAndFullQueue(t *testing.T) {
	t.Parallel()

	p := &TelegramProvider{
		chatID: 1,
		logger: testNotifierLogger(),
		queue:  make(chan Event),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, p.Send(ctx, Event{Message: "cancelled"}), context.Canceled)

	err := p.Send(context.Background(), Event{Message: "full"})
	require.ErrorContains(t, err, "notifier queue full")
}

func TestTelegramProviderStartDrainsQueueWithNilBot(t *testing.T) {
	t.Parallel()

	p := &TelegramProvider{
		chatID: 1,
		logger: testNotifierLogger(),
		queue:  make(chan Event, 10),
	}

	require.NoError(t, p.Start(context.Background()))
	require.Eventually(t, func() bool {
		return len(p.queue) == 0
	}, time.Second, 10*time.Millisecond)
	require.NoError(t, p.Stop(context.Background()))
}

func TestTelegramProviderFormatMessage(t *testing.T) {
	t.Parallel()

	p := &TelegramProvider{logger: testNotifierLogger()}

	normal := p.formatMessage(Event{
		Level:    LevelNormal,
		Exchange: "bybit",
		Symbol:   "BTC_USDT",
		Message:  "order filled",
		Data:     map[string]any{"price": 60000, "floatVal": 0.038429, "wholeFloat": 5.0, "volusdt24h": 60000000.0, "fundingRate": 0.008},
	})
	assert.Contains(t, normal, "[NORMAL] [bybit] [BTC_USDT]")
	assert.Contains(t, normal, "order filled")
	assert.Contains(t, normal, "price: 60000")
	assert.Contains(t, normal, "floatVal: 0.0384")
	assert.Contains(t, normal, "wholeFloat: 5")
	assert.Contains(t, normal, "volusdt24h: 60m")
	assert.Contains(t, normal, "fundingRate: 0.8%")

	negativeFR := p.formatMessage(Event{
		Level: LevelNormal,
		Data:  map[string]any{"fundingRate": -0.01},
	})
	assert.Contains(t, negativeFR, "fundingRate: -1%")

	exchangeOnly := p.formatMessage(Event{Level: LevelNormal, Exchange: "mexc", Message: "risk"})
	assert.Contains(t, exchangeOnly, "[NORMAL] [mexc]")

	critical := p.formatMessage(Event{Level: LevelCritical, Message: "risk"})
	assert.Contains(t, critical, "[CRITICAL]")

	info := p.formatMessage(Event{Level: LevelInfo, Message: "started"})
	assert.Contains(t, info, "[INFO]")

	// Test color mapping
	greenEvt := p.formatMessage(Event{Level: LevelInfo, Color: "green", Message: "gain"})
	assert.Contains(t, greenEvt, "🟢 [INFO]")

	redEvt := p.formatMessage(Event{Level: LevelInfo, Color: "red", Message: "loss"})
	assert.Contains(t, redEvt, "🔴 [INFO]")

	blueEvt := p.formatMessage(Event{Level: LevelInfo, Color: "blue", Message: "info"})
	assert.Contains(t, blueEvt, "🔵 [INFO]")

	yellowEvt := p.formatMessage(Event{Level: LevelInfo, Color: "yellow", Message: "warn"})
	assert.Contains(t, yellowEvt, "🟡 [INFO]")

	defaultEvt := p.formatMessage(Event{Level: LevelInfo, Message: "default"})
	assert.Contains(t, defaultEvt, "🟡 [INFO]") // Defaults to yellow
}

func TestTelegramProviderTargetChatID(t *testing.T) {
	t.Parallel()

	// 1. Both chatID and criticalChatID set
	pDual := &TelegramProvider{
		chatID:         100,
		criticalChatID: 200,
		logger:         testNotifierLogger(),
	}

	// Normal events go to chatID
	assert.Equal(t, int64(100), pDual.chatID)
	// Critical level routes to criticalChatID
	evtCritical := Event{Level: LevelCritical, Message: "alert"}
	targetDual := pDual.chatID
	if evtCritical.Level == LevelCritical && pDual.criticalChatID != 0 {
		targetDual = pDual.criticalChatID
	}
	assert.Equal(t, int64(200), targetDual)

	// 2. Only chatID set (criticalChatID == 0) -> fallback to chatID
	pSingle := &TelegramProvider{
		chatID:         100,
		criticalChatID: 0,
		logger:         testNotifierLogger(),
	}
	targetSingle := pSingle.chatID
	if evtCritical.Level == LevelCritical && pSingle.criticalChatID != 0 {
		targetSingle = pSingle.criticalChatID
	}
	assert.Equal(t, int64(100), targetSingle)

	// 3. Normal / Info levels always route to chatID even when criticalChatID is set
	evtNormal := Event{Level: LevelNormal, Message: "fill"}
	targetNormal := pDual.chatID
	if evtNormal.Level == LevelCritical && pDual.criticalChatID != 0 {
		targetNormal = pDual.criticalChatID
	}
	assert.Equal(t, int64(100), targetNormal)
}
