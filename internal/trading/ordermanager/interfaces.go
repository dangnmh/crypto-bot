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

// OrderStreamUpdate represents private order stream updates.
type OrderStreamUpdate struct {
	Symbol    string
	OrderID   string
	Status    string
	FilledVol float64
	AvgPrice  float64
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

// RiskLimitLeverageProvider checks maximum leverage allowed for a given position value.
type RiskLimitLeverageProvider interface {
	GetMaxLeverageForValue(ctx context.Context, symbol string, notionalValue float64) (int, error)
}

// TPSLProvider allows placing post-fill TP/SL trigger orders.
type TPSLProvider interface {
	PlaceTPSL(ctx context.Context, req exchange.TPSLRequest) error
}

// ClosedPnLProvider enriches actual trade fill prices and net profit metrics.
type ClosedPnLProvider interface {
	GetOrderPNL(ctx context.Context, symbol string, orderID string) (*exchange.ClosedPnLInfo, error)
}

// OrderWatcher is the interface for subscribing to private order stream fill notifications.
type OrderWatcher interface {
	SubscribeOrderUpdates(ctx context.Context, symbol string) (<-chan OrderStreamUpdate, error)
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
