package exchange

import (
	"bytes"
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
}

// NewClient creates a new MEXC API client with optimized connection pooling.
func NewClient(baseURL, apiKey, apiSecret string, logCfg config.LoggingConfig) *Client {
	transport := &http.Transport{
		MaxIdleConns:        10,
		MaxIdleConnsPerHost: 6,
		IdleConnTimeout:     60 * time.Second,
		TLSHandshakeTimeout: 5 * time.Second,
		DisableCompression:  false,
	}

	logger := slog.Default().With("component", "exchange")

	var rt http.RoundTripper = transport
	if logCfg.HTTP {
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
		)
	}

	return &Client{
		httpClient: &http.Client{
			Transport: rt,
			Timeout:   10 * time.Second,
		},
		baseURL:   strings.TrimRight(baseURL, "/"),
		apiKey:    apiKey,
		apiSecret: apiSecret,
		logCfg:    logCfg,
		logger:    logger,
	}
}

// WarmUp pre-establishes connection pool and maintains it via periodic ping requests.
func (c *Client) WarmUp(ctx context.Context, interval time.Duration) {
	c.logger.Info("🔗 Warming up connection pool...", "interval", interval)

	ticker.RunImmediate(ctx, interval, func() bool {
		_, err := c.GetCtx(ctx, "/api/v1/contract/ping", nil)
		if err != nil {
			c.logger.Debug("Warmup ping failed", "error", err)
		}
		return true
	})
}

// Get makes a signed GET request to a private or public endpoint.
func (c *Client) Get(ctx context.Context, path string, params map[string]string) ([]byte, error) {
	return c.GetCtx(ctx, path, params)
}

// GetCtx makes a signed GET request with context.
func (c *Client) GetCtx(ctx context.Context, path string, params map[string]string) ([]byte, error) {
	url := c.baseURL + path
	isPrivate := strings.Contains(path, "/private/")

	// Build query string
	if len(params) > 0 {
		qs := buildSortedQueryString(params)
		url += "?" + qs
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create GET request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	if isPrivate {
		ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
		sig := SignRequest(c.apiKey, c.apiSecret, ts, "GET", params)
		req.Header.Set("ApiKey", c.apiKey)
		req.Header.Set("Request-Time", ts)
		req.Header.Set("Signature", sig)
	}

	return c.doRequest(req)
}

// Post makes a signed POST request to a private endpoint.
func (c *Client) Post(ctx context.Context, path string, body interface{}) ([]byte, error) {
	return c.PostCtx(ctx, path, body)
}

// PostCtx makes a signed POST request with context.
func (c *Client) PostCtx(ctx context.Context, path string, body interface{}) ([]byte, error) {
	url := c.baseURL + path

	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal POST body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create POST request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Sign the request
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	sig := SignRequest(c.apiKey, c.apiSecret, ts, "POST", body)
	req.Header.Set("ApiKey", c.apiKey)
	req.Header.Set("Request-Time", ts)
	req.Header.Set("Signature", sig)

	return c.doRequest(req)
}

// doRequest executes the HTTP request and returns the response body.
func (c *Client) doRequest(req *http.Request) ([]byte, error) {
	trace := &httptrace.ClientTrace{
		GotConn: func(connInfo httptrace.GotConnInfo) {
			if !connInfo.Reused {
				c.logger.Debug("HTTP new connection",
					"was_idle", connInfo.WasIdle,
					"idle_time", connInfo.IdleTime,
				)
			}
		},
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))

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
		// Log a warning if the response was non-200 and transport logger didn't log due to config
		if !c.logCfg.HTTP {
			c.logger.Warn("🟡 Non-200 response",
				"status", resp.StatusCode,
				"body", string(body),
			)
		}
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
