package deepcoin

import (
	"bytes"
	"context"
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

	transportlog "github.com/dangnmh/transport"

	"crypto-bot/pkg/xjson"
)

// Client is the Deepcoin Futures REST API client.
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

// NewClient creates a new Deepcoin API client using the provided HTTP client.
func NewClient(httpClient *http.Client, baseURL, apiKey, apiSecret, passphrase string, logCfg config.LoggingConfig) *Client {
	logger := slog.Default().With("component", "exchange").With("exchange", "deepcoin")

	if passphrase == "" {
		passphrase = os.Getenv("DEEPCOIN_PASSPHRASE")
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
						"GET|/deepcoin/market/time",
						"GET|/deepcoin/market/tickers",
						"GET|/deepcoin/market/instruments",
						"GET|/deepcoin/trade/funding-rate",
						"GET|/deepcoin/trade/fund-rate/current-funding-rate",
						"GET|/deepcoin/listenkey/acquire",
					},
				}),
				transportlog.LogOptionRedactSensitive(true),
				transportlog.LogOptionRedactSensitiveKeys([]string{"DC-ACCESS-KEY", "DC-ACCESS-PASSPHRASE", "DC-ACCESS-SIGN"}),
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
		bodyBytes, err = xjson.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal POST body: %w", err)
		}
	}
	return c.RawRequest(ctx, http.MethodPost, path, nil, bodyBytes)
}

// RawRequest makes a signed HTTP request of any method to the Deepcoin API.
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
		reqURL.RawQuery = buildDeepcoinQueryString(q)
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

	isPrivate := !strings.Contains(path, "/market/")
	if isPrivate && c.apiKey != "" {
		ts := c.clock.Now().UTC().Format("2006-01-02T15:04:05.000Z")
		sig := SignRequest(c.apiSecret, ts, method, urlPath, bodyStr)
		req.Header.Set("DC-ACCESS-KEY", c.apiKey)
		req.Header.Set("DC-ACCESS-SIGN", sig)
		req.Header.Set("DC-ACCESS-TIMESTAMP", ts)
		req.Header.Set("DC-ACCESS-PASSPHRASE", c.passphrase)
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
		if resp.StatusCode == http.StatusTooManyRequests {
			return nil, &exchange.RateLimitError{
				Message: string(body),
				Path:    path,
			}
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

// Latency measures round-trip time of a public API request in milliseconds.
func (c *Client) Latency(ctx context.Context) (int64, error) {
	start := time.Now()
	_, err := c.GetCtx(ctx, "/deepcoin/market/time", nil)
	if err != nil {
		return 0, err
	}
	return time.Since(start).Milliseconds(), nil
}

type deepcoinListenKeyData struct {
	ListenKey string `json:"listenkey"`
}

func (c *Client) rawCreateListenKey(ctx context.Context) (*deepcoinListenKeyData, error) {
	body, err := c.GetCtx(ctx, "/deepcoin/listenkey/acquire", nil)
	if err != nil {
		return nil, err
	}
	res, err := ParseResponseFirst[deepcoinListenKeyData](body, "acquire_listenkey")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

// CreateListenKey acquires a new user stream listenKey from Deepcoin.
func (c *Client) CreateListenKey(ctx context.Context) (string, error) {
	res, err := c.rawCreateListenKey(ctx)
	if err != nil {
		return "", err
	}
	return res.ListenKey, nil
}

func (c *Client) rawKeepAliveListenKey(ctx context.Context, listenKey string) error {
	params := map[string]string{
		"listenkey": listenKey,
	}
	body, err := c.GetCtx(ctx, "/deepcoin/listenkey/extend", params)
	if err != nil {
		return err
	}
	_, err = ParseResponseFirst[deepcoinListenKeyData](body, "extend_listenkey")
	return err
}

// KeepAliveListenKey extends the lifetime of a listenKey.
func (c *Client) KeepAliveListenKey(ctx context.Context, listenKey string) error {
	return c.rawKeepAliveListenKey(ctx, listenKey)
}

// RawRequest interface implementations for diagnostics.

func (c *Client) GetFundingRateRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.GetCtx(ctx, "/deepcoin/trade/fund-rate/current-funding-rate", params)
}

func (c *Client) GetTickersRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.GetCtx(ctx, "/deepcoin/market/tickers", params)
}

func (c *Client) GetOpenPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.GetCtx(ctx, "/deepcoin/account/positions", params)
}

func (c *Client) GetHistoryPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.GetCtx(ctx, "/deepcoin/account/positions-history", params)
}

func (c *Client) GetOrderDetailRaw(ctx context.Context, orderID string, params map[string]string) ([]byte, error) {
	query := map[string]string{
		"ordId": orderID,
	}
	maps.Copy(query, params)
	return c.GetCtx(ctx, "/deepcoin/trade/orderByID", query)
}

func (c *Client) GetHistoryOrdersRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.GetCtx(ctx, "/deepcoin/trade/orders-history", params)
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
	return xjson.Marshal(info)
}

func buildDeepcoinQueryString(q url.Values) string {
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
