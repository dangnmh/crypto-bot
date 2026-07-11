package bitmart

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"maps"
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

// Client is the Bitmart REST API client.
type Client struct {
	httpClient    *http.Client
	baseURL       string
	apiKey        string
	apiSecret     string
	apiPassphrase string
	logCfg        config.LoggingConfig
	logger        *slog.Logger
	clock         exchange.Clock
	limiter       *ratelimit.ExchangeRateLimiter
}

// NewClient creates a new Bitmart client.
func NewClient(httpClient *http.Client, baseURL, apiKey, apiSecret, apiPassphrase string, logCfg config.LoggingConfig) *Client {
	logger := slog.Default().With("component", "exchange").With("exchange", "bitmart")
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
					"GET|/system/time",
					"GET|/contract/public/details",
				},
			}),
			transportlog.LogOptionRedactSensitive(true),
			transportlog.LogOptionRedactSensitiveKeys([]string{"X-BM-KEY"}),
			transportlog.LogOptionQueryParams(true),
		)
		clientCopy.Transport = rt
	}
	clientCopy.Transport = httpclient.WrapWithRequestID(clientCopy.Transport)

	limiter := ratelimit.NewExchangeRateLimiter(rate.Limit(10), 2, nil)

	return &Client{
		httpClient:    &clientCopy,
		baseURL:       strings.TrimRight(baseURL, "/"),
		apiKey:        apiKey,
		apiSecret:     apiSecret,
		apiPassphrase: apiPassphrase,
		logCfg:        logCfg,
		logger:        logger,
		clock:         exchange.RealClock{},
		limiter:       limiter,
	}
}

// SetClock configures a custom clock for testing.
func (c *Client) SetClock(clk exchange.Clock) {
	if clk != nil {
		c.clock = clk
	}
}

// request executes an unsigned GET request to the Bitmart API (for backwards compatibility with market.go).
func (c *Client) request(ctx context.Context, method, path string, query map[string]string) ([]byte, error) {
	return c.requestFull(ctx, method, path, query, nil, false)
}

//nolint:cyclop // Request wrappers are naturally complex
func (c *Client) requestFull(ctx context.Context, method, path string, query map[string]string, body []byte, signed bool) ([]byte, error) {
	if c.limiter != nil {
		if err := c.limiter.Acquire(ctx, path); err != nil {
			return nil, fmt.Errorf("rate limit acquire: %w", err)
		}
	}

	reqURL, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}

	var rawQuery string
	if len(query) > 0 {
		q := reqURL.Query()
		for k, v := range query {
			q.Set(k, v)
		}
		rawQuery = q.Encode()
		reqURL.RawQuery = rawQuery
	}

	var reqBody io.Reader
	if len(body) > 0 {
		reqBody = bytes.NewReader(body)
	} else {
		reqBody = http.NoBody
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), reqBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	if signed {
		timestamp := strconv.FormatInt(c.clock.Now().UnixMilli(), 10)
		var payload string
		if method == http.MethodPost || method == http.MethodPut || method == http.MethodDelete {
			payload = string(body)
		} else {
			payload = rawQuery
		}

		signature := GenerateSignature(timestamp, c.apiPassphrase, c.apiSecret, payload)

		req.Header.Set("X-BM-KEY", c.apiKey)
		req.Header.Set("X-BM-TIMESTAMP", timestamp)
		req.Header.Set("X-BM-SIGN", signature)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP Do: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, &exchange.RateLimitError{Message: string(respBody), Path: path}
	}

	if err := c.handleAPIError(resp.StatusCode, path, respBody); err != nil {
		return nil, err
	}

	return respBody, nil
}

type bitmartErrorResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (c *Client) handleAPIError(statusCode int, path string, body []byte) error {
	var errResp bitmartErrorResponse
	if xjson.Unmarshal(body, &errResp) == nil {
		if errResp.Code != 0 && errResp.Code != 1000 && errResp.Code != 200 {
			return &exchange.APIError{
				StatusCode: statusCode,
				Code:       errResp.Code,
				Message:    errResp.Message,
				Path:       path,
			}
		}
	}
	if statusCode != http.StatusOK {
		return &exchange.APIError{
			StatusCode: statusCode,
			Message:    string(body),
			Path:       path,
		}
	}
	return nil
}

// RawRequest makes a signed or unsigned HTTP request to the Bitmart API.
func (c *Client) RawRequest(ctx context.Context, method, path string, query map[string]string, body []byte) ([]byte, error) {
	signed := c.apiKey != "" &&
		!strings.Contains(path, "/public/") &&
		!strings.Contains(path, "/system/")

	return c.requestFull(ctx, method, path, query, body, signed)
}

// GetFundingRateRaw fetches raw funding rates.
func (c *Client) GetFundingRateRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/contract/public/details", params, nil)
}

// GetTickersRaw fetches raw tickers.
func (c *Client) GetTickersRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/contract/public/details", params, nil)
}

// GetOpenPositionsRaw fetches raw positions.
func (c *Client) GetOpenPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/contract/private/position-v2", params, nil)
}

// GetHistoryPositionsRaw fetches raw history positions.
func (c *Client) GetHistoryPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return nil, fmt.Errorf("not implemented for bitmart")
}

// GetOrderDetailRaw fetches raw order detail.
func (c *Client) GetOrderDetailRaw(ctx context.Context, orderID string, params map[string]string) ([]byte, error) {
	p := make(map[string]string)
	maps.Copy(p, params)
	p["order_id"] = orderID
	return c.RawRequest(ctx, http.MethodGet, "/contract/private/order", p, nil)
}

// GetHistoryOrdersRaw fetches raw history orders.
func (c *Client) GetHistoryOrdersRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/contract/private/order-history", params, nil)
}

// GetOrderPNLRaw fetches raw order PNL.
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
	return xjson.Marshal(info)
}
