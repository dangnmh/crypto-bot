package application

import (
	"log/slog"

	"crypto-bot/internal/bots/funding_reversion/config"
	"crypto-bot/internal/infrastructure/ws"
)

// SubscriptionManager handles WS channel lifecycle for a trading cycle.
// Centralizes subscribe/unsubscribe logic to eliminate duplication.
type SubscriptionManager struct {
	ws     *ws.Client
	symbol string
	dynCfg config.DynamicPricingConfig
	log    *slog.Logger
}

// NewSubscriptionManager creates a new subscription manager for a symbol.
func NewSubscriptionManager(wsClient *ws.Client, symbol string, dynCfg config.DynamicPricingConfig, log *slog.Logger) *SubscriptionManager {
	return &SubscriptionManager{ws: wsClient, symbol: symbol, dynCfg: dynCfg, log: log}
}

// SubscribeAll subscribes to all required WS channels for the trading cycle.
func (sm *SubscriptionManager) SubscribeAll() {
	_ = sm.ws.Subscribe(sm.symbol, "ticker")
	if sm.dynCfg.Enabled {
		_ = sm.ws.Subscribe(sm.symbol, "kline")
		if sm.dynCfg.SlippageMode == "OB_IMBALANCE" {
			_ = sm.ws.SubscribeDepth(sm.symbol, sm.dynCfg.ObStep)
		}
	}
}

// UnsubscribeAll cleans up all WS subscriptions for the trading cycle.
func (sm *SubscriptionManager) UnsubscribeAll() {
	sm.ws.Unsubscribe(sm.symbol, "ticker")
	if sm.dynCfg.Enabled {
		sm.ws.Unsubscribe(sm.symbol, "kline")
		if sm.dynCfg.SlippageMode == "OB_IMBALANCE" {
			sm.ws.UnsubscribeDepth(sm.symbol, sm.dynCfg.ObStep)
		}
	}
}

// UnsubscribeDepthOnly cleans up only depth subscription (called at cycle end).
func (sm *SubscriptionManager) UnsubscribeDepthOnly() {
	if sm.dynCfg.Enabled && sm.dynCfg.SlippageMode == "OB_IMBALANCE" {
		sm.ws.UnsubscribeDepth(sm.symbol, sm.dynCfg.ObStep)
	}
}
