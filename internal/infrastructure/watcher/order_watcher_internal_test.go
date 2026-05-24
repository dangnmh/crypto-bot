package watcher

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/eventbus"

	"github.com/stretchr/testify/assert"
)

func TestOrderWatcherSkipsInvalidPublishTopics(t *testing.T) {
	t.Parallel()

	bus := eventbus.New(slog.Default())
	defer func() { _ = bus.Close() }()
	w := NewOrderWatcher(bus, slog.Default())

	assert.NotPanics(t, func() {
		w.PublishPosition(exchange.PersonalPositionUpdate{})
	})
}

func TestOrderWatcherTopicHelpersAndEmptySubscriptions(t *testing.T) {
	t.Parallel()

	bus := eventbus.New(slog.Default())
	defer func() { _ = bus.Close() }()
	w := NewOrderWatcher(bus, slog.Default())

	assert.Equal(t, "position:BTC_USDT", positionTopic("BTC_USDT"))

	assert.NotPanics(t, func() {
		w.OnPositionUpdate(context.Background(), "", time.Millisecond, func(exchange.PersonalPositionUpdate) {})
	})
}
