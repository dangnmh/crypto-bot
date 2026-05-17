package application

import (
	"context"
	"log/slog"

	"crypto-bot/internal/bots/funding/application/orders"
	"crypto-bot/internal/bots/funding/domain"
	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
)

type OrderResult = orders.OrderResult

func FireIOC(ctx context.Context, client exchange.Client, candidate *domain.Candidate, ts shared.Clock, logger *slog.Logger, ob *shared.OrderBook) orders.OrderResult {
	return orders.FireIOC(ctx, client, candidate, ts, logger, ob)
}

func FireLimitTrap(ctx context.Context, client exchange.Client, candidate *domain.Candidate, ts shared.Clock, logger *slog.Logger) orders.OrderResult {
	return orders.FireLimitTrap(ctx, client, candidate, ts, logger)
}
