package futures

import (
	"context"
	"net/http"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/mexc"
)

var (
	_ exchange.Client               = (*Client)(nil)
	_ exchange.DepthProvider        = (*Client)(nil)
	_ exchange.DepthCommitsProvider = (*Client)(nil)
	_ exchange.TopGainerProvider    = (*Client)(nil)
	_ exchange.OrderExecutor        = (*Client)(nil)
	_ exchange.PreSignExecutor      = (*Client)(nil)
	_ exchange.PreWarmer            = (*Client)(nil)
	_ exchange.TPSLProvider         = (*Client)(nil)
)

// Client is the MEXC Futures REST API client.
type Client struct {
	base *mexc.BaseClient
}

// NewClient creates a new MEXC Futures API client.
func NewClient(httpClient *http.Client, baseURL, apiKey, apiSecret string, logCfg config.LoggingConfig) *Client {
	return &Client{
		base: mexc.NewBaseClient(httpClient, baseURL, apiKey, apiSecret, logCfg),
	}
}

// BaseClient returns the underlying shared MEXC BaseClient.
func (c *Client) BaseClient() *mexc.BaseClient {
	return c.base
}

// SetOrderHTTPClient sets the dedicated HTTP client for order execution.
func (c *Client) SetOrderHTTPClient(httpClient *http.Client) {
	c.base.SetOrderHTTPClient(httpClient)
}

// PreWarm pre-warms the dedicated order HTTP client connection.
func (c *Client) PreWarm(ctx context.Context) error {
	return c.base.PreWarm(ctx)
}

// SetClock configures a custom clock implementation for testing.
func (c *Client) SetClock(clk exchange.Clock) {
	c.base.SetClock(clk)
}

// IsFutures returns true for Futures client.
func (c *Client) IsFutures() bool {
	return true
}
