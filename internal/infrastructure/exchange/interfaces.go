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
	GetFundingRate(ctx context.Context, symbol string) (*FundingRateDetail, error)
	GetServerTime(ctx context.Context) (int64, error)
	GetKlines(ctx context.Context, symbol, interval string, start, end int64) ([]Kline, error)
	GetDepthSnapshot(ctx context.Context, symbol string, limit int) (*domain.OrderBook, error)
	GetDepthCommits(ctx context.Context, symbol string, limit int) ([]DepthCommit, error)
}

// OrderExecutor is the interface for placing and managing orders.
// Satisfied by *Client. Enables mock-based testing without hitting the real exchange.
type OrderExecutor interface {
	CreateOrder(ctx context.Context, req SubmitOrderRequest) (string, error)
	CreateTrackOrder(ctx context.Context, req SubmitTrackOrderRequest) (string, error)
	CancelOrder(ctx context.Context, symbol, orderID string) error
	CancelOrders(ctx context.Context, orderIDs []string) error
	CancelAllOpenOrders(ctx context.Context, symbol string) error
	GetOrder(ctx context.Context, orderID string) (*OrderInfo, error)
	GetOpenOrders(ctx context.Context, symbol string) ([]OrderInfo, error)
	ClosePosition(ctx context.Context, symbol string, closeSide domain.Side, volume float64, positionMode int) error
	CloseAllPositions(ctx context.Context, symbol string) error
	ChangeLeverage(ctx context.Context, req ChangeLeverageRequest) error
}

// AccountProvider is the interface for reading account data.
// Satisfied by *Client. Enables mock-based testing without hitting the real exchange.
type AccountProvider interface {
	GetAssets(ctx context.Context) ([]AssetInfo, error)
	GetAssetByCurrency(ctx context.Context, currency string) (*AssetInfo, error)
	GetOpenPositions(ctx context.Context, symbol string) ([]Position, error)
}

// Client is the generic composite interface for any Exchange provider.
type Client interface {
	MarketDataProvider
	OrderExecutor
	AccountProvider
	WarmUp(ctx context.Context, interval time.Duration)
}
