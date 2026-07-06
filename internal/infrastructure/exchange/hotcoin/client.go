package hotcoin

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
	"strings"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/httpclient"
	"crypto-bot/pkg/xjson"

	transportlog "github.com/dangnmh/transport"
)

// Client handles REST calls to the Hotcoin perpetual futures API.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	apiSecret  string
	logCfg     config.LoggingConfig
	logger     *slog.Logger
	clock      exchange.Clock
}

// NewClient creates a new Hotcoin client.
func NewClient(httpClient *http.Client, baseURL, apiKey, apiSecret string, logCfg config.LoggingConfig) *Client {
	logger := slog.Default().With("component", "exchange").With("exchange", "hotcoin")
	if baseURL == "" {
		baseURL = "https://api-ct.hotcoin.fit"
	}
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
					"GET|/api/v1/perpetual/public/time",
					"GET|/api/v1/perpetual/public/contracts",
					"GET|/api/v1/perpetual/public",
				},
			}),
			transportlog.LogOptionRedactSensitive(true),
			transportlog.LogOptionRedactSensitiveKeys([]string{"AccessKeyId", "Signature"}),
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

func (c *Client) addSignature(method string, reqURL *url.URL, queryParams map[string]string) {
	queryParams["AccessKeyId"] = c.apiKey
	queryParams["SignatureMethod"] = "HmacSHA256"
	queryParams["SignatureVersion"] = "2"
	queryParams["Timestamp"] = c.clock.Now().UTC().Format("2006-01-02T15:04:05.000Z")

	sig := c.signRequest(method, reqURL.Host, reqURL.Path, queryParams)
	queryParams["Signature"] = sig
}

func (c *Client) parseAPIError(respBody []byte) error {
	var apiErr struct {
		Code xjson.Number `json:"code"`
		Msg  string       `json:"msg"`
	}
	if len(respBody) > 0 && respBody[0] == '{' {
		if err := json.Unmarshal(respBody, &apiErr); err == nil && apiErr.Code != "" {
			codeVal := xjson.ToInt64(apiErr.Code)
			if codeVal != 0 && codeVal != 200 {
				return fmt.Errorf("API error code %d: %s", codeVal, apiErr.Msg)
			}
		}
	}
	return nil
}

// request executes a signed or unsigned request to the Hotcoin API.
func (c *Client) request(ctx context.Context, method, path string, query map[string]string, body any, signed bool) ([]byte, error) {
	reqURL, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}

	queryParams := make(map[string]string)
	if len(query) > 0 {
		maps.Copy(queryParams, query)
	}

	if signed {
		c.addSignature(method, reqURL, queryParams)
	}

	if len(queryParams) > 0 {
		reqURL.RawQuery = buildSortedQuery(queryParams)
	}

	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error status %d: %s", resp.StatusCode, string(respBody))
	}

	if err := c.parseAPIError(respBody); err != nil {
		return nil, err
	}

	return respBody, nil
}

// signRequest generates Hotcoin Signature Version 2.
func (c *Client) signRequest(method, host, path string, query map[string]string) string {
	keys := make([]string, 0, len(query))
	for k := range query {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, customURLEscape(k)+"="+customURLEscape(query[k]))
	}
	queryString := strings.Join(parts, "&")

	// Lowercase the host as per API standard documentation signature normalize steps
	signHost := strings.ToLower(host)
	if signHost == "" {
		// Fallback parse host from baseURL
		if u, err := url.Parse(c.baseURL); err == nil {
			signHost = strings.ToLower(u.Host)
		}
	}

	baseString := fmt.Sprintf("%s\n%s\n%s\n%s", method, signHost, path, queryString)

	h := hmac.New(sha256.New, []byte(c.apiSecret))
	h.Write([]byte(baseString))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func customURLEscape(s string) string {
	escaped := url.QueryEscape(s)
	escaped = strings.ReplaceAll(escaped, "+", "%20")
	var sb strings.Builder
	runes := []rune(escaped)
	for i := 0; i < len(runes); i++ {
		if runes[i] == '%' && i+2 < len(runes) {
			sb.WriteRune('%')
			sb.WriteString(strings.ToUpper(string(runes[i+1 : i+3])))
			i += 2
		} else {
			sb.WriteRune(runes[i])
		}
	}
	return sb.String()
}

