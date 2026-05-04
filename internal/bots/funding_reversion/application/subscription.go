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
func (sm *SubscriptionManager) SubscribeAll() {
	_ = sm.ws.SubscribeTicker(sm.symbol)
	if sm.dynCfg.Enabled {
		_ = sm.ws.SubscribeKline(sm.symbol)
		if sm.dynCfg.SlippageMode == "OB_IMBALANCE" {
			_ = sm.ws.SubscribeDepth(sm.symbol, sm.dynCfg.ObStep)
		}
	}
}

// UnsubscribeAll cleans up all WS subscriptions for the trading cycle.
func (sm *SubscriptionManager) UnsubscribeAll() {
	_ = sm.ws.UnsubscribeTicker(sm.symbol)
	if sm.dynCfg.Enabled {
		_ = sm.ws.UnsubscribeKline(sm.symbol)
		if sm.dynCfg.SlippageMode == "OB_IMBALANCE" {
			_ = sm.ws.UnsubscribeDepth(sm.symbol, sm.dynCfg.ObStep)
		}
	}
}

// UnsubscribeDepthOnly cleans up only depth subscription (called at cycle end).
func (sm *SubscriptionManager) UnsubscribeDepthOnly() {
	if sm.dynCfg.Enabled && sm.dynCfg.SlippageMode == "OB_IMBALANCE" {
		_ = sm.ws.UnsubscribeDepth(sm.symbol, sm.dynCfg.ObStep)
	}
}
