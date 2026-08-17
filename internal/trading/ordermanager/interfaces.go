package ordermanager

import (
	"context"
	"time"

	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/notifier"
)

// Notifier is an alias for notifier.Notifier interface.
type Notifier = notifier.Notifier

// NotiLevelProvider allows execution events to specify custom notification severity levels.
type NotiLevelProvider interface {
	GetNotiLevel() notifier.Level
}

// ExchangeClient is the interface for executing exchange setups and order operations.
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
type ClosedPnLProvider interface {
	GetOrderPNL(ctx context.Context, symbol string, orderID string) (*exchange.ClosedPnLInfo, error)
}

// PositionWatcher is the interface for subscribing to real-time personal position updates.
type PositionWatcher interface {
	OnPositionUpdate(ctx context.Context, symbol string, timeout time.Duration, callback func(pos exchange.PersonalPositionUpdate))
}

// Clock provides time and latency queries.
type Clock interface {
	Now() time.Time
	LatencyMs() int64
	Until(t time.Time) time.Duration
	Sleep(ctx context.Context, d time.Duration) error
}

// SyncerClock represents a clock capable of forcing time synchronization.
type SyncerClock interface {
	SyncNow(ctx context.Context)
}

// TradeRepository interface for persisting trade records.
type TradeRepository interface {
	Save(ctx context.Context, event OrderTradeRecordEvent) error
}
