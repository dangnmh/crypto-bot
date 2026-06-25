package binance

import (
	"compress/gzip"
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
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/httpclient"
	"crypto-bot/pkg/ticker"

	transportlog "github.com/dangnmh/transport"

	"crypto-bot/pkg/xjson"
)

// Client is the Binance USD-M Futures REST API client.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	apiSecret  string
	logCfg     config.LoggingConfig
	logger     *slog.Logger
	clock      exchange.Clock
}

// NewClient creates a new Binance Futures REST Client.
func NewClient(httpClient *http.Client, baseURL, apiKey, apiSecret string, logCfg config.LoggingConfig) *Client {
	logger := slog.Default().With("component", "exchange", "exchange", "binance")

	var clientCopy http.Client
	if httpClient != nil {
		clientCopy = *httpClient
	}
	if clientCopy.Transport == nil {
		clientCopy.Transport = http.DefaultTransport
	}

	if clientCopy.Transport != nil {
		if logCfg.HTTP {
			rt := clientCopy.Transport
			rt = &decompressionRoundTripper{underlying: rt}
			rt = transportlog.NewTransportLog(rt,
				transportlog.LogOptionLogger(logger),
				transportlog.LogOptionMatcherConfig(transportlog.MatcherConfig{
					OnStatus:       []int{0},
					WhiteListPaths: []string{"*"},
					BlackListPaths: []string{
						"GET|/fapi/v1/ping",
						"GET|/fapi/v1/time",
						"GET|/fapi/v1/ticker/24hr",
						"GET|/fapi/v1/ticker/bookTicker",
						"GET|/fapi/v1/exchangeInfo",
						"GET|/fapi/v1/premiumIndex",
						"POST|/fapi/v1/listenKey",
					},
				}),
				transportlog.LogOptionRedactSensitive(true),
				transportlog.LogOptionRedactSensitiveKeys([]string{
					"X-MBX-APIKEY",
				}),
				transportlog.LogOptionQueryParams(true),
			)
			clientCopy.Transport = rt
		}
		clientCopy.Transport = httpclient.WrapWithRequestID(clientCopy.Transport)
	}

	if baseURL == "" {
		baseURL = "https://fapi.binance.com"
	}

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

// SetClock configures a custom clock implementation.
func (c *Client) SetClock(clk exchange.Clock) {
	if clk != nil {
		c.clock = clk
	}
}

// request sends a raw HTTP request to the Binance API, automatically signing it if required.
func (c *Client) request(ctx context.Context, method, path string, params map[string]any, signed bool, result any) error {
	reqURL, err := url.Parse(path)
	if err != nil {
		return fmt.Errorf("parse path: %w", err)
	}

	// Extract existing query parameters
	mergedParams := make(map[string]any)
	for k, vs := range reqURL.Query() {
		if len(vs) > 0 {
			mergedParams[k] = vs[0]
		}
	}
	maps.Copy(mergedParams, params)

	if len(mergedParams) > 0 || signed {
		reqURL.RawQuery = c.encodeParams(mergedParams, signed)
	}

	fullURL := c.baseURL + reqURL.String()
	req, err := http.NewRequestWithContext(ctx, method, fullURL, http.NoBody)
	if err != nil {
		return fmt.Errorf("create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("X-MBX-APIKEY", c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return handleBinanceError(ctx, resp.StatusCode, body, path, c.logger)
	}

	if result != nil {
		if err := xjson.Unmarshal(body, result); err != nil {
			return fmt.Errorf("unmarshal response: %w (body=%s)", err, string(body))
		}
	}

	return nil
}

// encodeParams formats and signs request parameters alphabetically.
func (c *Client) encodeParams(params map[string]any, signed bool) string {
	values := url.Values{}
	for k, v := range params {
		if v != nil {
			values.Set(k, fmt.Sprintf("%v", v))
		}
	}

	if signed {
		if !values.Has("timestamp") {
			values.Set("timestamp", strconv.FormatInt(c.clock.Now().UnixMilli(), 10))
		}
		queryString := values.Encode()

		mac := hmac.New(sha256.New, []byte(c.apiSecret))
		mac.Write([]byte(queryString))
		signature := hex.EncodeToString(mac.Sum(nil))

		queryString += "&signature=" + signature
		return queryString
	}

	return values.Encode()
}

// WarmUp maintains the connection pool via periodic pings.
func (c *Client) WarmUp(ctx context.Context, interval time.Duration) {
	ticker.RunImmediate(ctx, interval, func() bool {
		err := c.request(ctx, http.MethodGet, "/fapi/v1/ping", nil, false, nil)
		if err != nil {
			c.logger.DebugContext(ctx, "Binance warmup connectivity check failed", slog.Any("error", err))
		}
		return true
	})
}

// Latency measures round-trip time of a ping request (ms).
func (c *Client) Latency(ctx context.Context) (int64, error) {
	start := time.Now()
	err := c.request(ctx, http.MethodGet, "/fapi/v1/ping", nil, false, nil)
	if err != nil {
		return 0, err
	}
	return time.Since(start).Milliseconds(), nil
}

// CreateListenKey starts a new Binance user data stream and returns its listenKey.
func (c *Client) CreateListenKey(ctx context.Context) (string, error) {
	var resp listenKeyResponse
	err := c.request(ctx, http.MethodPost, "/fapi/v1/listenKey", nil, false, &resp)
	if err != nil {
		return "", fmt.Errorf("binance start user data stream: %w", err)
	}
	return resp.ListenKey, nil
}

// KeepAliveListenKey pings the active Binance user data stream to keep it open.
func (c *Client) KeepAliveListenKey(ctx context.Context) error {
	err := c.request(ctx, http.MethodPut, "/fapi/v1/listenKey", nil, false, nil)
	if err != nil {
		return fmt.Errorf("binance keepalive user data stream: %w", err)
	}
	return nil
}

// SupportLeverageOnOrder returns false since Binance doesn't support setting leverage directly on orders.
func (c *Client) SupportLeverageOnOrder() bool {
	return false
}

type decompressionRoundTripper struct {
	underlying http.RoundTripper
}

func (d *decompressionRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("Accept-Encoding") != "" {
		req.Header.Set("Accept-Encoding", "gzip")
	}

	resp, err := d.underlying.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	if resp.Header.Get("Content-Encoding") == "gzip" {
		gzReader, gzErr := gzip.NewReader(resp.Body)
		if gzErr == nil {
			resp.Body = &gzipReadCloser{
				gz:   gzReader,
				body: resp.Body,
			}
			resp.Header.Del("Content-Encoding")
			resp.Header.Del("Content-Length")
		}
	}

	return resp, nil
}

type gzipReadCloser struct {
	gz   *gzip.Reader
	body io.ReadCloser
}

func (g *gzipReadCloser) Read(p []byte) (int, error) {
	return g.gz.Read(p)
}

func (g *gzipReadCloser) Close() error {
	err1 := g.gz.Close()
	err2 := g.body.Close()
	if err1 != nil {
		return err1
	}
	return err2
}

func handleBinanceError(ctx context.Context, statusCode int, body []byte, path string, logger *slog.Logger) error {
	if statusCode == http.StatusTooManyRequests || statusCode == 418 {
		return &exchange.RateLimitError{
			Message: string(body),
			Path:    path,
		}
	}
	bodyStr := string(body)
	if strings.Contains(bodyStr, "-4046") || strings.Contains(strings.ToLower(bodyStr), "no need to change") {
		return nil
	}
	logger.WarnContext(ctx, "🟡 Binance Non-200 response",
		"status", statusCode,
		"path", path,
		"body", bodyStr,
	)
	return fmt.Errorf("binance API error: status=%d body=%s", statusCode, bodyStr)
}
