package bybit

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/pkg/ticker"

	bybitsdk "github.com/bybit-exchange/bybit.go.api"
)

// Client is the Bybit V5 Perpetual Futures REST API client.
type Client struct {
	sdkClient   *bybitsdk.Client
	baseURL     string
	apiKey      string
	apiSecret   string
	accountType string // "standard" or "unified"
	logCfg      config.LoggingConfig
	logger      *slog.Logger
}

// NewClient creates a new Bybit client.
func NewClient(httpClient *http.Client, baseURL, apiKey, apiSecret, accountType string, logCfg config.LoggingConfig) *Client {
	logger := slog.Default().With("component", "exchange").With("exchange", "bybit")

	sdkOpts := []bybitsdk.ClientOption{}
	if baseURL != "" {
		sdkOpts = append(sdkOpts, bybitsdk.WithBaseURL(baseURL))
	}

	sdkClient := bybitsdk.NewBybitHttpClient(apiKey, apiSecret, sdkOpts...)
	if httpClient != nil {
		sdkClient.HTTPClient = httpClient
	}

	return &Client{
		sdkClient:   sdkClient,
		baseURL:     baseURL,
		apiKey:      apiKey,
		apiSecret:   apiSecret,
		accountType: accountType,
		logCfg:      logCfg,
		logger:      logger,
	}
}

// WarmUp maintains the connection pool.
func (c *Client) WarmUp(ctx context.Context, interval time.Duration) {
	ticker.RunImmediate(ctx, interval, func() bool {
		_, err := c.sdkClient.NewUtaBybitServiceNoParams().GetServerTime(ctx)
		if err != nil {
			c.logger.Debug("Bybit warmup ping failed", "error", err)
		}
		return true
	})
}
