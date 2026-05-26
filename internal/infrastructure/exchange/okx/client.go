package okx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptrace"
	"os"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	applogger "crypto-bot/pkg/logger"
	"crypto-bot/pkg/ticker"

	transportlog "github.com/dangnmh/transport"
)

// Client is the OKX V5 Futures REST API client.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	apiSecret  string
	passphrase string
	logCfg     config.LoggingConfig
	logger     *slog.Logger
}

// NewClient creates a new OKX API client using the provided HTTP client.
func NewClient(httpClient *http.Client, baseURL, apiKey, apiSecret, passphrase string, logCfg config.LoggingConfig) *Client {
	logger := slog.Default().With("component", "exchange").With("exchange", "okx")

	if passphrase == "" {
		passphrase = os.Getenv("OKX_PASSPHRASE")
	}

	if logCfg.HTTP && httpClient.Transport != nil {
		rt := httpClient.Transport
		rt = transportlog.NewTransportLog(rt,
			transportlog.LogOptionLogger(logger),
			transportlog.LogOptionMatcherConfig(transportlog.MatcherConfig{
				OnStatus:       []int{0},
				WhiteListPaths: []string{"*"},
				BlackListPaths: []string{
					"GET|/api/v5/public/time",
					"GET|/api/v5/market/tickers",
					"GET|/api/v5/public/instruments",
					"GET|/api/v5/public/funding-rate",
					"GET|/api/v5/market/candles",
				},
			}),
			transportlog.LogOptionRedactSensitive(true),
			transportlog.LogOptionRedactSensitiveKeys([]string{"OK-ACCESS-KEY", "OK-ACCESS-PASSPHRASE"}),
		)
		httpClient.Transport = rt
	}

	return &Client{
		httpClient: httpClient,
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		apiSecret:  apiSecret,
		passphrase: passphrase,
		logCfg:     logCfg,
		logger:     logger,
	}
}

// WarmUp pre-establishes connection pool and maintains it via periodic public calls.
func (c *Client) WarmUp(ctx context.Context, interval time.Duration) {
	applogger.WithCtx(ctx, c.logger).Info("🔗 Warming up OKX connection pool...", "interval", interval)

	ticker.RunImmediate(ctx, interval, func() bool {
		_, err := c.GetCtx(ctx, pathServerTime, nil)
		if err != nil {
			applogger.WithCtx(ctx, c.logger).Debug("Warmup server time call failed", "error", err)
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
	urlPath := path
	if len(params) > 0 {
		parts := make([]string, 0, len(params))
		for k, v := range params {
			parts = append(parts, fmt.Sprintf("%s=%s", k, v))
		}
		urlPath += "?" + strings.Join(parts, "&")
	}

	url := c.baseURL + urlPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create GET request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// OKX private endpoints require credentials
	isPrivate := !strings.Contains(path, "/public/") && !strings.Contains(path, "/market/")
	if isPrivate && c.apiKey != "" {
		ts := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
		sig := SignRequest(c.apiSecret, ts, http.MethodGet, urlPath, "")
		req.Header.Set("OK-ACCESS-KEY", c.apiKey)
		req.Header.Set("OK-ACCESS-SIGN", sig)
		req.Header.Set("OK-ACCESS-TIMESTAMP", ts)
		req.Header.Set("OK-ACCESS-PASSPHRASE", c.passphrase)
	}

	return c.doRequest(req)
}

// Post makes a signed POST request.
func (c *Client) Post(ctx context.Context, path string, body interface{}) ([]byte, error) {
	return c.PostCtx(ctx, path, body)
}

// PostCtx makes a signed POST request with context.
func (c *Client) PostCtx(ctx context.Context, path string, body interface{}) ([]byte, error) {
	url := c.baseURL + path

	var bodyReader io.Reader
	var bodyStr string
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal POST body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
		bodyStr = string(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create POST request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	if c.apiKey != "" {
		ts := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
		sig := SignRequest(c.apiSecret, ts, http.MethodPost, path, bodyStr)
		req.Header.Set("OK-ACCESS-KEY", c.apiKey)
		req.Header.Set("OK-ACCESS-SIGN", sig)
		req.Header.Set("OK-ACCESS-TIMESTAMP", ts)
		req.Header.Set("OK-ACCESS-PASSPHRASE", c.passphrase)
	}

	return c.doRequest(req)
}

func (c *Client) doRequest(req *http.Request) ([]byte, error) {
	trace := &httptrace.ClientTrace{
		GotConn: func(connInfo httptrace.GotConnInfo) {
			if !connInfo.Reused {
				applogger.WithCtx(req.Context(), c.logger).Debug("HTTP new connection",
					"was_idle", connInfo.WasIdle,
					"idle_time", connInfo.IdleTime,
				)
			}
		},
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))

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
