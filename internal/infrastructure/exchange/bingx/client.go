package bingx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"sort"
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
	clock      exchange.Clock
}

// NewClient creates a new BingX API client using the provided HTTP client.
func NewClient(httpClient *http.Client, baseURL, apiKey, apiSecret string, logCfg config.LoggingConfig) *Client {
	logger := slog.Default().With("component", "exchange").With("exchange", "bingx")

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
				WhiteListPaths: []string{"*"},
				BlackListPaths: []string{
					"GET|/openApi/swap/v2/server/time",
					"GET|/openApi/swap/v2/quote/premiumIndex",
					"POST|/openApi/user/auth/userDataStream",
					"GET|/openApi/swap/v2/quote/contracts",
					"GET|/openApi/swap/v2/quote/ticker",
				},
			}),
			transportlog.LogOptionRedactSensitive(true),
			transportlog.LogOptionRedactSensitiveKeys([]string{headerKey}),
			transportlog.LogOptionQueryParams(true),
		)
		clientCopy.Transport = rt
	}

	var finalClient *http.Client
	if httpClient != nil {
		finalClient = &clientCopy
	} else {
		finalClient = &http.Client{}
	}

	if baseURL == "" {
		baseURL = defaultRestURL
	}

	return &Client{
		httpClient: finalClient,
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
	u, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("parse path: %w", err)
	}

	allParams := make(map[string]string)
	for k, vs := range u.Query() {
		if len(vs) > 0 {
			allParams[k] = vs[0]
		}
	}
	maps.Copy(allParams, params)

	isPrivate := !strings.Contains(path, "/server/") && !strings.Contains(path, "/quote/")
	if isPrivate && c.apiKey != "" {
		allParams["timestamp"] = strconv.FormatInt(c.clock.Now().UnixMilli(), 10)
		sig := SignParams(c.apiSecret, allParams)
		allParams["signature"] = sig
	}

	urlPath := buildQueryString(u.Path, allParams)
	fullURL := c.baseURL + urlPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, http.NoBody)
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
func (c *Client) Post(ctx context.Context, path string, body any) ([]byte, error) {
	return c.PostCtx(ctx, path, nil, body)
}

// PostCtx makes a signed POST request with context.
func (c *Client) PostCtx(ctx context.Context, path string, params map[string]string, body any) ([]byte, error) {
	return c.requestCtx(ctx, http.MethodPost, path, params, body)
}

// PutCtx makes a signed PUT request with context.
func (c *Client) PutCtx(ctx context.Context, path string, params map[string]string, body any) ([]byte, error) {
	return c.requestCtx(ctx, http.MethodPut, path, params, body)
}

// DeleteCtx makes a signed DELETE request with context.
func (c *Client) DeleteCtx(ctx context.Context, path string, params map[string]string, body any) ([]byte, error) {
	return c.requestCtx(ctx, http.MethodDelete, path, params, body)
}

func (c *Client) requestCtx(ctx context.Context, method, path string, params map[string]string, body any) ([]byte, error) {
	u, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("parse path: %w", err)
	}

	allParams := make(map[string]string)
	for k, vs := range u.Query() {
		if len(vs) > 0 {
			allParams[k] = vs[0]
		}
	}
	maps.Copy(allParams, params)

	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err == nil {
			var m map[string]any
			if err := json.Unmarshal(bodyBytes, &m); err == nil {
				for k, v := range m {
					allParams[k] = fmt.Sprintf("%v", v)
				}
			}
		}
	}

	if c.apiKey != "" {
		allParams["timestamp"] = strconv.FormatInt(c.clock.Now().UnixMilli(), 10)
		sig := SignParams(c.apiSecret, allParams)
		allParams["signature"] = sig
	}

	qStr := formatQueryParams(allParams)
	fullURL := c.baseURL + u.Path
	var bodyReader io.Reader
	if qStr != "" {
		bodyReader = strings.NewReader(qStr)
	} else {
		bodyReader = http.NoBody
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create %s request: %w", method, err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if c.apiKey != "" {
		req.Header.Set(headerKey, c.apiKey)
	}

	return c.doRequest(ctx, req)
}

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

// SupportLeverageOnOrder returns false since BingX doesn't support setting leverage directly on orders.
func (c *Client) SupportLeverageOnOrder() bool {
	return false
}
func formatQueryParams(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	hasJson := false
	for _, v := range params {
		if strings.Contains(v, "[") || strings.Contains(v, "{") {
			hasJson = true
			break
		}
	}

	var parts []string
	for _, k := range keys {
		val := params[k]
		if hasJson {
			escaped := url.QueryEscape(val)
			escaped = strings.ReplaceAll(escaped, "+", "%20")
			parts = append(parts, k+"="+escaped)
		} else {
			parts = append(parts, k+"="+val)
		}
	}
	return strings.Join(parts, "&")
}

func buildQueryString(path string, params map[string]string) string {
	u, err := url.Parse(path)
	if err != nil {
		qStr := formatQueryParams(params)
		if qStr == "" {
			return path
		}
		return path + "?" + qStr
	}

	allParams := make(map[string]string)
	for k, vs := range u.Query() {
		if len(vs) > 0 {
			allParams[k] = vs[0]
		}
	}
	maps.Copy(allParams, params)

	qStr := formatQueryParams(allParams)
	if qStr == "" {
		return path
	}

	return u.Path + "?" + qStr
}
