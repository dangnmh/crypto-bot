package bitget

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
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/httpclient"
	"crypto-bot/pkg/ratelimit"
	"crypto-bot/pkg/ticker"

	"golang.org/x/time/rate"

	transportlog "github.com/dangnmh/transport"

	"crypto-bot/pkg/xjson"
)

// Client is the Bitget V2 Futures REST API client.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	apiSecret  string
	passphrase string
	logCfg     config.LoggingConfig
	logger     *slog.Logger
	clock      exchange.Clock
	limiter    *ratelimit.ExchangeRateLimiter
}

// NewClient creates a new Bitget API client using the provided HTTP client.
func NewClient(httpClient *http.Client, baseURL, apiKey, apiSecret, passphrase string, logCfg config.LoggingConfig) *Client {
	logger := slog.Default().With("component", "exchange").With("exchange", "bitget")

	if passphrase == "" {
		passphrase = os.Getenv("BITGET_PASSPHRASE")
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
						"GET|/api/v2/public/time",
						"GET|/api/v2/mix/market/current-fund-rate",
						"GET|/api/v2/mix/market/contracts",
						"GET|/api/v2/mix/market/tickers",
					},
				}),
				transportlog.LogOptionRedactSensitive(true),
				transportlog.LogOptionRedactSensitiveKeys([]string{headerKey, headerPassphrase}),
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

	limiter := ratelimit.NewExchangeRateLimiter(rate.Limit(10), 2, nil)

	return &Client{
		httpClient: finalClient,
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		apiSecret:  apiSecret,
		passphrase: passphrase,
		logCfg:     logCfg,
		logger:     logger,
		clock:      exchange.RealClock{},
		limiter:    limiter,
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
	c.logger.InfoContext(ctx, "🔗 Warming up Bitget connection pool...", slog.Duration("interval", interval))

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
	reqURL, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("parse path: %w", err)
	}

	q := reqURL.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	if len(q) > 0 {
		reqURL.RawQuery = q.Encode()
	}
	urlPath := reqURL.String()

	fullURL := c.baseURL + urlPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create GET request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("locale", "en-US")

	// Bitget private endpoints require credentials
	isPrivate := !strings.Contains(path, "/public/") && !strings.Contains(path, "/market/")
	if isPrivate && c.apiKey != "" {
		ts := strconv.FormatInt(c.clock.Now().UnixMilli(), 10)
		sig := SignRequest(c.apiSecret, ts, http.MethodGet, urlPath, "")
		req.Header.Set(headerKey, c.apiKey)
		req.Header.Set(headerSign, sig)
		req.Header.Set(headerTimestamp, ts)
		req.Header.Set(headerPassphrase, c.passphrase)
	}

	return c.doRequest(ctx, req)
}

// Post makes a signed POST request.
func (c *Client) Post(ctx context.Context, path string, body any) ([]byte, error) {
	return c.PostCtx(ctx, path, body)
}

// PostCtx makes a signed POST request with context.
func (c *Client) PostCtx(ctx context.Context, path string, body any) ([]byte, error) {
	fullURL := c.baseURL + path

	var bodyReader io.Reader
	var bodyStr string
	if body != nil {
		bodyBytes, err := xjson.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal POST body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
		bodyStr = string(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create POST request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("locale", "en-US")

	if c.apiKey != "" {
		ts := strconv.FormatInt(c.clock.Now().UnixMilli(), 10)
		sig := SignRequest(c.apiSecret, ts, http.MethodPost, path, bodyStr)
		req.Header.Set(headerKey, c.apiKey)
		req.Header.Set(headerSign, sig)
		req.Header.Set(headerTimestamp, ts)
		req.Header.Set(headerPassphrase, c.passphrase)
	}

	return c.doRequest(ctx, req)
}

func (c *Client) doRequest(ctx context.Context, req *http.Request) ([]byte, error) {
	if c.limiter != nil {
		if err := c.limiter.Acquire(ctx, req.URL.Path); err != nil {
			return nil, fmt.Errorf("rate limit acquire: %w", err)
		}
	}

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

// SupportLeverageOnOrder returns false since Bitget doesn't support setting leverage directly on orders.
func (c *Client) SupportLeverageOnOrder() bool {
	return false
}

// RawRequest executes a signed or unsigned request to the Bitget API returning raw bytes.
func (c *Client) RawRequest(ctx context.Context, method, path string, query map[string]string, body []byte) ([]byte, error) {
	if method == http.MethodPost {
		var bodyVal any
		if len(body) > 0 {
			var temp map[string]any
			if err := xjson.Unmarshal(body, &temp); err == nil {
				bodyVal = temp
			} else {
				bodyVal = body
			}
		}
		return c.PostCtx(ctx, path, bodyVal)
	}
	return c.GetCtx(ctx, path, query)
}

// GetFundingRateRaw fetches raw funding rates.
func (c *Client) GetFundingRateRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, pathFundingRate, params, nil)
}

// GetTickersRaw fetches raw tickers.
func (c *Client) GetTickersRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, pathTickers, params, nil)
}

// GetOpenPositionsRaw fetches raw positions.
func (c *Client) GetOpenPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, pathOpenPositions, params, nil)
}

// GetHistoryPositionsRaw fetches raw history positions.
func (c *Client) GetHistoryPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, pathHistoryPositions, params, nil)
}

// GetOrderDetailRaw fetches raw order detail.
func (c *Client) GetOrderDetailRaw(ctx context.Context, orderID string, params map[string]string) ([]byte, error) {
	p := make(map[string]string)
	maps.Copy(p, params)
	p["orderId"] = orderID
	return c.RawRequest(ctx, http.MethodGet, pathGetOrder, p, nil)
}

// GetHistoryOrdersRaw fetches raw history orders.
func (c *Client) GetHistoryOrdersRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/api/v2/mix/order/orders-history", params, nil)
}

// GetOrderPNLRaw fetches raw realized pnl logs for debugging.
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
