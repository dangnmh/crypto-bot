package weex

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
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

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/httpclient"
	"crypto-bot/pkg/xjson"

	transportlog "github.com/dangnmh/transport"
)

// Client is the WEEX REST API client.
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

// NewClient creates a new WEEX client.
func NewClient(httpClient *http.Client, baseURL, apiKey, apiSecret, passphrase string, logCfg config.LoggingConfig) *Client {
	logger := slog.Default().With("component", "exchange").With("exchange", "weex")
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
					"GET|/capi/v3/market/time",
					"GET|/capi/v3/market/ticker/24hr",
					"GET|/capi/v3/market/premiumIndex",
					"GET|/capi/v3/market/exchangeInfo",
				},
			}),
			transportlog.LogOptionRedactSensitive(true),
			transportlog.LogOptionRedactSensitiveKeys([]string{"ACCESS-KEY", "ACCESS-PASSPHRASE", "ACCESS-SIGN"}),
			transportlog.LogOptionQueryParams(true),
		)
		clientCopy.Transport = rt
	}
	clientCopy.Transport = httpclient.WrapWithRequestID(clientCopy.Transport)

	return &Client{
		httpClient: &clientCopy,
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		apiSecret:  apiSecret,
		passphrase: passphrase,
		logCfg:     logCfg,
		logger:     logger,
		clock:      exchange.RealClock{},
	}
}

// SetClock configures a custom clock for testing.
func (c *Client) SetClock(clk exchange.Clock) {
	if clk != nil {
		c.clock = clk
	}
}

// request executes a signed or unsigned request to the WEEX API.
func (c *Client) request(ctx context.Context, method, path string, query map[string]string, body any, signed bool) ([]byte, error) {
	reqURL, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}

	var queryString string
	if len(query) > 0 {
		queryString = buildQuery(query)
		reqURL.RawQuery = queryString
	}

	var bodyBytes []byte
	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if signed {
		c.signRequest(req, method, path, queryString, bodyBytes)
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

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusTooManyRequests {
			return nil, &exchange.RateLimitError{Message: string(respBody), Path: path}
		}
		return nil, toHTTPError(resp.StatusCode, respBody, path)
	}

	// Weex API returns format: {"code":"00000","msg":"success","data":...}
	// Let's verify response code.
	var baseResp struct {
		Code string          `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(respBody, &baseResp); err == nil && baseResp.Code != "" {
		if !isWeexSuccess(baseResp.Code) {
			return nil, toAPIError(baseResp.Code, baseResp.Msg, path)
		}
	}

	return respBody, nil
}

// RawRequest executes a signed or unsigned request to the WEEX API returning raw bytes.
func (c *Client) RawRequest(ctx context.Context, method, path string, query map[string]string, body []byte) ([]byte, error) {
	var bodyVal any
	if len(body) > 0 {
		var temp map[string]any
		if err := json.Unmarshal(body, &temp); err == nil {
			bodyVal = temp
		} else {
			bodyVal = body
		}
	}
	signed := c.apiKey != "" && !strings.Contains(path, "/market/")
	return c.request(ctx, method, path, query, bodyVal, signed)
}

// GetFundingRateRaw fetches raw funding rates.
func (c *Client) GetFundingRateRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/capi/v3/market/premiumIndex", params, nil)
}

// GetTickersRaw fetches raw tickers.
func (c *Client) GetTickersRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/capi/v3/market/ticker/24hr", params, nil)
}

// GetOpenPositionsRaw fetches raw positions.
func (c *Client) GetOpenPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/capi/v3/account/position/allPosition", params, nil)
}

// GetHistoryPositionsRaw fetches raw history positions.
func (c *Client) GetHistoryPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/capi/v3/account/position/allPosition", params, nil)
}

// GetOrderDetailRaw fetches raw order detail.
func (c *Client) GetOrderDetailRaw(ctx context.Context, orderID string, params map[string]string) ([]byte, error) {
	p := make(map[string]string)
	maps.Copy(p, params)
	p["orderId"] = orderID
	return c.RawRequest(ctx, http.MethodGet, "/capi/v3/order", p, nil)
}

// GetHistoryOrdersRaw fetches raw history orders.
func (c *Client) GetHistoryOrdersRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/capi/v3/order/history", params, nil)
}

// GetOrderPNLRaw fetches raw order PNL (income).
func (c *Client) GetOrderPNLRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodPost, "/capi/v3/account/income", params, nil)
}

func buildQuery(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var pairs []string
	for _, k := range keys {
		pairs = append(pairs, url.QueryEscape(k)+"="+url.QueryEscape(params[k]))
	}
	return strings.Join(pairs, "&")
}

func parseResponse[T any](body []byte) (T, error) {
	var res T
	// Try parsing as the standard wrapped response structure first.
	var wrapped struct {
		Code string          `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapped); err == nil && wrapped.Code != "" {
		if !isWeexSuccess(wrapped.Code) {
			return res, toAPIError(wrapped.Code, wrapped.Msg, "")
		}
		if len(wrapped.Data) == 0 || string(wrapped.Data) == "null" {
			return res, nil
		}
		if err := json.Unmarshal(wrapped.Data, &res); err != nil {
			return res, fmt.Errorf("unmarshal data: %w", err)
		}
		return res, nil
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return res, fmt.Errorf("unmarshal direct response: %w", err)
	}
	return res, nil
}

func (c *Client) signRequest(req *http.Request, method, path, queryString string, bodyBytes []byte) {
	timestamp := strconv.FormatInt(c.clock.Now().UnixMilli(), 10)
	bodyStr := ""
	if len(bodyBytes) > 0 {
		bodyStr = string(bodyBytes)
	}
	message := timestamp + strings.ToUpper(method) + path
	if queryString != "" {
		message += "?" + queryString
	}
	message += bodyStr

	mac := hmac.New(sha256.New, []byte(c.apiSecret))
	mac.Write([]byte(message))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	req.Header.Set("ACCESS-KEY", c.apiKey)
	req.Header.Set("ACCESS-SIGN", signature)
	req.Header.Set("ACCESS-TIMESTAMP", timestamp)
	req.Header.Set("ACCESS-PASSPHRASE", c.passphrase)
}

func isWeexSuccess(code string) bool {
	return code == "00000" || code == "200" || code == "0"
}

func toAPIError(codeStr, message, path string) *exchange.APIError {
	codeVal, _ := strconv.Atoi(codeStr)
	return &exchange.APIError{
		Code:    codeVal,
		Message: message,
		Path:    path,
	}
}

func toHTTPError(statusCode int, body []byte, path string) *exchange.APIError {
	apiErr := &exchange.APIError{
		StatusCode: statusCode,
		Message:    string(body),
		Path:       path,
	}
	var baseResp struct {
		Code xjson.Number `json:"code"`
		Msg  string       `json:"msg"`
	}
	if err := json.Unmarshal(body, &baseResp); err == nil {
		apiErr.Code, _ = baseResp.Code.Int()
		if baseResp.Msg != "" {
			apiErr.Message = baseResp.Msg
		}
	}
	return apiErr
}
