package toobit

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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

	transportlog "github.com/dangnmh/transport"
	"golang.org/x/time/rate"

	"crypto-bot/pkg/xjson"
)

// Client is the Toobit REST API client.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	apiSecret  string
	logCfg     config.LoggingConfig
	logger     *slog.Logger
	clock      exchange.Clock
	limiter    *ratelimit.ExchangeRateLimiter
}

// NewClient creates a new Toobit client.
func NewClient(httpClient *http.Client, baseURL, apiKey, apiSecret string, logCfg config.LoggingConfig) *Client {
	logger := slog.Default().With("component", "exchange").With("exchange", "toobit")
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
					"GET|/api/v1/time",
					"GET|/quote/v1/contract/ticker/24hr",
					"GET|/api/v1/futures/fundingRate",
					"GET|/api/v1/exchangeInfo",
					"GET|/api/v1/futures/riskLimits",
					"POST|/api/v1/listenKey",
					"PUT|/api/v1/listenKey",
				},
			}),
			transportlog.LogOptionRedactSensitive(true),
			transportlog.LogOptionRedactSensitiveKeys([]string{"X-BB-APIKEY"}),
			transportlog.LogOptionQueryParams(true),
		)
		clientCopy.Transport = rt
	}
	clientCopy.Transport = httpclient.WrapWithRequestID(clientCopy.Transport)

	limiter := ratelimit.NewExchangeRateLimiter(rate.Limit(10), 2, nil)

	return &Client{
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

// SetClock configures a custom clock for testing.
func (c *Client) SetClock(clk exchange.Clock) {
	if clk != nil {
		c.clock = clk
	}
}

// request executes a signed or unsigned request to the Toobit API.
func (c *Client) request(ctx context.Context, method, path string, params map[string]string, signed bool) ([]byte, error) {
	if c.limiter != nil {
		if err := c.limiter.Acquire(ctx, path); err != nil {
			return nil, fmt.Errorf("rate limit acquire: %w", err)
		}
	}

	reqURL, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}

	var body io.Reader
	if signed {
		var query string
		body, query, err = c.signParams(method, params)
		if err != nil {
			return nil, err
		}
		if method == http.MethodGet {
			reqURL.RawQuery = query
		}
	} else if len(params) > 0 {
		val := url.Values{}
		for k, v := range params {
			val.Set(k, v)
		}
		reqURL.RawQuery = val.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	c.setHeaders(req, method, signed)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP Do: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusTooManyRequests {
			return nil, &exchange.RateLimitError{Message: string(respBody), Path: path}
		}
		return nil, fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(respBody))
	}

	if err := checkError(respBody); err != nil {
		return nil, err
	}

	return respBody, nil
}

func (c *Client) signParams(method string, params map[string]string) (io.Reader, string, error) {
	val := url.Values{}
	for k, v := range params {
		val.Set(k, v)
	}
	val.Set("recvWindow", "5000")
	val.Set("timestamp", strconv.FormatInt(c.clock.Now().UnixMilli(), 10))
	queryString := val.Encode()

	mac := hmac.New(sha256.New, []byte(c.apiSecret))
	mac.Write([]byte(queryString))
	signature := hex.EncodeToString(mac.Sum(nil))
	queryString += "&signature=" + signature

	var body io.Reader
	var query string
	if method == http.MethodGet {
		query = queryString
	} else {
		body = strings.NewReader(queryString)
	}
	return body, query, nil
}

func (c *Client) setHeaders(req *http.Request, method string, signed bool) {
	if signed && (method == http.MethodPost || method == http.MethodDelete || method == http.MethodPut) {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("X-BB-APIKEY", c.apiKey)
		req.Header.Set("X-BB-API-PLATFORM", "177321641268789")
	}
}

type toobitErrorResponse struct {
	Code    any    `json:"code"`
	Message string `json:"msg"`
}

func checkError(body []byte) error {
	var envelope toobitErrorResponse
	_ = xjson.Unmarshal(body, &envelope)
	if envelope.Code == nil {
		return nil
	}
	var codeStr string
	switch v := envelope.Code.(type) {
	case string:
		codeStr = v
	case float64:
		codeStr = strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		codeStr = strconv.Itoa(v)
	case int64:
		codeStr = strconv.FormatInt(v, 10)
	default:
		codeStr = fmt.Sprintf("%v", v)
	}

	if codeStr != "200" && codeStr != "0" && codeStr != successCode && codeStr != "" {
		return fmt.Errorf("API error code %s: %s", codeStr, envelope.Message)
	}
	return nil
}

// RawRequest makes a signed or unsigned HTTP request to the Toobit API.
func (c *Client) RawRequest(ctx context.Context, method, path string, query map[string]string, body []byte) ([]byte, error) {
	params := make(map[string]string)
	maps.Copy(params, query)

	if len(body) > 0 {
		if strings.HasPrefix(string(body), "{") {
			var bodyMap map[string]any
			if err := xjson.Unmarshal(body, &bodyMap); err == nil {
				for k, v := range bodyMap {
					params[k] = fmt.Sprintf("%v", v)
				}
			}
		} else {
			if values, err := url.ParseQuery(string(body)); err == nil {
				for k, vs := range values {
					if len(vs) > 0 {
						params[k] = vs[0]
					}
				}
			}
		}
	}

	signed := c.apiKey != "" &&
		!strings.Contains(path, "/time") &&
		!strings.Contains(path, "/exchangeInfo") &&
		!strings.Contains(path, "/quote/")

	return c.request(ctx, method, path, params, signed)
}

// GetFundingRateRaw fetches raw funding rates.
func (c *Client) GetFundingRateRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/api/v1/futures/fundingRate", params, nil)
}

// GetTickersRaw fetches raw tickers.
func (c *Client) GetTickersRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/quote/v1/contract/ticker/24hr", params, nil)
}

// GetOpenPositionsRaw fetches raw positions.
func (c *Client) GetOpenPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/api/v1/futures/positions", params, nil)
}

// GetHistoryPositionsRaw fetches raw history positions.
func (c *Client) GetHistoryPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/api/v1/futures/historyPositions", params, nil)
}

// GetOrderDetailRaw fetches raw order detail.
func (c *Client) GetOrderDetailRaw(ctx context.Context, orderID string, params map[string]string) ([]byte, error) {
	p := make(map[string]string)
	maps.Copy(p, params)
	p["orderId"] = orderID
	return c.RawRequest(ctx, http.MethodGet, "/api/v1/futures/order", p, nil)
}

// GetHistoryOrdersRaw fetches raw history orders.
func (c *Client) GetHistoryOrdersRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/api/v2/futures/open-orders", params, nil)
}

// GetFuturesBalanceFlowRaw fetches raw futures balance flow.
func (c *Client) GetFuturesBalanceFlowRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/api/v1/futures/balanceFlow", params, nil)
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
