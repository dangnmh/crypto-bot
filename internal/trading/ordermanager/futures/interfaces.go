package futures

import (
	"context"
	"time"

	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/trading/ordermanager/common"
)

// Re-export common types for futures consumers.
type (
	Notifier          = common.Notifier
	NotiLevelProvider = common.NotiLevelProvider
	Clock             = common.Clock
	SyncerClock       = common.SyncerClock
	MarketType        = common.MarketType
	StrategyType      = common.StrategyType
	OrderType         = common.OrderType
	OrderOutcome      = common.OrderOutcome
	TradeRepository   = common.TradeRepository
	OrderEvent        = common.OrderEvent
	BaseOrderEvent    = common.BaseOrderEvent
)

// Constants re-exported from common.
const (
	MarketTypeSpot    = common.MarketTypeSpot
	MarketTypeFutures = common.MarketTypeFutures
	MarketTypeFuture  = common.MarketTypeFutures

	StrategyFundingReversion = common.StrategyFundingReversion
	StrategyFundingArbitrage = common.StrategyFundingArbitrage
	StrategyObfuscator       = common.StrategyObfuscator
	StrategyDilution         = common.StrategyDilution
	StrategyPennyJumper      = common.StrategyPennyJumper

	OrderTypeMarket   = common.OrderTypeMarket
	OrderTypeLimit    = common.OrderTypeLimit
	OrderTypePostOnly = common.OrderTypePostOnly
	OrderTypeIOC      = common.OrderTypeIOC
	OrderTypeFOK      = common.OrderTypeFOK

	OutcomeFilled          = common.OutcomeFilled
	OutcomePartialFilled   = common.OutcomePartialFilled
	OutcomePartiallyFilled = common.OutcomePartiallyFilled
	OutcomeCanceledNoFill  = common.OutcomeCanceledNoFill
	OutcomeCanceled        = common.OutcomeCanceled
	OutcomeAborted         = common.OutcomeAborted
	OutcomeResting         = common.OutcomeResting
	OutcomeRejected        = common.OutcomeRejected
	OutcomeExpired         = common.OutcomeExpired
	OutcomeUnknown         = common.OutcomeUnknown
)

// ExchangeClient is the interface for executing exchange setups and order operations for futures.
type ExchangeClient interface {
	SwitchMarginMode(ctx context.Context, symbol string, mode shared.MarginMode, leverage int, side shared.Side) error
	ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error
	SupportLeverageOnOrder() bool
	CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error)
	CancelOrder(ctx context.Context, symbol string, orderID string) error
	ClosePosition(ctx context.Context, symbol string, side shared.Side, volume float64, positionMode shared.PositionMode, leverage int) error
	CloseAllPositions(ctx context.Context, symbol string) error
	GetOrder(ctx context.Context, symbol string, orderID string) (*exchange.OrderInfo, error)
	GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error)
}

// PositionModeSwitcher allows switching exchange position mode (Hedge / OneWay).
type PositionModeSwitcher interface {
	SwitchPositionMode(ctx context.Context, symbol string, mode shared.PositionMode) error
}

// TPSLProvider allows placing post-fill TP/SL trigger orders.
type TPSLProvider interface {
	PlaceTPSL(ctx context.Context, req exchange.TPSLRequest) error
}

// ClosedPnLProvider enriches actual trade fill prices and net profit metrics.
type ClosedPnLProvider = common.ClosedPnLProvider

// PositionWatcher is the interface for subscribing to real-time personal position and trade updates.
type PositionWatcher interface {
	OnPositionUpdate(ctx context.Context, symbol string, timeout time.Duration, callback func(pos exchange.PersonalPositionUpdate))
	OnTradeUpdate(ctx context.Context, symbol string, timeout time.Duration, callback func(trades []shared.PublicTrade))
}
