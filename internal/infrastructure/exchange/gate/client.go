package gate

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/pkg/ticker"

	transportlog "github.com/dangnmh/transport"
	"github.com/gateio/gateapi-go/v7"
)

// Client is the Gate.io Perpetual Futures REST API client.
type Client struct {
	apiClient *gateapi.APIClient
	baseURL   string
	apiKey    string
	apiSecret string
	logCfg    config.LoggingConfig
	logger    *slog.Logger
}

// NewClient creates a new Gate.io Perpetual Futures REST API client.
func NewClient(httpClient *http.Client, baseURL, apiKey, apiSecret string, logCfg config.LoggingConfig) *Client {
	logger := slog.Default().With("component", "exchange").With("exchange", "gate")

	clientCopy := *httpClient

	if logCfg.HTTP && clientCopy.Transport != nil {
		rt := clientCopy.Transport
		rt = transportlog.NewTransportLog(rt,
			transportlog.LogOptionLogger(logger),
			transportlog.LogOptionMatcherConfig(transportlog.MatcherConfig{
				OnStatus:       []int{0},
				WhiteListPaths: []string{"*"}, // match all paths
				BlackListPaths: []string{},    // match everything cleanly
			}),
			transportlog.LogOptionRedactSensitive(true),
			transportlog.LogOptionRedactSensitiveKeys([]string{"ApiKey"}),
			transportlog.LogOptionQueryParams(true),
		)
		clientCopy.Transport = rt
	}

	cfg := gateapi.NewConfiguration()
	cfg.HTTPClient = &clientCopy
	if baseURL != "" {
		cfg.BasePath = strings.TrimRight(baseURL, "/")
	}

	apiClient := gateapi.NewAPIClient(cfg)

	return &Client{
		apiClient: apiClient,
		baseURL:   cfg.BasePath,
		apiKey:    apiKey,
		apiSecret: apiSecret,
		logCfg:    logCfg,
		logger:    logger,
	}
}

// authCtx returns a context decorated with Gate.io credentials.
func (c *Client) authCtx(ctx context.Context) context.Context {
	return context.WithValue(ctx, gateapi.ContextGateAPIV4, gateapi.GateAPIV4{
		Key:    c.apiKey,
		Secret: c.apiSecret,
	})
}

// WarmUp maintaining connection pool via periodic ping requests (GetSystemTime).
func (c *Client) WarmUp(ctx context.Context, interval time.Duration) {
	c.logger.InfoContext(ctx, "🔗 Warming up Gate.io connection pool...", slog.Duration("interval", interval))

	ticker.RunImmediate(ctx, interval, func() bool {
		_, httpResp, err := c.apiClient.SpotApi.GetSystemTime(ctx)
		if httpResp != nil && httpResp.Body != nil {
			_ = httpResp.Body.Close()
		}
		if err != nil {
			c.logger.DebugContext(ctx, "Gate.io warmup ping failed", slog.Any("error", err))
		}
		return true
	})
}

// Latency measures round-trip time of fetching server time (ms).
func (c *Client) Latency(ctx context.Context) (int64, error) {
	start := time.Now()
	_, httpResp, err := c.apiClient.SpotApi.GetSystemTime(ctx)
	if httpResp != nil && httpResp.Body != nil {
		_ = httpResp.Body.Close()
	}
	if err != nil {
		return 0, err
	}
	return time.Since(start).Milliseconds(), nil
}

// SupportLeverageOnOrder returns false since Gate.io doesn't support setting leverage directly on orders.
func (c *Client) SupportLeverageOnOrder() bool {
	return false
}
