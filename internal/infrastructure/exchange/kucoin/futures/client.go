package futures

import (
	"net/http"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/kucoin"
)

var (
	_ exchange.Client               = (*Client)(nil)
	_ exchange.DepthProvider        = (*Client)(nil)
	_ exchange.DepthCommitsProvider = (*Client)(nil)
	_ exchange.KlineProvider        = (*Client)(nil)
	_ exchange.TPSLProvider         = (*Client)(nil)
	_ exchange.TopGainerProvider    = (*Client)(nil)
	_ exchange.OrderExecutor        = (*Client)(nil)
)

// Client is the KuCoin Futures REST API client.
type Client struct {
	base *kucoin.BaseClient
}

// NewClient creates a new KuCoin Futures API client.
func NewClient(
	httpClient *http.Client,
	baseURL string,
	apiKey string,
	apiSecret string,
	passphrase string,
	logCfg config.LoggingConfig,
) *Client {
	return &Client{
		base: kucoin.NewBaseClient(httpClient, baseURL, apiKey, apiSecret, passphrase, logCfg),
	}
}

// BaseClient returns the underlying shared KuCoin BaseClient.
func (c *Client) BaseClient() *kucoin.BaseClient {
	return c.base
}

// SetClock configures a custom clock implementation for testing.
func (c *Client) SetClock(clk exchange.Clock) {
	c.base.SetClock(clk)
}

// IsFutures returns true for Futures client.
func (c *Client) IsFutures() bool {
	return true
}
