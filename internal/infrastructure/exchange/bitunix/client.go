package bitunix

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
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
	"crypto-bot/pkg/httpclient"
	"crypto-bot/pkg/ratelimit"

	transportlog "github.com/dangnmh/transport"
	"golang.org/x/time/rate"
)

// Client is the Bitunix REST API client.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	apiSecret  string
	logger     *slog.Logger
	limiter    *ratelimit.ExchangeRateLimiter
}

// NewClient creates a new Bitunix client.
func NewClient(httpClient *http.Client, baseURL, apiKey, apiSecret string, logCfg config.LoggingConfig) *Client {
	logger := slog.Default().With("component", "exchange").With("exchange", "bitunix")

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
					"GET|/",
					"GET|",
					"GET|/api/v1/futures/market/tickers",
					"GET|/api/v1/futures/market/trading_pairs",
					"GET|/api/v1/futures/market/funding_rate/batch",
				},
			}),
			transportlog.LogOptionRedactSensitive(true),
			transportlog.LogOptionRedactSensitiveKeys([]string{"api-key", paramSign}),
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
		logger:     logger,
		limiter:    limiter,
	}
}

func generateNonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func formatQueryParams(query map[string]string) string {
	if len(query) == 0 {
		return ""
	}
	keys := make([]string, 0, len(query))
	for k := range query {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteString(query[k])
	}
	return sb.String()
}

func (c *Client) sign(nonce string, timestamp int64, queryParams string, body []byte) string {
	tsStr := strconv.FormatInt(timestamp, 10)

	digestInput := nonce + tsStr + c.apiKey + queryParams + string(body)
	digestHash := sha256.Sum256([]byte(digestInput))
	digest := hex.EncodeToString(digestHash[:])

	signInput := digest + c.apiSecret
	signHash := sha256.Sum256([]byte(signInput))
	return hex.EncodeToString(signHash[:])
}

func (c *Client) request(ctx context.Context, method, path string, query map[string]string, bodyPayload any) ([]byte, error) {
	if c.limiter != nil {
		if err := c.limiter.Acquire(ctx, path); err != nil {
			return nil, fmt.Errorf("rate limit acquire: %w", err)
		}
	}

	reqURL, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}

	if len(query) > 0 {
		q := reqURL.Query()
		for k, v := range query {
			q.Set(k, v)
		}
		reqURL.RawQuery = q.Encode()
	}

	var reqBody []byte
	if bodyPayload != nil {
		reqBody, err = json.Marshal(bodyPayload)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
	}

	var bodyReader io.Reader
	if len(reqBody) > 0 {
		bodyReader = bytes.NewReader(reqBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Apply authentication headers if API Key is configured
	if c.apiKey != "" && c.apiSecret != "" {
		nonce := generateNonce()
		timestamp := time.Now().UnixMilli()
		queryParamsStr := formatQueryParams(query)
		signature := c.sign(nonce, timestamp, queryParamsStr, reqBody)

		req.Header.Set("api-key", c.apiKey)
		req.Header.Set("nonce", nonce)
		req.Header.Set("timestamp", strconv.FormatInt(timestamp, 10))
		req.Header.Set(paramSign, signature)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

// RawRequest executes a signed HTTP request of any method to the Bitunix API.
func (c *Client) RawRequest(ctx context.Context, method, path string, query map[string]string, body []byte) ([]byte, error) {
	var bodyPayload any
	if len(body) > 0 {
		var payloadMap map[string]any
		if err := json.Unmarshal(body, &payloadMap); err == nil {
			bodyPayload = payloadMap
		} else {
			var payloadArray []any
			if err := json.Unmarshal(body, &payloadArray); err == nil {
				bodyPayload = payloadArray
			} else {
				bodyPayload = body
			}
		}
	}
	return c.request(ctx, method, path, query, bodyPayload)
}

func (c *Client) GetFundingRateRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.request(ctx, http.MethodGet, "/api/v1/futures/market/funding_rate", params, nil)
}

func (c *Client) GetTickersRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.request(ctx, http.MethodGet, "/api/v1/futures/market/ticker", params, nil)
}

func (c *Client) GetOpenPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.request(ctx, http.MethodGet, "/api/v1/futures/position/get_pending_positions", params, nil)
}

func (c *Client) GetHistoryPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.request(ctx, http.MethodGet, "/api/v1/futures/position/get_history_positions", params, nil)
}

func (c *Client) GetOrderDetailRaw(ctx context.Context, orderID string, params map[string]string) ([]byte, error) {
	query := map[string]string{paramOrderID: orderID}
	maps.Copy(query, params)
	return c.request(ctx, http.MethodGet, "/api/v1/futures/trade/get_order_detail", query, nil)
}

func (c *Client) GetHistoryOrdersRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.request(ctx, http.MethodGet, "/api/v1/futures/trade/get_history_orders", params, nil)
}

func (c *Client) GetOrderPNLRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.request(ctx, http.MethodGet, "/api/v1/futures/trade/get_history_trades", params, nil)
}
