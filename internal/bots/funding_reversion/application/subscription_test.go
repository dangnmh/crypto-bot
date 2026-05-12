package application_test

import (
	"errors"
	"log/slog"
	"testing"

	"crypto-bot/internal/bots/funding_reversion/application"
	"crypto-bot/internal/bots/funding_reversion/domain"
	"crypto-bot/internal/testutil/mocks"

	"go.uber.org/mock/gomock"
)

func TestSubscriptionManager_SubscribeAll_NoDynamic(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)

	ws := mocks.NewMockSubscriber(ctrl)
	ws.EXPECT().SubscribeTicker("BTC_USDT").Return(nil)

	sm := application.NewSubscriptionManager(ws, "BTC_USDT", domain.DynamicPricingConfig{Enabled: false}, slog.Default())

	if err := sm.SubscribeAll(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSubscriptionManager_SubscribeAll_WithOBImbalance(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)

	ws := mocks.NewMockSubscriber(ctrl)
	ws.EXPECT().SubscribeTicker("BTC_USDT").Return(nil)
	ws.EXPECT().SubscribeKline("BTC_USDT").Return(nil)
	ws.EXPECT().SubscribeDepth("BTC_USDT", "step0").Return(nil)

	dynCfg := domain.DynamicPricingConfig{
		Enabled:      true,
		SlippageMode: domain.SlippageModeOBImbalance,
		ObStep:       "step0",
	}
	sm := application.NewSubscriptionManager(ws, "BTC_USDT", dynCfg, slog.Default())

	if err := sm.SubscribeAll(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSubscriptionManager_SubscribeAll_SpreadMode(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)

	ws := mocks.NewMockSubscriber(ctrl)
	ws.EXPECT().SubscribeTicker("BTC_USDT").Return(nil)
	ws.EXPECT().SubscribeKline("BTC_USDT").Return(nil)
	// SubscribeDepth should NOT be called — gomock fails on unexpected calls.

	dynCfg := domain.DynamicPricingConfig{
		Enabled:      true,
		SlippageMode: domain.SlippageModeSpreadMultipler,
	}
	sm := application.NewSubscriptionManager(ws, "BTC_USDT", dynCfg, slog.Default())

	if err := sm.SubscribeAll(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSubscriptionManager_SubscribeAll_TickerError(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)

	ws := mocks.NewMockSubscriber(ctrl)
	ws.EXPECT().SubscribeTicker("BTC_USDT").Return(errors.New("ws error"))

	sm := application.NewSubscriptionManager(ws, "BTC_USDT", domain.DynamicPricingConfig{}, slog.Default())

	if err := sm.SubscribeAll(); err == nil {
		t.Fatal("expected error")
	}
}

func TestSubscriptionManager_SubscribeAll_KlineError(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)

	ws := mocks.NewMockSubscriber(ctrl)
	ws.EXPECT().SubscribeTicker("BTC_USDT").Return(nil)
	ws.EXPECT().SubscribeKline("BTC_USDT").Return(errors.New("kline error"))

	dynCfg := domain.DynamicPricingConfig{Enabled: true}
	sm := application.NewSubscriptionManager(ws, "BTC_USDT", dynCfg, slog.Default())

	if err := sm.SubscribeAll(); err == nil {
		t.Fatal("expected error")
	}
}

func TestSubscriptionManager_SubscribeAll_DepthError(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)

	ws := mocks.NewMockSubscriber(ctrl)
	ws.EXPECT().SubscribeTicker("BTC_USDT").Return(nil)
	ws.EXPECT().SubscribeKline("BTC_USDT").Return(nil)
	ws.EXPECT().SubscribeDepth("BTC_USDT", "step0").Return(errors.New("depth error"))

	dynCfg := domain.DynamicPricingConfig{
		Enabled:      true,
		SlippageMode: domain.SlippageModeOBImbalance,
		ObStep:       "step0",
	}
	sm := application.NewSubscriptionManager(ws, "BTC_USDT", dynCfg, slog.Default())

	if err := sm.SubscribeAll(); err == nil {
		t.Fatal("expected error")
	}
}

func TestSubscriptionManager_UnsubscribeAll(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)

	ws := mocks.NewMockSubscriber(ctrl)
	ws.EXPECT().UnsubscribeTicker("BTC_USDT").Return(nil)
	ws.EXPECT().UnsubscribeKline("BTC_USDT").Return(nil)
	ws.EXPECT().UnsubscribeDepth("BTC_USDT", "step0").Return(nil)

	dynCfg := domain.DynamicPricingConfig{
		Enabled:      true,
		SlippageMode: domain.SlippageModeOBImbalance,
		ObStep:       "step0",
	}
	sm := application.NewSubscriptionManager(ws, "BTC_USDT", dynCfg, slog.Default())

	sm.UnsubscribeAll()
	// gomock verifies all expectations at controller cleanup.
}

func TestSubscriptionManager_UnsubscribeDepthOnly_OBMode(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)

	ws := mocks.NewMockSubscriber(ctrl)
	ws.EXPECT().UnsubscribeDepth("BTC_USDT", "step0").Return(nil)

	dynCfg := domain.DynamicPricingConfig{
		Enabled:      true,
		SlippageMode: domain.SlippageModeOBImbalance,
		ObStep:       "step0",
	}
	sm := application.NewSubscriptionManager(ws, "BTC_USDT", dynCfg, slog.Default())

	sm.UnsubscribeDepthOnly()
}

func TestSubscriptionManager_UnsubscribeDepthOnly_SpreadMode_NoCall(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)

	ws := mocks.NewMockSubscriber(ctrl)
	// No expectations — UnsubscribeDepth should NOT be called.

	dynCfg := domain.DynamicPricingConfig{
		Enabled:      true,
		SlippageMode: domain.SlippageModeSpreadMultipler,
	}
	sm := application.NewSubscriptionManager(ws, "BTC_USDT", dynCfg, slog.Default())

	sm.UnsubscribeDepthOnly()
}
