package cycle

import (
	"context"
	"log/slog"

	"crypto-bot/internal/infrastructure/ws"
)

// SubscriptionManager handles WS channel lifecycle for a trading cycle.
type SubscriptionManager struct {
	ws     ws.Subscriber
	symbol string
	log    *slog.Logger
}

func NewSubscriptionManager(
	wsClient ws.Subscriber,
	symbol string,
	log *slog.Logger,
) *SubscriptionManager {
	return &SubscriptionManager{ws: wsClient, symbol: symbol, log: log}
}

func (sm *SubscriptionManager) SubscribeAll(ctx context.Context) error {
	if err := sm.ws.SubscribeTicker(ctx, sm.symbol); err != nil {
		sm.log.Error("🔴 Failed to subscribe ticker", slog.String("symbol", sm.symbol), slog.Any("error", err))
		return err
	}
	return nil
}

func (sm *SubscriptionManager) UnsubscribeAll(ctx context.Context) {
	if err := sm.ws.UnsubscribeTicker(ctx, sm.symbol); err != nil {
		sm.log.Warn("⚠️ Failed to unsubscribe ticker", slog.String("symbol", sm.symbol), slog.Any("error", err))
	}
}
