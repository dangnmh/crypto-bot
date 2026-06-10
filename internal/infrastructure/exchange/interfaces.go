package exchange

import (
	"context"
	"time"

	"crypto-bot/internal/domain"
)

// MarketDataProvider is the interface for reading market data.
// Satisfied by *Client. Enables mock-based testing without hitting the real exchange.
type MarketDataProvider interface {
	GetTickers(ctx context.Context, symbol string) ([]Ticker, error)
	GetContractDetails(ctx context.Context) ([]ContractDetail, error)
	GetFundingRates(ctx context.Context, symbols []string) ([]FundingRateResult, error)
	GetServerTime(ctx context.Context) (int64, error)
	GetKlines(ctx context.Context, symbol, interval string, start, end int64) ([]Kline, error)
	GetDepthSnapshot(ctx context.Context, symbol string, limit int) (*domain.OrderBook, error)
	GetDepthCommits(ctx context.Context, symbol string, limit int) ([]DepthCommit, error)
}

// CreateOrderResult is the result returned from the CreateOrder method.
type CreateOrderResult struct {
	OrderID       string `json:"orderId"`
	TPSLSubmitted bool   `json:"tpslSubmitted"`
}

// OrderExecutor is the interface for placing and managing orders.
// Satisfied by *Client. Enables mock-based testing without hitting the real exchange.
type OrderExecutor interface {
	CreateOrder(ctx context.Context, req SubmitOrderRequest) (CreateOrderResult, error)
	CreateTrackOrder(ctx context.Context, req SubmitTrackOrderRequest) (string, error)
	CancelOrder(ctx context.Context, symbol, orderID string) error
	CancelOrders(ctx context.Context, orderIDs []string) error
	CancelAllOpenOrders(ctx context.Context, symbol string) error
	GetOrder(ctx context.Context, symbol, orderID string) (*OrderInfo, error)
	GetOpenOrders(ctx context.Context, symbol string) ([]OrderInfo, error)
	ClosePosition(ctx context.Context, symbol string, closeSide domain.Side, volume float64, positionMode domain.PositionMode) error
	CloseAllPositions(ctx context.Context, symbol string) error
	ChangeLeverage(ctx context.Context, req ChangeLeverageRequest) error
	SwitchMarginMode(ctx context.Context, symbol, marginMode string, leverage int, side domain.Side) error
}

// TPSLProvider is an optional interface that exchange REST clients can implement
// to support post-fill Take Profit and Stop Loss configuration.
type TPSLProvider interface {
	PlaceTPSL(ctx context.Context, req TPSLRequest) error
}

// AccountProvider is the interface for reading account data.
// Satisfied by *Client. Enables mock-based testing without hitting the real exchange.
type AccountProvider interface {
	GetAssets(ctx context.Context) ([]AssetInfo, error)
	GetAssetByCurrency(ctx context.Context, currency string) (*AssetInfo, error)
	GetOpenPositions(ctx context.Context, symbol string) ([]Position, error)
}

// ClosedPnLInfo represents the standardized historical ledger of a closed trade.
type ClosedPnLInfo struct {
	Symbol     string
	EntryPrice float64
	ExitPrice  float64
	ClosedSize float64
	GrossPnL   float64
	Fee        float64
	FundingFee float64
	DurationMs int64
	NetPnl     float64
	PnLRate    float64
}

// ClosedPnLProvider is an optional interface that exchange REST clients can implement.
type ClosedPnLProvider interface {
	GetRecentClosedPnL(ctx context.Context, symbol, extOrderID string, startTime time.Time) (*ClosedPnLInfo, error)
}

// Client is the generic composite interface for any Exchange provider.
type Client interface {
	MarketDataProvider
	OrderExecutor
	AccountProvider
	WarmUp(ctx context.Context, interval time.Duration)
	SupportLeverageOnOrder() bool
}
