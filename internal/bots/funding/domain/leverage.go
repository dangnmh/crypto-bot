package domain

import (
	"context"
	"log/slog"
)

// LeverageProvider defines optional methods an exchange client may implement for leverage constraints.
type LeverageProvider interface {
	SupportLeverageOnOrder() bool
}

// RiskLimitLeverageProvider is an interface for exchanges with tier-based risk limits.
type RiskLimitLeverageProvider interface {
	GetMaxLeverageForValue(ctx context.Context, symbol string, value float64) (int, error)
}

// MaxLeverageProvider is an interface for exchanges with fixed max leverage limits per symbol.
type MaxLeverageProvider interface {
	GetMaxLeverage(ctx context.Context, symbol string) (int, error)
}

// DetermineCandidateLeverage calculates the valid leverage for a candidate taking into account:
// 1. Contract max leverage (symbol specifications)
// 2. Exchange risk limits / leverage provider limits for the target position value.
func DetermineCandidateLeverage(
	ctx context.Context,
	client any,
	candidate *Candidate,
	logger *slog.Logger,
) int {
	if candidate == nil {
		return 1
	}

	leverage := candidate.Config.Leverage
	if candidate.MaxLeverage > 0 && leverage > candidate.MaxLeverage {
		if logger != nil {
			logger.InfoContext(ctx, "Configured leverage exceeds symbol max leverage, adjusting to max",
				slog.String("symbol", candidate.Symbol),
				slog.Int("configured", leverage),
				slog.Int("max", candidate.MaxLeverage),
			)
		}
		leverage = candidate.MaxLeverage
	}

	if leverage <= 0 {
		return 1
	}
	if client == nil {
		return leverage
	}

	if levProv, ok := client.(LeverageProvider); ok && levProv.SupportLeverageOnOrder() {
		return leverage
	}

	return resolveExchangeLeverage(ctx, client, candidate, leverage, logger)
}

func resolveExchangeLeverage(
	ctx context.Context,
	client any,
	candidate *Candidate,
	leverage int,
	logger *slog.Logger,
) int {
	targetValue := candidateTargetValue(candidate, leverage)

	switch provider := client.(type) {
	case RiskLimitLeverageProvider:
		return checkRiskLimitLeverage(ctx, provider, candidate, leverage, targetValue, logger)
	case MaxLeverageProvider:
		return checkMaxLeverage(ctx, provider, candidate, leverage, logger)
	}

	return leverage
}

func candidateTargetValue(candidate *Candidate, leverage int) float64 {
	price := candidate.LastPrice
	if price == 0 {
		price = candidate.BestBid
	}
	if price == 0 {
		price = candidate.BestAsk
	}

	targetValue := candidate.Volume * candidate.ContractSize * price
	if targetValue == 0 && candidate.Config.MarginUSDT > 0 {
		targetValue = candidate.Config.MarginUSDT * float64(leverage)
	}
	return targetValue
}

func checkRiskLimitLeverage(
	ctx context.Context,
	provider RiskLimitLeverageProvider,
	candidate *Candidate,
	leverage int,
	targetValue float64,
	logger *slog.Logger,
) int {
	maxLev, err := provider.GetMaxLeverageForValue(ctx, candidate.Symbol, targetValue)
	if err != nil {
		if logger != nil {
			logger.ErrorContext(ctx, "Failed to get max leverage for value from client",
				slog.Any("error", err),
				slog.String("symbol", candidate.Symbol),
				slog.Float64("value", targetValue),
			)
		}
		return leverage
	}
	if maxLev > 0 && leverage > maxLev {
		if logger != nil {
			logger.InfoContext(ctx, "Configured leverage exceeds exchange risk limits for position size, adjusting to max",
				slog.String("symbol", candidate.Symbol),
				slog.Int("configured", leverage),
				slog.Int("max", maxLev),
				slog.Float64("value", targetValue),
			)
		}
		return maxLev
	}
	return leverage
}

func checkMaxLeverage(
	ctx context.Context,
	provider MaxLeverageProvider,
	candidate *Candidate,
	leverage int,
	logger *slog.Logger,
) int {
	maxLev, err := provider.GetMaxLeverage(ctx, candidate.Symbol)
	if err != nil {
		if logger != nil {
			logger.ErrorContext(ctx, "Failed to get max leverage from client",
				slog.Any("error", err),
				slog.String("symbol", candidate.Symbol),
			)
		}
		return leverage
	}
	if maxLev > 0 && leverage > maxLev {
		if logger != nil {
			logger.InfoContext(ctx, "Configured leverage exceeds exchange risk limits, adjusting to max",
				slog.String("symbol", candidate.Symbol),
				slog.Int("configured", leverage),
				slog.Int("max", maxLev),
			)
		}
		return maxLev
	}
	return leverage
}
