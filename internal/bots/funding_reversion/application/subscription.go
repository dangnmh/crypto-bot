package application

import (
	"log/slog"

	"crypto-bot/internal/bots/funding_reversion/domain"
	"crypto-bot/internal/infrastructure/ws"
)

// SubscriptionManager handles WS channel lifecycle for a trading cycle.
// Centralizes subscribe/unsubscribe logic to eliminate duplication.
type SubscriptionManager struct {
	ws     ws.Subscriber
	symbol string
	dynCfg domain.DynamicPricingConfig
	log    *slog.Logger
}

// NewSubscriptionManager creates a new subscription manager for a symbol.
func NewSubscriptionManager(wsClient ws.Subscriber, symbol string, dynCfg domain.DynamicPricingConfig, log *slog.Logger) *SubscriptionManager {
	return &SubscriptionManager{ws: wsClient, symbol: symbol, dynCfg: dynCfg, log: log}
}

// SubscribeAll subscribes to all required WS channels for the trading cycle.
func (sm *SubscriptionManager) SubscribeAll() error {
	if err := sm.ws.SubscribeTicker(sm.symbol); err != nil {
		sm.log.Error("🔴 Failed to subscribe ticker", "symbol", sm.symbol, "error", err)
		return err
	}
	if sm.dynCfg.Enabled {
		if err := sm.ws.SubscribeKline(sm.symbol); err != nil {
			sm.log.Error("🔴 Failed to subscribe kline", "symbol", sm.symbol, "error", err)
			return err
		}
		if sm.dynCfg.SlippageMode == domain.SlippageModeOBImbalance {
			if err := sm.ws.SubscribeDepth(sm.symbol, sm.dynCfg.ObStep); err != nil {
				sm.log.Error("🔴 Failed to subscribe depth", "symbol", sm.symbol, "error", err)
				return err
			}
		}
	}
	return nil
}

// UnsubscribeAll cleans up all WS subscriptions for the trading cycle.
func (sm *SubscriptionManager) UnsubscribeAll() {
	if err := sm.ws.UnsubscribeTicker(sm.symbol); err != nil {
		sm.log.Warn("⚠️ Failed to unsubscribe ticker", "symbol", sm.symbol, "error", err)
	}
	if sm.dynCfg.Enabled {
		if err := sm.ws.UnsubscribeKline(sm.symbol); err != nil {
			sm.log.Warn("⚠️ Failed to unsubscribe kline", "symbol", sm.symbol, "error", err)
		}
		if sm.dynCfg.SlippageMode == domain.SlippageModeOBImbalance {
			if err := sm.ws.UnsubscribeDepth(sm.symbol, sm.dynCfg.ObStep); err != nil {
				sm.log.Warn("⚠️ Failed to unsubscribe depth", "symbol", sm.symbol, "error", err)
			}
		}
	}
}

// UnsubscribeDepthOnly cleans up only depth subscription (called at cycle end).
func (sm *SubscriptionManager) UnsubscribeDepthOnly() {
	if sm.dynCfg.Enabled && sm.dynCfg.SlippageMode == domain.SlippageModeOBImbalance {
		if err := sm.ws.UnsubscribeDepth(sm.symbol, sm.dynCfg.ObStep); err != nil {
			sm.log.Warn("⚠️ Failed to unsubscribe depth", "symbol", sm.symbol, "error", err)
		}
	}
}
