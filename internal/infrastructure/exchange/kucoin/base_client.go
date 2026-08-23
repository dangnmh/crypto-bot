package kucoin

import (
	"bytes"
	"context"
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

	transportlog "github.com/dangnmh/transport"
	"golang.org/x/time/rate"
)

// BaseClient encapsulates shared transport, authentication, signing, and rate limiting for KuCoin.
type BaseClient struct {
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

// NewBaseClient creates a new KuCoin BaseClient.
func NewBaseClient(
	httpClient *http.Client,
	baseURL string,
	apiKey string,
	apiSecret string,
	passphrase string,
	logCfg config.LoggingConfig,
) *BaseClient {
	logger := slog.Default().With("exchange", exchange.ExchangeKucoin)

	var clientCopy http.Client
	if httpClient != nil {
		clientCopy = *httpClient
	}
	if clientCopy.Transport == nil {
		clientCopy.Transport = http.DefaultTransport
	}

	if logCfg.HTTP {
		rt := clientCopy.Transport
		rt = transportlog.NewTransportLog(
			rt,
			transportlog.LogOptionLogger(logger),
			transportlog.LogOptionMatcherConfig(transportlog.MatcherConfig{
				OnStatus:       []int{0},
				WhiteListPaths: []string{"*"},
				BlackListPaths: []string{
					"GET|/api/v1/timestamp",
					"GET|/api/v1/allTickers",
					"GET|/api/v1/contracts/active",
					"POST|/api/v1/bullet-public",
					"POST|/api/v1/bullet-private",
					"GET|/api/v1/level2/depth20",
					"GET|/api/v1/level2/depth100",
					"GET|/api/v1/level2/snapshot",
					"GET|/api/v1/market/allTickers",
					"GET|/api/v1/market/orderbook/level2_100",
					"GET|/api/v2/symbols",
				},
			}),
			transportlog.LogOptionRedactSensitive(true),
			transportlog.LogOptionRedactSensitiveKeys([]string{headerKey, headerAuthPhrase}),
			transportlog.LogOptionQueryParams(true),
		)
		clientCopy.Transport = rt
	}
	clientCopy.Transport = httpclient.WrapWithRequestID(clientCopy.Transport)

	limiter := ratelimit.NewExchangeRateLimiter(rate.Limit(30), 10, nil)

	return &BaseClient{
		httpClient: &clientCopy,
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

// HTTPClient returns the configured *http.Client.
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

// Passphrase returns the API passphrase.
func (c *BaseClient) Passphrase() string {
	return c.passphrase
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

// Limiter returns the rate limiter.
func (c *BaseClient) Limiter() *ratelimit.ExchangeRateLimiter {
	return c.limiter
}

// Request executes an HTTP request with rate limiting and optional KuCoin signing headers.
func (c *BaseClient) Request(ctx context.Context, method, path string, params map[string]string, body []byte, signed bool) ([]byte, error) {
	if c.limiter != nil {
		if err := c.limiter.Acquire(ctx, path); err != nil {
			return nil, fmt.Errorf("rate limiter: %w", err)
		}
	}

	fullURL := c.baseURL + path
	queryString := ""
	if len(params) > 0 {
		query := url.Values{}
		for k, v := range params {
			query.Set(k, v)
		}
		queryString = query.Encode()
		if strings.Contains(fullURL, "?") {
			fullURL += "&" + queryString
		} else {
			fullURL += "?" + queryString
		}
	}

	var reqBody io.Reader
	if len(body) > 0 {
		reqBody = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	if signed && c.apiKey != "" {
		timestamp := strconv.FormatInt(c.clock.Now().UnixMilli(), 10)
		signPath := path
		if queryString != "" {
			signPath += "?" + queryString
		}
		sign := SignRequest(c.apiSecret, timestamp, method, signPath, string(body))
		passphraseSign := SignPassphrase(c.apiSecret, c.passphrase)

		req.Header.Set(headerKey, c.apiKey)
		req.Header.Set(headerSign, sign)
		req.Header.Set(headerTimestamp, timestamp)
		req.Header.Set(headerAuthPhrase, passphraseSign)
		req.Header.Set(headerVersion, "2")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &exchange.APIError{
			StatusCode: resp.StatusCode,
			Message:    string(respBody),
			Path:       path,
		}
	}

	return respBody, nil
}
