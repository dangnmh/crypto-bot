package mexc

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/httpclient"
	"crypto-bot/pkg/ratelimit"
	"crypto-bot/pkg/xjson"

	transportlog "github.com/dangnmh/transport"
	"golang.org/x/time/rate"
)

// BaseClient encapsulates shared transport, authentication, signing, and rate limiting for MEXC.
type BaseClient struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	apiSecret  string
	logCfg     config.LoggingConfig
	logger     *slog.Logger
	clock      exchange.Clock
	limiter    *ratelimit.ExchangeRateLimiter
}

// NewBaseClient creates a new MEXC BaseClient.
func NewBaseClient(httpClient *http.Client, baseURL, apiKey, apiSecret string, logCfg config.LoggingConfig) *BaseClient {
	logger := slog.Default().With("component", "exchange").With("exchange", "mexc")

	var clientCopy http.Client
	if httpClient != nil {
		clientCopy = *httpClient
	}
	if clientCopy.Transport == nil {
		clientCopy.Transport = http.DefaultTransport
	}

	if logCfg.HTTP {
		rt := clientCopy.Transport
		rt = transportlog.NewTransportLog(rt,
			transportlog.LogOptionLogger(logger),
			transportlog.LogOptionMatcherConfig(transportlog.MatcherConfig{
				OnStatus:       []int{0},
				WhiteListPaths: []string{"*"},
				BlackListPaths: []string{
					"GET|/api/v1/contract/ping",
					"GET|/api/v1/contract/ticker",
					"GET|/api/v1/contract/detail",
					"GET|/api/v1/contract/funding_rate/*",
					"GET|/api/v1/contract/kline/*",
					"GET|/api/v1/contract/depth/*",
					"GET|/api/v1/contract/depth_commits/*",
					"GET|/api/v3/ticker/24hr",
					"GET|/api/v3/depth",
					"GET|/api/v3/exchangeInfo",
				},
			}),
			transportlog.LogOptionRedactSensitive(true),
			transportlog.LogOptionRedactSensitiveKeys([]string{"ApiKey", "X-MEXC-APIKEY"}),
			transportlog.LogOptionQueryParams(true),
		)
		clientCopy.Transport = rt
	}
	clientCopy.Transport = httpclient.WrapWithRequestID(clientCopy.Transport)

	configs := map[string]ratelimit.EndpointConfig{
		"/api/v1/contract/depth/":         {Limit: rate.Limit(3), Burst: 1, Weight: 1},
		"/api/v1/contract/depth_commits/": {Limit: rate.Limit(5), Burst: 2, Weight: 1},
		"/api/v3/depth":                   {Limit: rate.Limit(5), Burst: 2, Weight: 3},
	}
	limiter := ratelimit.NewExchangeRateLimiter(rate.Limit(20), 5, configs)

	return &BaseClient{
		httpClient: &clientCopy,
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		apiSecret:  apiSecret,
		logCfg:     logCfg,
		logger:     logger,
		clock:      exchange.RealClock{},
		limiter:    limiter,
	}
}

// HTTPClient returns the configured *http.Client.
func (c *BaseClient) HTTPClient() *http.Client {
	return c.httpClient
}

// BaseURL returns the base URL.
func (c *BaseClient) BaseURL() string {
	return c.baseURL
}

// APIKey returns the API key.
func (c *BaseClient) APIKey() string {
	return c.apiKey
}

// APISecret returns the API secret.
func (c *BaseClient) APISecret() string {
	return c.apiSecret
}

// Logger returns the logger.
func (c *BaseClient) Logger() *slog.Logger {
	return c.logger
}

// Clock returns the clock.
func (c *BaseClient) Clock() exchange.Clock {
	return c.clock
}

// SetClock sets a custom clock.
func (c *BaseClient) SetClock(clk exchange.Clock) {
	if clk != nil {
		c.clock = clk
	}
}

// Limiter returns the rate limiter.
func (c *BaseClient) Limiter() *ratelimit.ExchangeRateLimiter {
	return c.limiter
}

func buildFullURL(baseURL, path string, params map[string]any) string {
	if len(params) == 0 {
		return baseURL + path
	}
	query := url.Values{}
	for k, v := range params {
		query.Set(k, fmt.Sprintf("%v", v))
	}
	encoded := query.Encode()
	if strings.Contains(path, "?") {
		return baseURL + path + "&" + encoded
	}
	return baseURL + path + "?" + encoded
}

func (c *BaseClient) applyFuturesAuth(req *http.Request, method string, params map[string]any, body []byte) {
	if c.apiKey == "" {
		return
	}
	timestamp := strconv.FormatInt(c.clock.Now().UnixMilli(), 10)
	var signTarget any
	if method == http.MethodGet || method == http.MethodDelete {
		signTarget = params
	} else if len(body) > 0 {
		signTarget = body
	}
	signature := SignRequest(c.apiKey, c.apiSecret, timestamp, method, signTarget)
	req.Header.Set("ApiKey", c.apiKey)
	req.Header.Set("Request-Time", timestamp)
	req.Header.Set("Signature", signature)
}

// Request executes an HTTP request with rate limiting and optional MEXC futures header authentication.
func (c *BaseClient) Request(ctx context.Context, method, path string, params map[string]any, body []byte, signed bool) ([]byte, error) {
	if c.limiter != nil {
		if err := c.limiter.Acquire(ctx, path); err != nil {
			return nil, fmt.Errorf("rate limiter: %w", err)
		}
	}

	fullURL := buildFullURL(c.baseURL, path, params)

	var reqBody io.Reader
	if len(body) > 0 {
		reqBody = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if signed {
		c.applyFuturesAuth(req, method, params, body)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &exchange.APIError{
			StatusCode: resp.StatusCode,
			Message:    string(respBody),
			Path:       path,
		}
	}

	return respBody, nil
}

// RequestSpot executes a signed or unsigned request using MEXC Spot v3 API query parameter signing format.
func (c *BaseClient) RequestSpot(ctx context.Context, method, path string, params map[string]string, signed bool) ([]byte, error) {
	if c.limiter != nil {
		if err := c.limiter.Acquire(ctx, path); err != nil {
			return nil, fmt.Errorf("rate limiter: %w", err)
		}
	}

	fullURL := c.baseURL + path
	query := url.Values{}
	for k, v := range params {
		query.Set(k, v)
	}

	if signed {
		query.Set("timestamp", strconv.FormatInt(c.clock.Now().UnixMilli(), 10))
		query.Set("recvWindow", "5000")
		sig := SignSpot(query.Encode(), c.apiSecret)
		query.Set("signature", sig)
	}

	queryString := query.Encode()
	if queryString != "" {
		if strings.Contains(fullURL, "?") {
			fullURL += "&" + queryString
		} else {
			fullURL += "?" + queryString
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create spot request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("X-MEXC-APIKEY", c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute spot request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read spot response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &exchange.APIError{
			StatusCode: resp.StatusCode,
			Message:    string(respBody),
			Path:       path,
		}
	}

	return respBody, nil
}

// ParseFuturesResponse unmarshals a MEXC futures response envelope.
func ParseFuturesResponse[T any](data []byte) (*APIResponse[T], error) {
	var resp APIResponse[T]
	if err := xjson.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal mexc futures response: %w", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("mexc futures api error: code=%d message=%s", resp.Code, resp.Message)
	}
	return &resp, nil
}
