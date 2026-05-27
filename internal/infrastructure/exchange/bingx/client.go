package bingx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptrace"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/ticker"

	transportlog "github.com/dangnmh/transport"
)

// Client is the BingX Futures Swap V2 REST API client.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	apiSecret  string
	logCfg     config.LoggingConfig
	logger     *slog.Logger
}

// NewClient creates a new BingX API client using the provided HTTP client.
func NewClient(httpClient *http.Client, baseURL, apiKey, apiSecret string, logCfg config.LoggingConfig) *Client {
	logger := slog.Default().With("component", "exchange").With("exchange", "bingx")

	if logCfg.HTTP && httpClient.Transport != nil {
		rt := httpClient.Transport
		rt = transportlog.NewTransportLog(rt,
			transportlog.LogOptionLogger(logger),
			transportlog.LogOptionMatcherConfig(transportlog.MatcherConfig{
				OnStatus:       []int{0},
				WhiteListPaths: []string{"*"},
				BlackListPaths: []string{
					"GET|/openApi/swap/v2/server/time",
					"GET|/openApi/swap/v2/quote/ticker",
					"GET|/openApi/swap/v2/quote/contracts",
					"GET|/openApi/swap/v2/quote/premiumIndex",
				},
			}),
			transportlog.LogOptionRedactSensitive(true),
			transportlog.LogOptionRedactSensitiveKeys([]string{headerKey}),
		)
		httpClient.Transport = rt
	}

	if baseURL == "" {
		baseURL = defaultRestURL
	}

	return &Client{
		httpClient: httpClient,
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		apiSecret:  apiSecret,
		logCfg:     logCfg,
		logger:     logger,
	}
}

// WarmUp pre-establishes connection pool and maintains it via periodic public calls.
func (c *Client) WarmUp(ctx context.Context, interval time.Duration) {
	c.logger.InfoContext(ctx, "🔗 Warming up BingX connection pool...", slog.Duration("interval", interval))

	ticker.RunImmediate(ctx, interval, func() bool {
		_, err := c.GetCtx(ctx, pathServerTime, nil)
		if err != nil {
			c.logger.DebugContext(ctx, "Warmup server time call failed", slog.Any("error", err))
		}
		return true
	})
}

// Get makes a signed GET request.
func (c *Client) Get(ctx context.Context, path string, params map[string]string) ([]byte, error) {
	return c.GetCtx(ctx, path, params)
}

// GetCtx makes a signed GET request with context.
func (c *Client) GetCtx(ctx context.Context, path string, params map[string]string) ([]byte, error) {
	if params == nil {
		params = make(map[string]string)
	}

	isPrivate := !strings.Contains(path, "/server/") && !strings.Contains(path, "/quote/")
	if isPrivate && c.apiKey != "" {
		params["timestamp"] = strconv.FormatInt(time.Now().UnixMilli(), 10)
		sig := SignParams(c.apiSecret, params)
		params["signature"] = sig
	}

	urlPath := path
	if len(params) > 0 {
		parts := make([]string, 0, len(params))
		for k, v := range params {
			parts = append(parts, fmt.Sprintf("%s=%s", k, v))
		}
		urlPath += "?" + strings.Join(parts, "&")
	}

	url := c.baseURL + urlPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create GET request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set(headerKey, c.apiKey)
	}

	return c.doRequest(ctx, req)
}

// Post makes a signed POST request.
func (c *Client) Post(ctx context.Context, path string, body interface{}) ([]byte, error) {
	return c.PostCtx(ctx, path, body)
}

// PostCtx makes a signed POST request with context.
func (c *Client) PostCtx(ctx context.Context, path string, body interface{}) ([]byte, error) {
	params := make(map[string]string)
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err == nil {
			var m map[string]interface{}
			if err := json.Unmarshal(bodyBytes, &m); err == nil {
				for k, v := range m {
					params[k] = fmt.Sprintf("%v", v)
				}
			}
		}
	}

	if c.apiKey != "" {
		params["timestamp"] = strconv.FormatInt(time.Now().UnixMilli(), 10)
		sig := SignParams(c.apiSecret, params)
		params["signature"] = sig
	}

	urlPath := path
	if len(params) > 0 {
		parts := make([]string, 0, len(params))
		for k, v := range params {
			parts = append(parts, fmt.Sprintf("%s=%s", k, v))
		}
		urlPath += "?" + strings.Join(parts, "&")
	}

	url := c.baseURL + urlPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create POST request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set(headerKey, c.apiKey)
	}

	return c.doRequest(ctx, req)
}

func (c *Client) doRequest(ctx context.Context, req *http.Request) ([]byte, error) {
	trace := &httptrace.ClientTrace{
		GotConn: func(connInfo httptrace.GotConnInfo) {
			if !connInfo.Reused {
				c.logger.DebugContext(ctx, "HTTP new connection",
					"was_idle", connInfo.WasIdle,
					"idle_time", connInfo.IdleTime,
				)
			}
		},
	}
	req = req.WithContext(httptrace.WithClientTrace(ctx, trace))

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
		if isRateLimited(resp.StatusCode) {
			return nil, &exchange.RateLimitError{
				Message: string(body),
				Path:    path,
			}
		}
		return nil, toHTTPError(resp.StatusCode, body, path)
	}

	return body, nil
}

// Latency measures round-trip time of a public API request in milliseconds.
func (c *Client) Latency(ctx context.Context) (int64, error) {
	start := time.Now()
	_, err := c.GetCtx(ctx, pathServerTime, nil)
	if err != nil {
		return 0, err
	}
	return time.Since(start).Milliseconds(), nil
}
