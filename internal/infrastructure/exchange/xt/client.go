package xt

import (
	"bytes"
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
	"sort"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/pkg/httpclient"
	"crypto-bot/pkg/xjson"

	transportlog "github.com/dangnmh/transport"
)

const (
	sideLong          = "LONG"
	sideShort         = "SHORT"
	sideBuy           = "BUY"
	sideSell          = "SELL"
	paramSymbol       = "symbol"
	paramOrderId      = "orderId"
	modeIsolated      = "ISOLATED"
	modeCrossed       = "CROSSED"
	opSubscribe       = "SUBSCRIBE"
	paramParams       = "params"
	paramMethod       = "method"
	paramPositionSide = "positionSide"
	paramPositionType = "positionType"
	channelTicker     = "ticker"
	channelPosition   = "personal.position"
	channelOrder      = "personal.order"
	paramStartTime    = "startTime"
	paramLimit        = "limit"
)

// Client is the XT.com REST API client.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	apiSecret  string
	logCfg     config.LoggingConfig
	logger     *slog.Logger
}

// NewClient creates a new XT.com client.
func NewClient(
	httpClient *http.Client,
	baseURL string,
	apiKey string,
	apiSecret string,
	logCfg config.LoggingConfig,
) *Client {
	logger := slog.Default().With("component", "exchange").With("exchange", "xt")

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
					"GET|/future/market/v1/public/time",
					"GET|/future/market/v1/public/cg/contracts",
					"GET|/future/market/v1/public/symbol/list",
					"GET|/future/user/v1/user/listen-key",
				},
			}),
			transportlog.LogOptionRedactSensitive(true),
			transportlog.LogOptionRedactSensitiveKeys([]string{
				"validate-appkey",
				"validate-singature", // nolint:misspell // XT.com API explicitly spells this header field as validate-singature
				"validate-signature",
			}),
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
	}
}

func buildStringToSign(apiKey, timestamp, path, qStr, bodyStr string) string {
	fixedHeader := fmt.Sprintf("validate-appkey=%s&validate-timestamp=%s", apiKey, timestamp)
	switch {
	case bodyStr != "":
		return fmt.Sprintf("%s#%s#%s", fixedHeader, path, bodyStr)
	case qStr != "":
		return fmt.Sprintf("%s#%s#%s", fixedHeader, path, qStr)
	default:
		return fmt.Sprintf("%s#%s", fixedHeader, path)
	}
}

// request sends an HTTP request to XT.com, signing it if requested.
func (c *Client) request(
	ctx context.Context,
	method string,
	path string,
	params map[string]string,
	body []byte,
	signed bool,
) ([]byte, error) {
	reqURL, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}

	// Prepare query string
	if len(params) > 0 {
		q := reqURL.Query()
		for k, v := range params {
			q.Set(k, v)
		}
		reqURL.RawQuery = q.Encode()
	}

	var reqBody io.Reader
	if len(body) > 0 {
		reqBody = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), reqBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	// Set headers
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	} else {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	if signed {
		timestamp := fmt.Sprintf("%d", time.Now().UnixMilli())

		// Sort query parameters alphabetically for XT's string-to-sign calculation
		var qStr string
		if len(params) > 0 {
			var keys []string
			for k := range params {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			var parts []string
			for _, k := range keys {
				parts = append(parts, fmt.Sprintf("%s=%s", k, params[k]))
			}
			qStr = strings.Join(parts, "&")
		}

		toSign := buildStringToSign(c.apiKey, timestamp, path, qStr, string(body))

		h := hmac.New(sha256.New, []byte(c.apiSecret))
		h.Write([]byte(toSign))
		signature := hex.EncodeToString(h.Sum(nil))

		req.Header.Set("validate-appkey", c.apiKey)
		req.Header.Set("validate-timestamp", timestamp)
		req.Header.Set("validate-signature", signature)
		req.Header.Set("validate-algorithms", "HmacSHA256")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// RawRequest satisfies the RawRequester interface.
func (c *Client) RawRequest(
	ctx context.Context,
	method string,
	path string,
	query map[string]string,
	body []byte,
) ([]byte, error) {
	// Automatically sign if we have credentials
	signed := c.apiKey != ""
	return c.request(ctx, method, path, query, body, signed)
}

// GetFundingRateRaw fetches raw funding rates.
func (c *Client) GetFundingRateRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/future/market/v1/public/cg/funding-rates", params, nil)
}

// GetTickersRaw fetches raw tickers.
func (c *Client) GetTickersRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/future/market/v1/public/q/tickers", params, nil)
}

// GetOpenPositionsRaw fetches raw positions.
func (c *Client) GetOpenPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/future/user/v1/position", params, nil)
}

// GetHistoryPositionsRaw fetches raw history positions.
func (c *Client) GetHistoryPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/future/trade/v1/position/list-history", params, nil)
}

// GetOrderDetailRaw fetches raw order detail.
func (c *Client) GetOrderDetailRaw(
	ctx context.Context,
	orderID string,
	params map[string]string,
) ([]byte, error) {
	p := make(map[string]string)
	maps.Copy(p, params)
	p["orderId"] = orderID
	return c.RawRequest(ctx, http.MethodGet, "/future/trade/v1/order/detail", p, nil)
}

// GetHistoryOrdersRaw fetches raw history orders.
func (c *Client) GetHistoryOrdersRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/future/trade/v1/order/list", params, nil)
}

// GetOrderPNLRaw fetches raw closed PnL metrics.
func (c *Client) GetOrderPNLRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/future/trade/v1/order/pnl", params, nil)
}

// GetListenKey fetches the user listen key for private ws connection.
func (c *Client) GetListenKey(ctx context.Context) (string, error) {
	bodyBytes, err := c.request(ctx, "GET", "/future/user/v1/user/listen-key", nil, nil, true)
	if err != nil {
		return "", fmt.Errorf("request listenKey: %w", err)
	}

	type xtListenKeyResponse struct {
		ReturnCode int64  `json:"returnCode"`
		MsgInfo    string `json:"msgInfo"`
		Result     any    `json:"result"`
	}

	var resp xtListenKeyResponse
	err = xjson.Unmarshal(bodyBytes, &resp)
	if err != nil {
		return "", fmt.Errorf("unmarshal listenKey: %w", err)
	}
	if resp.ReturnCode != 0 {
		return "", fmt.Errorf("listenKey API error: code=%d, msg=%s", resp.ReturnCode, resp.MsgInfo)
	}

	switch v := resp.Result.(type) {
	case string:
		return v, nil
	case map[string]any:
		if lk, ok := v["listenKey"].(string); ok {
			return lk, nil
		}
	}
	return "", fmt.Errorf("unexpected listenKey format: %v", resp.Result)
}
