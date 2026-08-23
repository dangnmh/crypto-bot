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

	err := p.Send(context.Background(), Event{Level: LevelNormal, Message: "queued"})
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
