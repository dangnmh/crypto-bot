package bybit

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/pkg/ticker"

	bybitsdk "github.com/bybit-exchange/bybit.go.api"
	transportlog "github.com/dangnmh/transport"
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

	var clientCopy http.Client
	if httpClient != nil {
		clientCopy = *httpClient
	}

	if logCfg.HTTP && httpClient != nil && clientCopy.Transport != nil {
		rt := clientCopy.Transport
		rt = transportlog.NewTransportLog(rt,
			transportlog.LogOptionLogger(logger),
			transportlog.LogOptionMatcherConfig(transportlog.MatcherConfig{
				OnStatus:       []int{0},
				WhiteListPaths: []string{"*"}, // match all paths
				BlackListPaths: []string{
					"GET|/v5/market/tickers",
					"GET|/v5/market/time",
					"GET|/v5/market/instruments-info",
				}, // match everything cleanly
			}),
			transportlog.LogOptionRedactSensitive(true),
			transportlog.LogOptionRedactSensitiveKeys([]string{"X-Bapi-Api-Key"}),
		)
		clientCopy.Transport = rt
	}

	sdkOpts := []bybitsdk.ClientOption{}
	if baseURL != "" {
		sdkOpts = append(sdkOpts, bybitsdk.WithBaseURL(baseURL))
	}

	sdkClient := bybitsdk.NewBybitHttpClient(apiKey, apiSecret, sdkOpts...)
	if httpClient != nil {
		sdkClient.HTTPClient = &clientCopy
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
			c.logger.Debug("Bybit warmup ping failed", slog.Any("error", err))
		}
		return true
	})
}