func buildSortedQuery(query map[string]string) string {
	keys := make([]string, 0, len(query))
	for k := range query {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, customURLEscape(k)+"="+customURLEscape(query[k]))
	}
	return strings.Join(parts, "&")
}

// RawRequest executes a signed or unsigned request to the Hotcoin API returning raw bytes.
func (c *Client) RawRequest(ctx context.Context, method, path string, query map[string]string, body []byte) ([]byte, error) {
	var bodyVal any
	if len(body) > 0 {
		if err := json.Unmarshal(body, &bodyVal); err != nil {
			return nil, fmt.Errorf("unmarshal raw body: %w", err)
		}
	}
	return c.request(ctx, method, path, query, bodyVal, true)
}

// GetFundingRateRaw returns raw funding rate.
func (c *Client) GetFundingRateRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	symbol := params["symbol"]
	if symbol == "" {
		return nil, fmt.Errorf("missing symbol")
	}
	return c.request(ctx, http.MethodGet, "/api/v1/perpetual/public/"+strings.ToLower(symbol)+"/premiumIndex", nil, nil, false)
}

// GetTickersRaw returns raw tickers list.
func (c *Client) GetTickersRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.request(ctx, http.MethodGet, "/api/v1/perpetual/public/contracts", params, nil, false)
}

// GetOpenPositionsRaw returns raw positions.
func (c *Client) GetOpenPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	symbol := params["symbol"]
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required for Hotcoin GetOpenPositionsRaw")
	}
	contractCode := strings.ToLower(strings.ReplaceAll(symbol, "_", ""))
	return c.request(ctx, http.MethodGet, "/api/v1/perpetual/position/"+contractCode+"/list", params, nil, true)
}

// GetHistoryPositionsRaw is a stub returning not implemented error.
func (c *Client) GetHistoryPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return nil, fmt.Errorf("not implemented")
}

// GetOrderDetailRaw returns raw order details.
func (c *Client) GetOrderDetailRaw(ctx context.Context, orderID string, params map[string]string) ([]byte, error) {
	symbol := params[symbolKey]
	if symbol == "" {
		return nil, fmt.Errorf("missing symbol")
	}
	contractCode := strings.ToLower(strings.ReplaceAll(symbol, "_", ""))
	queryParams := map[string]string{
		orderIDKey: orderID,
	}
	for k, v := range params {
		if k != symbolKey {
			queryParams[k] = v
		}
	}
	return c.request(ctx, http.MethodGet, fmt.Sprintf("/api/v1/perpetual/products/%s/orderDetail", contractCode), queryParams, nil, true)
}

// GetHistoryOrdersRaw returns raw history orders.
func (c *Client) GetHistoryOrdersRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	symbol := params[symbolKey]
	if symbol == "" {
		return nil, fmt.Errorf("missing symbol")
	}
	contractCode := strings.ToLower(strings.ReplaceAll(symbol, "_", ""))
	return c.request(ctx, http.MethodGet, "/api/v1/perpetual/products/"+contractCode+"/history-list", params, nil, true)
}

// GetOrderPNLRaw returns raw deal records.
func (c *Client) GetOrderPNLRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.request(ctx, http.MethodGet, "/api/v1/perpetual/bills/deal-record", params, nil, true)
}

// QueryCustomPrivate sends a signed private request to any path (useful for testing custom paths).
func (c *Client) QueryCustomPrivate(ctx context.Context, method, path string, query map[string]string) ([]byte, error) {
	return c.request(ctx, method, path, query, nil, true)
}
