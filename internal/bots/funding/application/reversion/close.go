package reversion

import (
	"context"
	"log/slog"

	applogger "crypto-bot/pkg/logger"
)

const reversionRetryCount = 3

func (s *Strategy) forceClosePosition(
	ctx context.Context,
	symbol string,
	maxRetries int,
) (int, error) {
	if maxRetries <= 0 {
		maxRetries = reversionRetryCount
	}

	retries, err := s.RetryWithBackoff(ctx, maxRetries, func() error {
		return s.deps.Client.CloseAllPositions(ctx, symbol)
	})
	if err != nil {
		applogger.WithCtx(ctx, s.log).Error("Reversion fallback close all failed",
			slog.Any("error", err),
			slog.String("symbol", symbol),
		)
		return retries, err
	}

	return retries, nil
}
