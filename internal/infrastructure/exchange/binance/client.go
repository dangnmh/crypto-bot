package binance

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/pkg/ticker"

	transportlog "github.com/dangnmh/transport"

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

	var rt http.RoundTripper
	if httpClient != nil && httpClient.Transport != nil {
		rt = httpClient.Transport
	}

	if logCfg.HTTP && rt != nil {
		rt = &decompressionRoundTripper{underlying: rt}
		rt = transportlog.NewTransportLog(rt,
			transportlog.LogOptionLogger(logger),
			transportlog.LogOptionMatcherConfig(transportlog.MatcherConfig{
				OnStatus:       []int{0},
				WhiteListPaths: []string{"*"},
				BlackListPaths: []string{
					"GET|/fapi/v1/ping",
					"GET|/fapi/v1/time",
					"GET|/fapi/v1/ticker/24hr",
					"GET|/fapi/v1/ticker/bookTicker",
					"GET|/fapi/v1/exchangeInfo",
					"GET|/fapi/v1/premiumIndex",
					"POST|/fapi/v1/listenKey",
				},
			}),
			transportlog.LogOptionRedactSensitive(true),
			transportlog.LogOptionRedactSensitiveKeys([]string{
				"X-Mbx-Apikey",
			}),
			transportlog.LogOptionQueryParams(true),
		)
	}

	if rt != nil {
		restOpts = append(restOpts, binancecommon.WithHTTPSAgent(rt))
	}
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

type decompressionRoundTripper struct {
	underlying http.RoundTripper
}

func (d *decompressionRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("Accept-Encoding") != "" {
		req.Header.Set("Accept-Encoding", "gzip")
	}

	resp, err := d.underlying.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	if resp.Header.Get("Content-Encoding") == "gzip" {
		gzReader, gzErr := gzip.NewReader(resp.Body)
		if gzErr == nil {
			resp.Body = &gzipReadCloser{
				gz:   gzReader,
				body: resp.Body,
			}
			resp.Header.Del("Content-Encoding")
			resp.Header.Del("Content-Length")
		}
	}

	return resp, nil
}

type gzipReadCloser struct {
	gz   *gzip.Reader
	body io.ReadCloser
}

func (g *gzipReadCloser) Read(p []byte) (int, error) {
	return g.gz.Read(p)
}

func (g *gzipReadCloser) Close() error {
	err1 := g.gz.Close()
	err2 := g.body.Close()
	if err1 != nil {
		return err1
	}
	return err2
}

// CreateListenKey starts a new Binance user data stream and returns its listenKey.
func (c *Client) CreateListenKey(ctx context.Context) (string, error) {
	req := c.sdkClient.RestApi.UserDataStreamsAPI.StartUserDataStream(ctx)
	resp, err := c.sdkClient.RestApi.UserDataStreamsAPI.StartUserDataStreamExecute(req)
	if err != nil {
		return "", fmt.Errorf("binance start user data stream: %w", err)
	}
	return resp.Data.GetListenKey(), nil
}

// KeepAliveListenKey pings the active Binance user data stream to keep it open.
func (c *Client) KeepAliveListenKey(ctx context.Context) error {
	req := c.sdkClient.RestApi.UserDataStreamsAPI.KeepaliveUserDataStream(ctx)
	_, err := c.sdkClient.RestApi.UserDataStreamsAPI.KeepaliveUserDataStreamExecute(req)
	if err != nil {
		return fmt.Errorf("binance keepalive user data stream: %w", err)
	}
	return nil
}

// SupportLeverageOnOrder returns false since Binance doesn't support setting leverage directly on orders.
func (c *Client) SupportLeverageOnOrder() bool {
	return false
}
