package mexc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/httpclient"
	"crypto-bot/pkg/ticker"

	transportlog "github.com/dangnmh/transport"
)

// Client is the MEXC Futures REST API client with connection pooling.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	apiSecret  string
	logCfg     config.LoggingConfig
	logger     *slog.Logger
	clock      exchange.Clock
}

// NewClient creates a new MEXC API client using the provided optimized connection pool.
func NewClient(httpClient *http.Client, baseURL, apiKey, apiSecret string, logCfg config.LoggingConfig) *Client {
	logger := slog.Default().With("component", "exchange").With("exchange", "mexc")

	clientCopy := *httpClient

	if clientCopy.Transport != nil {
		if logCfg.HTTP {
			rt := clientCopy.Transport
			rt = transportlog.NewTransportLog(rt,
				transportlog.LogOptionLogger(logger),
				transportlog.LogOptionMatcherConfig(transportlog.MatcherConfig{
					OnStatus:       []int{0},
					WhiteListPaths: []string{"*"}, // match all paths
					BlackListPaths: []string{
						"GET|/api/v1/contract/ping",
						"GET|/api/v1/contract/ticker",
						"GET|/api/v1/contract/detail",
						"GET|/api/v1/contract/funding_rate/*",
						"GET|/api/v1/contract/kline/*",
					}, // ignore ping spam
				}),
				transportlog.LogOptionRedactSensitive(true),
				transportlog.LogOptionRedactSensitiveKeys([]string{"ApiKey"}),
				transportlog.LogOptionQueryParams(true),
			)
			clientCopy.Transport = rt
		}
		clientCopy.Transport = httpclient.WrapWithRequestID(clientCopy.Transport)
	}

	return &Client{
		httpClient: &clientCopy,
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		apiSecret:  apiSecret,
		logCfg:     logCfg,
		logger:     logger,
		clock:      exchange.RealClock{},
	}
}

// SetClock configures a custom clock implementation.
func (c *Client) SetClock(clk exchange.Clock) {
	if clk != nil {
		c.clock = clk
	}
}

// WarmUp pre-establishes connection pool and maintains it via periodic ping requests.
func (c *Client) WarmUp(ctx context.Context, interval time.Duration) {
	c.logger.InfoContext(ctx, "🔗 Warming up connection pool...", slog.Duration("interval", interval))

	ticker.RunImmediate(ctx, interval, func() bool {
		_, err := c.GetCtx(ctx, "/api/v1/contract/ping", nil)
		if err != nil {
			c.logger.DebugContext(ctx, "Warmup ping failed", slog.Any("error", err))
		}
		return true
	})
}

// Get makes a signed GET request to a private or public endpoint.
func (c *Client) Get(ctx context.Context, path string, params map[string]any) ([]byte, error) {
	return c.GetCtx(ctx, path, params)
}

// GetCtx makes a signed GET request with context.
func (c *Client) GetCtx(ctx context.Context, path string, params map[string]any) ([]byte, error) {
	query := make(map[string]string)
	for k, v := range params {
		query[k] = fmt.Sprintf("%v", v)
	}
	return c.RawRequest(ctx, http.MethodGet, path, query, nil)
}

// Post makes a signed POST request to a private endpoint.
func (c *Client) Post(ctx context.Context, path string, body any) ([]byte, error) {
	return c.PostCtx(ctx, path, body)
}

// PostCtx makes a signed POST request with context.
func (c *Client) PostCtx(ctx context.Context, path string, body any) ([]byte, error) {
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal POST body: %w", err)
		}
	}
	return c.RawRequest(ctx, http.MethodPost, path, nil, bodyBytes)
}

// RawRequest makes a signed HTTP request of any method to the MEXC API.
func (c *Client) RawRequest(ctx context.Context, method, path string, query map[string]string, body []byte) ([]byte, error) {
	reqURL, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}
	isPrivate := strings.Contains(path, "/private/")

	// Build query string
	params := make(map[string]any)
	for k, vs := range reqURL.Query() {
		if len(vs) > 0 {
			params[k] = vs[0]
		}
	}
	for k, v := range query {
		params[k] = v
	}

	if len(params) > 0 {
		qs := buildSortedQueryString(params)
		reqURL.RawQuery = qs
	}
	urlPath := reqURL.String()

	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, urlPath, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create %s request: %w", method, err)
	}

	req.Header.Set("Content-Type", "application/json")

	if isPrivate && c.apiKey != "" {
		ts := strconv.FormatInt(c.clock.Now().UnixMilli(), 10)
		var signTarget any
		if method == http.MethodGet || method == http.MethodDelete {
			signTarget = params
		} else if len(body) > 0 {
			signTarget = body
		}
		sig := SignRequest(c.apiKey, c.apiSecret, ts, method, signTarget)
		req.Header.Set("ApiKey", c.apiKey)
		req.Header.Set("Request-Time", ts)
		req.Header.Set("Signature", sig)
	}

	return c.doRequest(ctx, req)
}

// doRequest executes the HTTP request and returns the response body.
func (c *Client) doRequest(ctx context.Context, req *http.Request) ([]byte, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP %s %s: %w", req.Method, req.URL.Path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		path := req.URL.Path

		// Rate limit gets a dedicated error type for programmatic handling.
		if isRateLimited(resp.StatusCode) {
			return nil, &exchange.RateLimitError{
				Message: string(body),
				Path:    path,
			}
		}

		c.logger.WarnContext(ctx, "🟡 Non-200 response",
			"status", resp.StatusCode,
			"path", path,
			"body", string(body),
		)
		return nil, toHTTPError(resp.StatusCode, body, path)
	}

	return body, nil
}

// Latency measures the round-trip time of a ping request (ms).
func (c *Client) Latency(ctx context.Context) (int64, error) {
	start := time.Now()
	_, err := c.Get(ctx, "/api/v1/contract/ping", nil)
	if err != nil {
		return 0, err
	}
	return time.Since(start).Milliseconds(), nil
}

// SupportLeverageOnOrder returns false since MEXC doesn't support setting leverage directly on orders.
func (c *Client) SupportLeverageOnOrder() bool {
	return true
}

func (c *Client) GetAssetsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/api/v1/private/account/assets", params, nil)
}

func (c *Client) GetFundingRateRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/api/v1/contract/funding_rate", params, nil)
}

func (c *Client) GetTickersRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/api/v1/contract/ticker", params, nil)
}

func (c *Client) GetOpenPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/api/v1/private/position/open_positions", params, nil)
}

func (c *Client) GetHistoryPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/api/v1/private/position/list/history_positions", params, nil)
}

func (c *Client) GetOrderDetailRaw(ctx context.Context, orderID string, params map[string]string) ([]byte, error) {
	path := fmt.Sprintf("/api/v1/private/order/get/%s", orderID)
	return c.RawRequest(ctx, http.MethodGet, path, params, nil)
}

func (c *Client) GetOrdersRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/api/v1/private/order/open_orders/", params, nil)
}

func (c *Client) GetOrderDealsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/api/v1/private/order/list/order_deals/v3", params, nil)
}

func (c *Client) GetClosedPnLRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/api/v1/private/position/list/history_positions", params, nil)
}
