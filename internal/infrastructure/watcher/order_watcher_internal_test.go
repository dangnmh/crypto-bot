//nolint:testpackage // These tests exercise unexported topic helpers.
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
		w.PublishOrder(exchange.WsOrderDeal{})
		w.PublishDeal(exchange.PersonalOrderDeal{Symbol: "BTC_USDT", Side: 999})
		w.PublishPosition(exchange.PersonalPositionUpdate{})
	})
}

func TestOrderWatcherTopicHelpersAndEmptySubscriptions(t *testing.T) {
	t.Parallel()

	bus := eventbus.New(slog.Default())
	defer func() { _ = bus.Close() }()
	w := NewOrderWatcher(bus, slog.Default())

	assert.Empty(t, orderTopic(""))
	assert.Empty(t, symbolSideDealTopic("", "1"))
	assert.Empty(t, symbolSideDealTopic("BTC_USDT", ""))
	assert.Empty(t, symbolSideDealTopic("BTC_USDT", "UNKNOWN"))
	assert.Equal(t, "track:track-1", trackTopic("track-1"))
	assert.Equal(t, "track:order:order-1", trackOrderTopic("order-1"))
	assert.Equal(t, "position:BTC_USDT", positionTopic("BTC_USDT"))

	assert.NotPanics(t, func() {
		w.OnOrderUpdate(context.Background(), "", time.Millisecond, func(exchange.WsOrderDeal) {})
		w.OnOrderDealBySymbolSide(context.Background(), "", "", time.Millisecond, func(exchange.PersonalOrderDeal) {})
		w.OnTrackOrderUpdate(context.Background(), "", "", time.Millisecond, func(exchange.PersonalTrackOrderUpdate) {})
	})
}
