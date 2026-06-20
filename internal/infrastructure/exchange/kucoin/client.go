package kucoin

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

// Client is the KuCoin Futures REST API client.
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

// NewClient creates a new KuCoin API client using the provided HTTP client.
func NewClient(httpClient *http.Client, baseURL, apiKey, apiSecret, passphrase string, logCfg config.LoggingConfig) *Client {
	logger := slog.Default().With("component", "exchange").With("exchange", "kucoin")
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
						"GET|/api/v1/timestamp",
						"GET|/api/v1/allTickers",
						"GET|/api/v1/contracts/active",
						"POST|/api/v1/bullet-public",
						"POST|/api/v1/bullet-private",
					},
				}),
				transportlog.LogOptionRedactSensitive(true),
				transportlog.LogOptionRedactSensitiveKeys([]string{headerKey, headerAuthPhrase}),
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

	if baseURL == "" {
		baseURL = defaultRestURL
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
	c.logger.InfoContext(ctx, "🔗 Warming up KuCoin connection pool...", slog.Duration("interval", interval))

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

// RawRequest makes a signed HTTP request of any method to the Kucoin API.
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
		reqURL.RawQuery = q.Encode()
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

	isPrivate := isKucoinPrivatePath(path)

	if isPrivate && c.apiKey != "" {
		ts := strconv.FormatInt(c.clock.Now().UnixMilli(), 10)
		sig := SignRequest(c.apiSecret, ts, method, urlPath, bodyStr)
		req.Header.Set(headerKey, c.apiKey)
		req.Header.Set(headerSign, sig)
		req.Header.Set(headerTimestamp, ts)
		req.Header.Set(headerAuthPhrase, SignPassphrase(c.apiSecret, c.passphrase))
		req.Header.Set(headerVersion, "2")
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

// SupportLeverageOnOrder returns false since KuCoin doesn't support setting leverage directly on orders.
func (c *Client) SupportLeverageOnOrder() bool {
	return true
}

func (c *Client) GetFundingRateRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	symbol := params["symbol"]
	path := fmt.Sprintf("/api/v1/funding-rate/%s/current", symbol)
	return c.RawRequest(ctx, http.MethodGet, path, nil, nil)
}

func (c *Client) GetTickersRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	if params["symbol"] != "" {
		return c.RawRequest(ctx, http.MethodGet, "/api/v1/ticker", params, nil)
	}
	return c.RawRequest(ctx, http.MethodGet, "/api/v1/allTickers", params, nil)
}

func (c *Client) GetOpenPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/api/v1/positions", params, nil)
}

func (c *Client) GetHistoryPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/api/v1/history-positions", params, nil)
}

func (c *Client) GetOrderDetailRaw(ctx context.Context, orderID string, params map[string]string) ([]byte, error) {
	path := fmt.Sprintf("/api/v1/orders/%s", orderID)
	return c.RawRequest(ctx, http.MethodGet, path, params, nil)
}

func (c *Client) GetHistoryOrdersRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/api/v1/history-orders", params, nil)
}

func (c *Client) GetOrderDealsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/api/v1/fills", params, nil)
}

func (c *Client) GetClosedPnLRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/api/v1/history-positions", params, nil)
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

func isKucoinPrivatePath(path string) bool {
	return !strings.Contains(path, "/timestamp") &&
		!strings.Contains(path, "/allTickers") &&
		!strings.Contains(path, "/ticker") &&
		!strings.Contains(path, "/contracts/active") &&
		!strings.Contains(path, "/kline/query") &&
		!strings.Contains(path, "/level2/snapshot") &&
		!strings.Contains(path, "/bullet-public") &&
		!strings.Contains(path, "/funding-rate/") &&
		!strings.Contains(path, "/ua/v1/")
}
