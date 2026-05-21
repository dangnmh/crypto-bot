package application_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"crypto-bot/internal/bots/funding/application"
	"crypto-bot/internal/testutil/mocks"

	"go.uber.org/mock/gomock"
)

func TestSubscriptionManager_SubscribeAll_TickerOnly(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)

	ws := mocks.NewMockSubscriber(ctrl)
	ws.EXPECT().SubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil)

	sm := application.NewSubscriptionManager(ws, "BTC_USDT", slog.Default())

	if err := sm.SubscribeAll(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSubscriptionManager_SubscribeAll_TickerError(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)

	ws := mocks.NewMockSubscriber(ctrl)
	ws.EXPECT().SubscribeTicker(gomock.Any(), "BTC_USDT").Return(errors.New("ws error"))

	sm := application.NewSubscriptionManager(ws, "BTC_USDT", slog.Default())

	if err := sm.SubscribeAll(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestSubscriptionManager_UnsubscribeAll_TickerOnly(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)

	ws := mocks.NewMockSubscriber(ctrl)
	ws.EXPECT().UnsubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil)

	sm := application.NewSubscriptionManager(ws, "BTC_USDT", slog.Default())

	sm.UnsubscribeAll(context.Background())
}
