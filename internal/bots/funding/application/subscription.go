package application

import (
	"log/slog"

	"crypto-bot/internal/bots/funding/application/cycle"
	"crypto-bot/internal/infrastructure/ws"
)

type SubscriptionManager = cycle.SubscriptionManager

func NewSubscriptionManager(
	wsClient ws.Subscriber,
	symbol string,
	log *slog.Logger,
) *SubscriptionManager {
	return cycle.NewSubscriptionManager(wsClient, symbol, log)
}
