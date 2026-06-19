package okx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/httpclient"
	"crypto-bot/pkg/ticker"

	transportlog "github.com/dangnmh/transport"
)

// Client is the OKX V5 Futures REST API client.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	apiSecret  string
	passphrase string
	logCfg     config.LoggingConfig
	logger     *slog.Logger
	clock      exchange.Clock
}

// NewClient creates a new OKX API client using the provided HTTP client.
func NewClient(httpClient *http.Client, baseURL, apiKey, apiSecret, passphrase string, logCfg config.LoggingConfig) *Client {
	logger := slog.Default().With("component", "exchange").With("exchange", "okx")

	if passphrase == "" {
		passphrase = os.Getenv("OKX_PASSPHRASE")
	}

	var clientCopy http.Client
	if httpClient != nil {
		clientCopy = *httpClient
	}

	if httpClient != nil && clientCopy.Transport != nil {
		if logCfg.HTTP {
			rt := clientCopy.Transport
			rt = transportlog.NewTransportLog(rt,
				transportlog.LogOptionLogger(logger),
				transportlog.LogOptionMatcherConfig(transportlog.MatcherConfig{
					OnStatus:       []int{0},
					WhiteListPaths: []string{"*"},
					BlackListPaths: []string{
						"GET|/api/v5/public/time",
						"GET|/api/v5/public/funding-rate",
						"GET|/api/v5/market/tickers",
						"GET|/api/v5/public/instruments",
					},
				}),
				transportlog.LogOptionRedactSensitive(true),
				transportlog.LogOptionRedactSensitiveKeys([]string{"OK-ACCESS-KEY", "OK-ACCESS-PASSPHRASE"}),
				transportlog.LogOptionQueryParams(true),
			)
			clientCopy.Transport = rt
		}
		clientCopy.Transport = httpclient.WrapWithRequestID(clientCopy.Transport)
	}

	var finalClient *http.Client
	if httpClient != nil {
		finalClient = &clientCopy
	} else {
		finalClient = &http.Client{}
	}

	return &Client{
		httpClient: finalClient,
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		apiSecret:  apiSecret,
		passphrase: passphrase,
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
	c.logger.InfoContext(ctx, "🔗 Warming up OKX connection pool...", slog.Duration("interval", interval))

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
	return c.RawRequest(ctx, http.MethodGet, path, params, nil)
}

// Post makes a signed POST request.
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

// RawRequest makes a signed HTTP request of any method to the OKX API.
func (c *Client) RawRequest(ctx context.Context, method, path string, query map[string]string, body []byte) ([]byte, error) {
	reqURL, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("parse path: %w", err)
	}

	q := reqURL.Query()
	for k, v := range query {
		q.Set(k, v)
	}

	if len(q) > 0 {
		reqURL.RawQuery = buildOKXQueryString(q)
	}
	urlPath := reqURL.String()

	fullURL := c.baseURL + urlPath

	var bodyReader io.Reader
	var bodyStr string
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
		bodyStr = string(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	isPrivate := !strings.Contains(path, "/public/") && !strings.Contains(path, "/market/")
	if isPrivate && c.apiKey != "" {
		ts := c.clock.Now().UTC().Format("2006-01-02T15:04:05.000Z")
		sig := SignRequest(c.apiSecret, ts, method, urlPath, bodyStr)
		req.Header.Set("OK-ACCESS-KEY", c.apiKey)
		req.Header.Set("OK-ACCESS-SIGN", sig)
		req.Header.Set("OK-ACCESS-TIMESTAMP", ts)
		req.Header.Set("OK-ACCESS-PASSPHRASE", c.passphrase)
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

// SupportLeverageOnOrder returns false since OKX doesn't support setting leverage directly on orders.
func (c *Client) SupportLeverageOnOrder() bool {
	return false
}

func (c *Client) GetAssetsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/api/v5/account/balance", params, nil)
}

func (c *Client) GetFundingRateRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/api/v5/public/funding-rate", params, nil)
}

func (c *Client) GetTickersRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	p := make(map[string]string)
	maps.Copy(p, params)
	if p["instType"] == "" {
		p["instType"] = instTypeSwap
	}
	return c.RawRequest(ctx, http.MethodGet, "/api/v5/market/tickers", p, nil)
}

func (c *Client) GetOpenPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	p := make(map[string]string)
	maps.Copy(p, params)
	if p["instType"] == "" {
		p["instType"] = instTypeSwap
	}
	return c.RawRequest(ctx, http.MethodGet, "/api/v5/account/positions", p, nil)
}

func (c *Client) GetHistoryPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	p := make(map[string]string)
	maps.Copy(p, params)
	if p["instType"] == "" {
		p["instType"] = instTypeSwap
	}
	return c.RawRequest(ctx, http.MethodGet, "/api/v5/account/positions-history", p, nil)
}

func (c *Client) GetOrderDetailRaw(ctx context.Context, orderID string, params map[string]string) ([]byte, error) {
	p := make(map[string]string)
	maps.Copy(p, params)
	p["ordId"] = orderID
	return c.RawRequest(ctx, http.MethodGet, "/api/v5/trade/order", p, nil)
}

func (c *Client) GetHistoryOrdersRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	p := make(map[string]string)
	maps.Copy(p, params)
	if p["instType"] == "" {
		p["instType"] = instTypeSwap
	}
	return c.RawRequest(ctx, http.MethodGet, "/api/v5/trade/orders-history", p, nil)
}

func (c *Client) GetOrderDealsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	p := make(map[string]string)
	maps.Copy(p, params)
	if p["instType"] == "" {
		p["instType"] = instTypeSwap
	}
	return c.RawRequest(ctx, http.MethodGet, "/api/v5/trade/fills", p, nil)
}

func (c *Client) GetClosedPnLRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	p := make(map[string]string)
	maps.Copy(p, params)
	if p["instType"] == "" {
		p["instType"] = instTypeSwap
	}
	return c.RawRequest(ctx, http.MethodGet, "/api/v5/account/positions-history", p, nil)
}

func (c *Client) GetOrderPNLRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	symbol := params["symbol"]
	orderID := params["order_id"]
	if orderID == "" {
		orderID = params["orderId"]
	}
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required")
	}
	if orderID == "" {
		return nil, fmt.Errorf("order_id is required")
	}
	info, err := c.GetOrderPNL(ctx, symbol, orderID)
	if err != nil {
		return nil, err
	}
	return json.Marshal(info)
}

func buildOKXQueryString(q url.Values) string {
	if len(q) == 0 {
		return ""
	}
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(q))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, q.Get(k)))
	}
	return strings.Join(parts, "&")
}
