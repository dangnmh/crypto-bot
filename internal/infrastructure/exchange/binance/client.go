package binance

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/pkg/ticker"

	"github.com/binance/binance-connector-go/clients/derivativestradingusdsfutures"
	binancecommon "github.com/binance/binance-connector-go/common/v2/common"
)

// Client is the Binance USD-M Futures REST API client.
type Client struct {
	sdkClient *derivativestradingusdsfutures.BinanceDerivativesTradingUsdsFuturesClient
	baseURL   string
	apiKey    string
	apiSecret string
	logCfg    config.LoggingConfig
	logger    *slog.Logger
}

// NewClient creates a new Binance Futures REST Client.
func NewClient(httpClient *http.Client, baseURL, apiKey, apiSecret string, logCfg config.LoggingConfig) *Client {
	logger := slog.Default().With("component", "exchange", "exchange", "binance")

	restOpts := []binancecommon.ConfigurationRestAPIOption{}
	if baseURL != "" {
		restOpts = append(restOpts, binancecommon.WithBasePath(baseURL))
	}
	if apiKey != "" {
		restOpts = append(restOpts, binancecommon.WithApiKey(apiKey))
	}
	if apiSecret != "" {
		restOpts = append(restOpts, binancecommon.WithApiSecret(apiSecret))
	}

	restCfg := binancecommon.NewConfigurationRestAPI(restOpts...)

	// Configure default timeout
	restCfg.Timeout = 10 * time.Second

	// Setup client using the SDK
	sdkClient := derivativestradingusdsfutures.NewBinanceDerivativesTradingUsdsFuturesClient(
		derivativestradingusdsfutures.WithRestAPI(restCfg),
	)

	return &Client{
		sdkClient: sdkClient,
		baseURL:   baseURL,
		apiKey:    apiKey,
		apiSecret: apiSecret,
		logCfg:    logCfg,
		logger:    logger,
	}
}

// WarmUp maintains the connection pool.
func (c *Client) WarmUp(ctx context.Context, interval time.Duration) {
	ticker.RunImmediate(ctx, interval, func() bool {
		req := c.sdkClient.RestApi.MarketDataAPI.TestConnectivity(ctx)
		_, err := c.sdkClient.RestApi.MarketDataAPI.TestConnectivityExecute(req)
		if err != nil {
			c.logger.Debug("Binance warmup connectivity check failed", slog.Any("error", err))
		}
		return true
	})
}

// Latency measures round-trip time of fetching server time (ms).
func (c *Client) Latency(ctx context.Context) (int64, error) {
	start := time.Now()
	req := c.sdkClient.RestApi.MarketDataAPI.TestConnectivity(ctx)
	_, err := c.sdkClient.RestApi.MarketDataAPI.TestConnectivityExecute(req)
	if err != nil {
		return 0, err
	}
	return time.Since(start).Milliseconds(), nil
}
