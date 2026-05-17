package cycle

import (
	"context"
	"log/slog"

	"crypto-bot/internal/bots/funding/domain"
	"crypto-bot/internal/infrastructure/ws"
)

// SubscriptionManager handles WS channel lifecycle for a trading cycle.
type SubscriptionManager struct {
	ws     ws.Subscriber
	symbol string
	dynCfg domain.DynamicPricingConfig
	imbCfg domain.ImbalanceFilterConfig
	log    *slog.Logger
}

func NewSubscriptionManager(
	wsClient ws.Subscriber,
	symbol string,
	dynCfg domain.DynamicPricingConfig,
	imbCfg domain.ImbalanceFilterConfig,
	log *slog.Logger,
) *SubscriptionManager {
	return &SubscriptionManager{ws: wsClient, symbol: symbol, dynCfg: dynCfg, imbCfg: imbCfg, log: log}
}

func (sm *SubscriptionManager) SubscribeAll(ctx context.Context) error {
	if err := sm.ws.SubscribeTicker(ctx, sm.symbol); err != nil {
		sm.log.Error("🔴 Failed to subscribe ticker", slog.String("symbol", sm.symbol), slog.Any("error", err))
		return err
	}
	if sm.dynCfg.Enabled {
		if err := sm.ws.SubscribeKline(ctx, sm.symbol); err != nil {
			sm.log.Error("🔴 Failed to subscribe kline", slog.String("symbol", sm.symbol), slog.Any("error", err))
			return err
		}
	}
	if sm.needsDepth() {
		if err := sm.ws.SubscribeDepth(ctx, sm.symbol, sm.dynCfg.ObStep); err != nil {
			sm.log.Error("🔴 Failed to subscribe depth", slog.String("symbol", sm.symbol), slog.Any("error", err))
			return err
		}
	}
	return nil
}

func (sm *SubscriptionManager) UnsubscribeAll(ctx context.Context) {
	if err := sm.ws.UnsubscribeTicker(ctx, sm.symbol); err != nil {
		sm.log.Warn("⚠️ Failed to unsubscribe ticker", slog.String("symbol", sm.symbol), slog.Any("error", err))
	}
	if sm.dynCfg.Enabled {
		if err := sm.ws.UnsubscribeKline(ctx, sm.symbol); err != nil {
			sm.log.Warn("⚠️ Failed to unsubscribe kline", slog.String("symbol", sm.symbol), slog.Any("error", err))
		}
	}
	if sm.needsDepth() {
		if err := sm.ws.UnsubscribeDepth(ctx, sm.symbol, sm.dynCfg.ObStep); err != nil {
			sm.log.Warn("⚠️ Failed to unsubscribe depth", slog.String("symbol", sm.symbol), slog.Any("error", err))
		}
	}
}

func (sm *SubscriptionManager) UnsubscribeDepthOnly(ctx context.Context) {
	if sm.needsDepth() {
		if err := sm.ws.UnsubscribeDepth(ctx, sm.symbol, sm.dynCfg.ObStep); err != nil {
			sm.log.Warn("⚠️ Failed to unsubscribe depth", slog.String("symbol", sm.symbol), slog.Any("error", err))
		}
	}
}

func (sm *SubscriptionManager) needsDepth() bool {
	return sm.imbCfg.Enabled || (sm.dynCfg.Enabled && sm.dynCfg.SlippageMode == domain.SlippageModeOBImbalance)
}
