package spot

import (
	"context"
	"net/http"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/mexc"
)

var (
	_ exchange.Client            = (*Client)(nil)
	_ exchange.DepthProvider     = (*Client)(nil)
	_ exchange.TopGainerProvider = (*Client)(nil)
)

// Client is the MEXC Spot REST API client.
type Client struct {
	base *mexc.BaseClient
}

// NewClient creates a new MEXC Spot API client.
func NewClient(httpClient *http.Client, baseURL, apiKey, apiSecret string, logCfg config.LoggingConfig) *Client {
	return &Client{
		base: mexc.NewBaseClient(httpClient, baseURL, apiKey, apiSecret, logCfg),
	}
}

// BaseClient returns the underlying shared MEXC BaseClient.
func (c *Client) BaseClient() *mexc.BaseClient {
	return c.base
}

// SetClock configures a custom clock implementation for testing.
func (c *Client) SetClock(clk exchange.Clock) {
	c.base.SetClock(clk)
}

// IsFutures returns false for Spot client.
func (c *Client) IsFutures() bool {
	return false
}

// SupportLeverageOnOrder returns false for spot.
func (c *Client) SupportLeverageOnOrder() bool {
	return false
}

// WarmUp pre-warms the connection pool.
func (c *Client) WarmUp(ctx context.Context, interval time.Duration) {}

// GetTickers returns empty tickers for spot in this phase.
func (c *Client) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	return nil, nil
}

// GetFundingRates is not applicable for spot.
func (c *Client) GetFundingRates(ctx context.Context, symbols []string) ([]exchange.FundingRateResult, error) {
	return nil, nil
}

// GetPotentialFundingSymbols is not applicable for spot.
func (c *Client) GetPotentialFundingSymbols(ctx context.Context, minVol24h, maxVol24h float64, whitelist, blacklist []string) ([]exchange.PotentialFundingResult, error) {
	return nil, nil
}

// CreateOrder is not supported for spot.
func (c *Client) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
	return exchange.CreateOrderResult{}, exchange.ErrNotSupported
}

// CancelOrder is not supported for spot.
func (c *Client) CancelOrder(ctx context.Context, symbol, orderID string) error {
	return exchange.ErrNotSupported
}

// CancelOrders is not supported for spot.
func (c *Client) CancelOrders(ctx context.Context, orderIDs []string) error {
	return exchange.ErrNotSupported
}

// CancelAllOpenOrders is not supported for spot.
func (c *Client) CancelAllOpenOrders(ctx context.Context, symbol string) error {
	return exchange.ErrNotSupported
}

// GetOrder is not supported for spot.
func (c *Client) GetOrder(ctx context.Context, symbol, orderID string) (*exchange.OrderInfo, error) {
	return nil, exchange.ErrNotSupported
}

// GetOrderByExternalID is not supported for spot.
func (c *Client) GetOrderByExternalID(ctx context.Context, symbol, externalOrderID string) (*exchange.OrderInfo, error) {
	return nil, exchange.ErrNotSupported
}

// GetOpenOrders is not supported for spot.
func (c *Client) GetOpenOrders(ctx context.Context, symbol string) ([]exchange.OrderInfo, error) {
	return nil, exchange.ErrNotSupported
}

// GetOpenPositions is not supported for spot.
func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	return nil, nil
}

// ClosePosition is not supported for spot.
func (c *Client) ClosePosition(ctx context.Context, symbol string, closeSide domain.Side, volume float64, positionMode domain.PositionMode, leverage int) error {
	return exchange.ErrNotSupported
}

// CloseAllPositions is not supported for spot.
func (c *Client) CloseAllPositions(ctx context.Context, symbol string) error {
	return exchange.ErrNotSupported
}

// ChangeLeverage is not supported for spot.
func (c *Client) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	return exchange.ErrNotSupported
}

// SwitchMarginMode is not supported for spot.
func (c *Client) SwitchMarginMode(ctx context.Context, symbol string, marginMode domain.MarginMode, leverage int, side domain.Side) error {
	return exchange.ErrNotSupported
}
