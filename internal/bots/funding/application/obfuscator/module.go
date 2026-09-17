package obfuscator

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	shared "crypto-bot/internal/domain"
	infraapp "crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/trading/ordermanager/futures"
	ordermanagerpersistence "crypto-bot/internal/trading/ordermanager/persistence"

	"go.uber.org/fx"
)

// Module wires obfuscator dependencies and lifecycle hooks.
var Module = fx.Options(
	fx.Provide(
		ProvideOrderGenerator,
		ProvideObfuscatorDispatcher,
		ProvideObfuscatorRunner,
		NewObfuscatorJobs,
	),
	fx.Invoke(
		RegisterObfuscatorCompletionCallback,
	),
)

// ProvideOrderGenerator provides an OrderGenerator instance.
func ProvideOrderGenerator(engine *infraapp.Engine) (*OrderGenerator, error) {
	return NewOrderGenerator(engine)
}

// ProvideObfuscatorDispatcher provides an OrderManagerDispatcher backed by the engine EventBus.
func ProvideObfuscatorDispatcher(engine *infraapp.Engine) (OrderManagerDispatcher, error) {
	if engine == nil || engine.Bus == nil {
		return nil, fmt.Errorf("missing required engine event bus for obfuscator dispatcher")
	}
	return NewEventBusDispatcher(engine.Bus)
}

// ProvideObfuscatorRunner provides an ObfuscatorRunner instance.
func ProvideObfuscatorRunner(
	disp OrderManagerDispatcher,
	clock shared.Clock,
	log *slog.Logger,
) (*ObfuscatorRunner, error) {
	return NewObfuscatorRunner(disp, clock, log)
}

type noopPnLReader struct{}

func (noopPnLReader) GetAccountSymbolPnLSummaries(ctx context.Context, accountID, exch string, since time.Time) ([]ordermanagerpersistence.SymbolPnLSummary, error) {
	return nil, nil
}

// ObfuscatorCallbackParams contains dependencies for registering completion callback.
type ObfuscatorCallbackParams struct {
	fx.In

	OrderManager *futures.OrderManager `optional:"true"`
	Repo         futures.TradeRepository
	Logger       *slog.Logger
}

// RegisterObfuscatorCompletionCallback configures OrderManager completion callback to mark trade reports as obfuscated when obfuscation orders complete.
func RegisterObfuscatorCompletionCallback(params ObfuscatorCallbackParams) {
	if params.OrderManager == nil || params.Repo == nil {
		return
	}

	marker, ok := params.Repo.(interface {
		MarkObfuscated(ctx context.Context, reqID string, obfuscatedAt time.Time) error
	})
	if !ok {
		return
	}

	log := params.Logger
	if log == nil {
		log = slog.Default()
	}
	params.OrderManager.RegisterOnCompletedCallback(func(ctx context.Context, evt futures.OrderCompletedEvent) {
		if evt.StrategyType == futures.StrategyObfuscator && evt.RefID != "" {
			if err := marker.MarkObfuscated(ctx, evt.RefID, evt.CompletedAt); err != nil {
				log.ErrorContext(ctx, "Failed to mark trade report as obfuscated", slog.String("ref_id", evt.RefID), slog.Any("error", err))
			} else {
				log.InfoContext(ctx, "🛡️ Marked trade report as obfuscated", slog.String("ref_id", evt.RefID), slog.Time("obfuscated_at", evt.CompletedAt))
			}
		}
	})
}
