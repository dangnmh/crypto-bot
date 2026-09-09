package bybit

import (
	"bytes"
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

// BaseClient encapsulates shared transport, signing, rate limiting, and execution for Bybit V5 API.
type BaseClient struct {
	httpClient  *http.Client
	baseURL     string
	apiKey      string
	apiSecret   string
	accountType string // "standard" or "unified"
	logCfg      config.LoggingConfig
	logger      *slog.Logger
	clock       exchange.Clock
	limiter     *ratelimit.ExchangeRateLimiter
}

// NewBaseClient creates a new Bybit BaseClient.
func NewBaseClient(httpClient *http.Client, baseURL, apiKey, apiSecret, accountType string, logCfg config.LoggingConfig) *BaseClient {
	logger := slog.Default().With("component", "exchange").With("exchange", "bybit")

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
					WhiteListPaths: []string{"*"}, // match all paths
					BlackListPaths: []string{
						"GET|/v5/market/tickers",
						"GET|/v5/market/time",
						"GET|/v5/market/instruments-info",
					}, // match everything cleanly
				}),
				transportlog.LogOptionRedactSensitive(true),
				transportlog.LogOptionRedactSensitiveKeys([]string{"X-Bapi-Api-Key", "X-BAPI-API-KEY"}),
				transportlog.LogOptionQueryParams(true),
			)
			clientCopy.Transport = rt
		}
		clientCopy.Transport = httpclient.WrapWithRequestID(clientCopy.Transport)
	}

	limiter := ratelimit.NewExchangeRateLimiter(rate.Limit(10), 2, nil)

	return &BaseClient{
		httpClient:  &clientCopy,
		baseURL:     strings.TrimRight(baseURL, "/"),
		apiKey:      apiKey,
		apiSecret:   apiSecret,
		accountType: accountType,
		logCfg:      logCfg,
		logger:      logger,
		clock:       exchange.RealClock{},
		limiter:     limiter,
	}
}

// HTTPClient returns the underlying HTTP client.
func (c *BaseClient) HTTPClient() *http.Client {
	return c.httpClient
}

// BaseURL returns the configured base URL.
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

// AccountType returns the configured account type ("standard" or "unified").
func (c *BaseClient) AccountType() string {
	return c.accountType
}

// Logger returns the logger.
func (c *BaseClient) Logger() *slog.Logger {
	return c.logger
}

// Clock returns the clock.
func (c *BaseClient) Clock() exchange.Clock {
	return c.clock
}

// SetClock configures a custom clock implementation.
func (c *BaseClient) SetClock(clk exchange.Clock) {
	if clk != nil {
		c.clock = clk
	}
}

// Limiter returns the rate limiter.
func (c *BaseClient) Limiter() *ratelimit.ExchangeRateLimiter {
	return c.limiter
}

func (c *BaseClient) signRequest(method string, bodyBytes []byte, queryString string) (string, string) {
	ts := strconv.FormatInt(c.clock.Now().UnixMilli(), 10)
	recvWindow := "5000"

	var signatureBase string
	if method == http.MethodPost {
		signatureBase = ts + c.apiKey + recvWindow + string(bodyBytes)
	} else {
		signatureBase = ts + c.apiKey + recvWindow + queryString
	}

	hmac256 := hmac.New(sha256.New, []byte(c.apiSecret))
	hmac256.Write([]byte(signatureBase))
	signature := hex.EncodeToString(hmac256.Sum(nil))

	return ts, signature
}

// RawRequest executes an HTTP request to Bybit V5 with rate limiting and signing.
//
//nolint:cyclop // Request wrappers are naturally complex
func (c *BaseClient) RawRequest(ctx context.Context, method, path string, query map[string]string, body []byte) ([]byte, error) {
	if c.limiter != nil {
		if err := c.limiter.Acquire(ctx, path); err != nil {
			return nil, fmt.Errorf("rate limit acquire: %w", err)
		}
	}

	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}

	reqURL, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}

	var queryString string
	if method == http.MethodGet || method == http.MethodDelete {
		q := reqURL.Query()
		for k, v := range query {
			q.Set(k, v)
		}
		if len(q) > 0 {
			queryString = q.Encode()
			reqURL.RawQuery = queryString
		}
	}
	urlPath := reqURL.String()

	req, err := http.NewRequestWithContext(ctx, method, urlPath, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", "bybit.api.go/1.0.7")
	if method != http.MethodGet && len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}

	isSigned := c.apiKey != "" && !strings.Contains(path, "/market/")
	if isSigned {
		ts, signature := c.signRequest(method, body, queryString)
		req.Header.Set("X-BAPI-API-KEY", c.apiKey)
		req.Header.Set("X-BAPI-SIGN-TYPE", "2")
		req.Header.Set("X-BAPI-TIMESTAMP", ts)
		req.Header.Set("X-BAPI-RECV-WINDOW", "5000")
		req.Header.Set("X-BAPI-SIGN", signature)
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

	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("HTTP error: status=%d, body=%s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// Response represents a standard Bybit V5 JSON response envelope.
type Response[T any] struct {
	RetCode int    `json:"retCode"`
	RetMsg  string `json:"retMsg"`
	Result  T      `json:"result"`
}

// ParseResponse unmarshals a Bybit V5 JSON response and validates retCode == 0.
func ParseResponse[T any](body []byte, errPrefix string) (T, error) {
	var resp Response[T]
	if err := xjson.Unmarshal(body, &resp); err != nil {
		var zero T
		return zero, fmt.Errorf("%s json unmarshal: %w", errPrefix, err)
	}
	if resp.RetCode != 0 {
		var zero T
		return zero, fmt.Errorf("%s error: retCode=%d, retMsg=%s", errPrefix, resp.RetCode, resp.RetMsg)
	}
	return resp.Result, nil
}

// DecodeListResponse unmarshals a Bybit V5 list response and validates retCode == 0.
func DecodeListResponse[T any](body []byte, errPrefix string) ([]T, error) {
	var resp Response[struct {
		List []T `json:"list"`
	}]
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("%s json unmarshal: %w", errPrefix, err)
	}
	if resp.RetCode != 0 {
		return nil, fmt.Errorf("%s error: retCode=%d, retMsg=%s", errPrefix, resp.RetCode, resp.RetMsg)
	}
	return resp.Result.List, nil
}
