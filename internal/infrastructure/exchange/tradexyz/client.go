package tradexyz

import (
	"context"
	"net/http"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/hyperliquid"
)

// Client wraps the Hyperliquid client to integrate tradeXYZ.
type Client struct {
	hlClient *hyperliquid.Client
}

// NewClient creates a new tradeXYZ client.
func NewClient(httpClient *http.Client, baseURL string, logCfg config.LoggingConfig) *Client {
	if baseURL == "" {
		baseURL = "https://api.hyperliquid.xyz"
	}
	return &Client{
		hlClient: hyperliquid.NewClient(context.Background(), httpClient, baseURL, "", "", logCfg),
	}
}

// GetPotentialFundingSymbols fetches all perpetual contracts, their tickers, and estimated funding rates.
func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	return c.hlClient.GetPotentialFundingSymbols(ctx, minVol24h, maxVol24h, whitelist, blacklist)
}

func (c *Client) GetTopGainer(_ context.Context, _ exchange.TopGainerRequest) ([]exchange.TopGainerResult, error) {
	return nil, nil
}
