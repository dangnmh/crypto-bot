package application

import (
	"log/slog"

	"crypto-bot/internal/bots/funding/application/cycle"
	"crypto-bot/internal/bots/funding/domain"
	"crypto-bot/internal/infrastructure/ws"
)

type SubscriptionManager = cycle.SubscriptionManager

func NewSubscriptionManager(
	wsClient ws.Subscriber,
	symbol string,
	dynCfg domain.DynamicPricingConfig,
	imbCfg domain.ImbalanceFilterConfig,
	log *slog.Logger,
) *SubscriptionManager {
	return cycle.NewSubscriptionManager(wsClient, symbol, dynCfg, imbCfg, log)
}
