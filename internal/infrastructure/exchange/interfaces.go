package exchange

import "context"

// MarketDataProvider is the interface for reading market data.
// Satisfied by *Client. Enables mock-based testing without hitting the real exchange.
type MarketDataProvider interface {
	GetTickers(ctx context.Context, symbol string) ([]Ticker, error)
	GetContractDetails(ctx context.Context) ([]ContractDetail, error)
	GetFundingRate(ctx context.Context, symbol string) (*FundingRateDetail, error)
	GetServerTime(ctx context.Context) (int64, error)
	GetKlines(ctx context.Context, symbol, interval string, start, end int64) ([]Kline, error)
}

// OrderExecutor is the interface for placing and managing orders.
// Satisfied by *Client. Enables mock-based testing without hitting the real exchange.
type OrderExecutor interface {
	CreateOrder(ctx context.Context, req SubmitOrderRequest) (string, error)
	CancelOrder(ctx context.Context, symbol, orderID string) error
	GetOrder(ctx context.Context, orderID string) (*OrderInfo, error)
	GetOpenOrders(ctx context.Context, symbol string) ([]OrderInfo, error)
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

// Compile-time interface compliance checks.
var (
	_ MarketDataProvider = (*Client)(nil)
	_ OrderExecutor      = (*Client)(nil)
	_ AccountProvider    = (*Client)(nil)
)
