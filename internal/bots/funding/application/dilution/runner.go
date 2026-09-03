package dilution

import (
	"context"
	"fmt"
	"log/slog"

	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/trading/ordermanager/futures"
)

// DilutionRunner executes dilution specs via OrderManager.
type DilutionRunner struct {
	dispatcher OrderManagerDispatcher
	clock      shared.Clock
	logger     *slog.Logger
}

// NewDilutionRunner creates a new DilutionRunner.
func NewDilutionRunner(dispatcher OrderManagerDispatcher, clock shared.Clock, logger *slog.Logger) (*DilutionRunner, error) {
	if dispatcher == nil || clock == nil {
		return nil, fmt.Errorf("missing required dependencies for DilutionRunner")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &DilutionRunner{
		dispatcher: dispatcher,
		clock:      clock,
		logger:     logger.With("component", "DilutionRunner"),
	}, nil
}

// Execute dispatches the PostOnly maker order to OrderManager.
func (r *DilutionRunner) Execute(ctx context.Context, spec *DilutionSpec) error {
	if spec == nil {
		return fmt.Errorf("dilution spec cannot be nil")
	}

	now := r.clock.Now()
	reqID := exchange.ExternalUniqueID(spec.Symbol, now, spec.Exchange) + string(futures.StrategyDilution)

	r.logger.InfoContext(ctx, "💧 Executing PostOnly dilution maker quote",
		slog.String("req_id", reqID),
		slog.String("exchange", spec.Exchange),
		slog.String("symbol", spec.Symbol),
		slog.String("side", spec.Side.String()),
		slog.Float64("price", spec.Price),
		slog.Float64("volume", spec.Volume),
		slog.Float64("notional_usdt", spec.NotionalUSDT),
		slog.Float64("margin_usdt", spec.MarginUSDT),
		slog.Int("leverage", spec.Leverage),
		slog.Duration("position_close_timeout", spec.PositionCloseTimeout),
		slog.Duration("unfilled_cancel_timeout", spec.UnfilledCancelTimeout),
	)

	orderType := spec.OrderType
	if orderType == "" {
		orderType = futures.OrderTypePostOnly
	}

	intentEvt := futures.OrderIntentEvent{
		ReqID:                 reqID,
		RefID:                 reqID,
		Symbol:                spec.Symbol,
		Exchange:              spec.Exchange,
		MarketType:            futures.MarketTypeFuture,
		StrategyType:          futures.StrategyDilution,
		PreTopic:              "",
		NextTopic:             futures.TopicOrderIntent,
		Timestamp:             now,
		ClientOrderID:         exchange.ExternalOrderID(spec.Symbol, now, spec.Exchange),
		Side:                  spec.Side,
		OrderType:             orderType,
		Price:                 spec.Price,
		Volume:                spec.Volume,
		ContractSize:          spec.ContractSize,
		MarginMode:            shared.MarginModeIsolated,
		PositionMode:          shared.PositionModeHedge,
		Leverage:              spec.Leverage,
		MarginUSDT:            spec.MarginUSDT,
		FundingRate:           0,
		Vol24hUSDT:            spec.Vol24hUSDT,
		TakeProfitPrice:       spec.TakeProfitPrice,
		StopLossPrice:         spec.StopLossPrice,
		PositionCloseTimeout:  spec.PositionCloseTimeout,
		UnfilledCancelTimeout: spec.UnfilledCancelTimeout,
		FireTime:              now,
		Extra: map[string]any{
			"notional_usdt": spec.NotionalUSDT,
			"tp_price":      spec.TakeProfitPrice,
			"sl_price":      spec.StopLossPrice,
		},
	}

	return r.dispatcher.Dispatch(ctx, intentEvt)
}

// CancelOpenOrders cancels all open orders for a symbol on an exchange via OrderManager.
func (r *DilutionRunner) CancelOpenOrders(ctx context.Context, exchangeName, symbol string) error {
	return r.dispatcher.CancelOpenOrders(ctx, exchangeName, symbol)
}
