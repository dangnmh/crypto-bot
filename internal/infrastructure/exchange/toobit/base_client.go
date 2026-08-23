package toobit

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
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

// BaseClient encapsulates shared transport, signing, rate limiting, and execution for Toobit.
type BaseClient struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	apiSecret  string
	logCfg     config.LoggingConfig
	logger     *slog.Logger
	clock      exchange.Clock
	limiter    *ratelimit.ExchangeRateLimiter
}

// NewBaseClient creates a new Toobit BaseClient.
func NewBaseClient(httpClient *http.Client, baseURL, apiKey, apiSecret string, logCfg config.LoggingConfig) *BaseClient {
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
					"GET|/quote/v1/ticker/24hr",
					"GET|/api/v1/futures/fundingRate",
					"GET|/api/v1/exchangeInfo",
					"GET|/api/v1/futures/riskLimits",
					"POST|/api/v1/listenKey",
					"PUT|/api/v1/listenKey",
					"GET|/quote/v1/depth",
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

	return &BaseClient{
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

// HTTPClient returns the configured HTTP client.
func (c *BaseClient) HTTPClient() *http.Client {
	return c.httpClient
}

// BaseURL returns the base URL.
func (c *BaseClient) BaseURL() string {
	return c.baseURL
}

// APIKey returns the API key.
func (c *BaseClient) APIKey() string {
	return c.apiKey
}

// APISecret returns the API secret.
func (c *BaseClient) APISecret() string {
	return c.apiSecret
}

// Logger returns the logger.
func (c *BaseClient) Logger() *slog.Logger {
	return c.logger
}

// Clock returns the clock.
func (c *BaseClient) Clock() exchange.Clock {
	return c.clock
}

// SetClock sets a custom clock.
func (c *BaseClient) SetClock(clk exchange.Clock) {
	if clk != nil {
		c.clock = clk
	}
}

// Request executes an HTTP request with rate limiting, optional HMAC-SHA256 signature, and error checking.
func (c *BaseClient) Request(ctx context.Context, method, path string, params map[string]string, signed bool) ([]byte, error) {
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
	defer func() { _ = resp.Body.Close() }()

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

func (c *BaseClient) signParams(method string, params map[string]string) (io.Reader, string, error) {
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

func (c *BaseClient) setHeaders(req *http.Request, method string, signed bool) {
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

	if codeStr != "200" && codeStr != "0" && codeStr != "200000" && codeStr != "" {
		return fmt.Errorf("API error code %s: %s", codeStr, envelope.Message)
	}
	return nil
}
