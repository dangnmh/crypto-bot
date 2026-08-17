package obfuscator

import (
	"context"
	"fmt"
	"log/slog"

	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/trading/ordermanager"
)

// ObfuscatorRunner manages the execution lifecycle of an obfuscation order via OrderManager.
type ObfuscatorRunner struct {
	dispatcher OrderManagerDispatcher
	clock      shared.Clock
	logger     *slog.Logger
}

// NewObfuscatorRunner creates a new ObfuscatorRunner.
func NewObfuscatorRunner(dispatcher OrderManagerDispatcher, clock shared.Clock, logger *slog.Logger) (*ObfuscatorRunner, error) {
	if dispatcher == nil || clock == nil {
		return nil, fmt.Errorf("missing required dependencies for ObfuscatorRunner")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &ObfuscatorRunner{
		dispatcher: dispatcher,
		clock:      clock,
		logger:     logger.With("component", "ObfuscatorRunner"),
	}, nil
}

// Execute dispatches the obfuscation order micro-events to OrderManager.
func (r *ObfuscatorRunner) Execute(ctx context.Context, spec *ObfuscationSpec) error {
	if spec == nil {
		return fmt.Errorf("obfuscation spec cannot be nil")
	}

	now := r.clock.Now()

	reqID := exchange.ExternalUniqueID(spec.Symbol, now, spec.Exchange) + string(ordermanager.StrategyObfuscator)

	r.logger.InfoContext(ctx, "🛡️ Executing obfuscation order via OrderManager",
		slog.String("req_id", reqID),
		slog.String("origin_req_id", spec.OriginReqID),
		slog.String("exchange", spec.Exchange),
		slog.String("symbol", spec.Symbol),
		slog.String("side", spec.Side.String()),
		slog.Float64("volume", spec.Volume),
		slog.Float64("price", spec.Price),
		slog.Float64("notional_usdt", spec.NotionalUSDT),
		slog.Float64("margin_usdt", spec.MarginUSDT),
		slog.Int("leverage", spec.Leverage),
		slog.Float64("tp_price", spec.TakeProfitPrice),
		slog.Float64("sl_price", spec.StopLossPrice),
		slog.Duration("hold_duration", spec.HoldDuration),
	)

	orderType := spec.OrderType
	if orderType == "" {
		orderType = ordermanager.OrderTypeMarket
	}

	intentEvt := ordermanager.OrderIntentEvent{
		BaseExecutionEvent: ordermanager.BaseExecutionEvent{
			ReqID:         reqID,
			RefID:         spec.OriginReqID,
			Symbol:        spec.Symbol,
			Exchange:      spec.Exchange,
			MarketType:    ordermanager.MarketTypeFuture,
			StrategyType:  ordermanager.StrategyObfuscator,
			PreTopic:      "",
			NextTopic:     ordermanager.TopicOrderIntent,
			Timestamp:     now,
			ClientOrderID: exchange.ExternalOrderID(spec.Symbol, now, spec.Exchange),
		},
		Side:                 spec.Side,
		OrderType:            orderType,
		Price:                spec.Price,
		Volume:               spec.Volume,
		ContractSize:         spec.ContractSize,
		MarginMode:           shared.MarginModeIsolated,
		PositionMode:         shared.PositionModeHedge,
		Leverage:             spec.Leverage,
		MarginUSDT:           spec.MarginUSDT,
		FundingRate:          spec.FundingRate,
		Vol24hUSDT:           spec.Vol24hUSDT,
		TakeProfitPrice:      spec.TakeProfitPrice,
		StopLossPrice:        spec.StopLossPrice,
		PositionCloseTimeout: spec.HoldDuration,
		FireTime:             now,
		Extra: map[string]any{
			"origin_req_id": spec.OriginReqID,
			"notional_usdt": spec.NotionalUSDT,
			"tp_pct":        spec.TakeProfitPct,
			"sl_pct":        spec.StopLossPct,
			"hold_duration": spec.HoldDuration.String(),
			"vol_usdt_24h":  spec.Vol24hUSDT,
			"funding_rate":  spec.FundingRate,
		},
	}

	return r.dispatcher.Dispatch(ctx, intentEvt)
}
