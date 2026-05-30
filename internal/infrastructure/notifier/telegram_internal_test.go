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

	trading := p.formatMessage(Event{
		Level:    LevelTrading,
		Exchange: "bybit",
		Symbol:   "BTC_USDT",
		Message:  "order filled",
		Data:     map[string]any{"price": 60000, "floatVal": 0.038429, "wholeFloat": 5.0},
	})
	assert.Contains(t, trading, "[TRADING] [bybit] [BTC_USDT]")
	assert.Contains(t, trading, "order filled")
	assert.Contains(t, trading, "price: 60000")
	assert.Contains(t, trading, "floatVal: 0.0384")
	assert.Contains(t, trading, "wholeFloat: 5")

	exchangeOnly := p.formatMessage(Event{Level: LevelTrading, Exchange: "mexc", Message: "risk"})
	assert.Contains(t, exchangeOnly, "[TRADING] [mexc]")

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
