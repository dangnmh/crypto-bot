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
	GetTopGainer(ctx context.Context, req TopGainerRequest) ([]TopGainerResult, error)
	GetContractDetails(ctx context.Context) ([]ContractDetail, error)
	GetFundingRates(ctx context.Context, symbols []string) ([]FundingRateResult, error)
	GetServerTime(ctx context.Context) (int64, error)
	GetPotentialFundingSymbols(ctx context.Context, minVol24h, maxVol24h float64, whitelist []string, blacklist []string) ([]PotentialFundingResult, error)
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
	CancelOrder(ctx context.Context, symbol, orderID string) error
	CancelOrders(ctx context.Context, orderIDs []string) error
	CancelAllOpenOrders(ctx context.Context, symbol string) error
	GetOrder(ctx context.Context, symbol, orderID string) (*OrderInfo, error)
	GetOrderByExternalID(ctx context.Context, symbol, externalOrderID string) (*OrderInfo, error)
	GetOpenOrders(ctx context.Context, symbol string) ([]OrderInfo, error)
	GetOpenPositions(ctx context.Context, symbol string) ([]Position, error)
	ClosePosition(ctx context.Context, symbol string, closeSide domain.Side, volume float64, positionMode domain.PositionMode, leverage int) error
	CloseAllPositions(ctx context.Context, symbol string) error
	ChangeLeverage(ctx context.Context, req ChangeLeverageRequest) error
	SwitchMarginMode(ctx context.Context, symbol string, marginMode domain.MarginMode, leverage int, side domain.Side) error
}

// TPSLProvider is an optional interface that exchange REST clients can implement
// to support post-fill Take Profit and Stop Loss configuration.
type TPSLProvider interface {
	PlaceTPSL(ctx context.Context, req TPSLRequest) error
}

// Interval represents candlestick granularity.
type Interval string

const (
	Interval1m  Interval = "1m"
	Interval3m  Interval = "3m"
	Interval5m  Interval = "5m"
	Interval15m Interval = "15m"
	Interval30m Interval = "30m"
	Interval1h  Interval = "1h"
	Interval2h  Interval = "2h"
	Interval4h  Interval = "4h"
	Interval6h  Interval = "6h"
	Interval8h  Interval = "8h"
	Interval12h Interval = "12h"
	Interval1d  Interval = "1d"
	Interval1w  Interval = "1w"
	Interval1M  Interval = "1M"
)

// KlineProvider is an optional interface that exchange REST clients can implement
// to support retrieving historical candlestick (K-line) data.
type KlineProvider interface {
	FetchKlines(ctx context.Context, symbol string, interval Interval, start, end time.Time) ([]Kline, error)
}

// PositionModeSwitcher is an optional interface that exchange REST clients can implement
// to support switching position mode (Hedge vs One-Way).
type PositionModeSwitcher interface {
	SwitchPositionMode(ctx context.Context, symbol string, positionMode domain.PositionMode) error
}

// MaxLeverageProvider is an optional interface that exchange REST clients can implement
// to support retrieving the maximum leverage allowed for a symbol.
type MaxLeverageProvider interface {
	GetMaxLeverage(ctx context.Context, symbol string) (int, error)
}

// RiskLimitLeverageProvider is an optional interface that exchange REST clients can implement
// to retrieve the maximum leverage allowed for a symbol given a target position notional value.
type RiskLimitLeverageProvider interface {
	GetMaxLeverageForValue(ctx context.Context, symbol string, value float64) (int, error)
}

// ClosedPnLInfo represents the standardized historical ledger of a closed trade.
type ClosedPnLInfo struct {
	Exchange           string
	Symbol             string
	EntryPrice         float64
	ExitPrice          float64
	ClosedSizeContract *float64
	ClosedSizeCoin     *float64
	GrossPnL           float64
	Fee                float64
	FundingFee         float64
	DurationMs         int64
	NetPnl             float64
	PnLRate            float64
}

// ClosedPnLProvider is an optional interface that exchange REST clients can implement.
type ClosedPnLProvider interface {
	GetOrderPNL(ctx context.Context, symbol, orderID string) (*ClosedPnLInfo, error)
}

// Client is the generic composite interface for any Exchange provider.
type Client interface {
	MarketDataProvider
	OrderExecutor
	WarmUp(ctx context.Context, interval time.Duration)
	SupportLeverageOnOrder() bool
}

// Clock represents a source of time, returning the current time.
type Clock interface {
	Now() time.Time
}

// RealClock is a Clock implementation that uses time.Now().
type RealClock struct{}

func (RealClock) Now() time.Time {
	return time.Now()
}

// RawRequester is an optional interface that exchange REST clients can implement
// to allow raw, signed HTTP requests for debugging or proxying.
type RawRequester interface {
	RawRequest(ctx context.Context, method, path string, query map[string]string, body []byte) ([]byte, error)
}

// RawRequest is a unified interface that exposes exchange endpoints returning raw bytes.
type RawRequest interface {
	GetFundingRateRaw(ctx context.Context, params map[string]string) ([]byte, error)
	GetTickersRaw(ctx context.Context, params map[string]string) ([]byte, error)
	GetOpenPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error)
	GetHistoryPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error)
	GetOrderDetailRaw(ctx context.Context, orderID string, params map[string]string) ([]byte, error)
	GetHistoryOrdersRaw(ctx context.Context, params map[string]string) ([]byte, error)
	GetOrderPNLRaw(ctx context.Context, params map[string]string) ([]byte, error)
}

// PotentialFundingResult holds combined information for active query results in scanner.
type PotentialFundingResult struct {
	Symbol     string  `json:"symbol"`
	Rate       float64 `json:"rate"`
	SettleTime int64   `json:"settleTime"`
	Volume24h  float64 `json:"volume24h"`
	Price      float64 `json:"price"`
}

// BackgroundTaskRunner is implemented by clients needing persistent background execution.
type BackgroundTaskRunner interface {
	StartBackgroundTasks(ctx context.Context)
}
