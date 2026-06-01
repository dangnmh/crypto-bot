package httpclient

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/hashicorp/go-retryablehttp"
)

// PoolConfig defines the configuration for a connection pool.
type PoolConfig struct {
	MaxIdleConns        int
	MaxIdleConnsPerHost int
	IdleConnTimeout     time.Duration
	TLSHandshakeTimeout time.Duration
	DisableCompression  bool
	Timeout             time.Duration

	// Resilience Configuration
	EnableRetry      bool
	RetryMaxAttempts int
	Logger           *slog.Logger
}

// DefaultPoolConfig returns sensible defaults for high-frequency trading.
func DefaultPoolConfig() PoolConfig {
	return PoolConfig{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 5 * time.Second,
		DisableCompression:  false,
		Timeout:             10 * time.Second,

		// Default retry parameters
		EnableRetry:      true,
		RetryMaxAttempts: 3,
	}
}

// checkRetry ensures we only retry GET requests that failed with HTTP 429 (Too Many Requests).
func checkRetry(ctx context.Context, resp *http.Response, err error) (bool, error) {
	if err != nil {
		return false, err
	}
	if resp != nil && resp.Request != nil && resp.Request.Method == http.MethodGet && resp.StatusCode == http.StatusTooManyRequests {
		return true, nil
	}
	return false, nil
}

// NewPool creates an optimized *http.Client with a pre-configured RoundTripper.
func NewPool(cfg PoolConfig) *http.Client {
	transport := &http.Transport{
		MaxIdleConns:        cfg.MaxIdleConns,
		MaxIdleConnsPerHost: cfg.MaxIdleConnsPerHost,
		IdleConnTimeout:     cfg.IdleConnTimeout,
		TLSHandshakeTimeout: cfg.TLSHandshakeTimeout,
		DisableCompression:  cfg.DisableCompression,
	}

	if !cfg.EnableRetry {
		return &http.Client{
			Transport: transport,
			Timeout:   cfg.Timeout,
		}
	}

	retryClient := retryablehttp.NewClient()
	retryClient.RetryMax = cfg.RetryMaxAttempts
	retryClient.Backoff = retryablehttp.RateLimitLinearJitterBackoff
	retryClient.CheckRetry = checkRetry
	retryClient.ErrorHandler = retryablehttp.PassthroughErrorHandler
	retryClient.HTTPClient.Transport = transport
	retryClient.Logger = nil // Skip log from httpretry since client already has log transport

	return &http.Client{
		Transport: &retryablehttp.RoundTripper{Client: retryClient},
		Timeout:   cfg.Timeout,
	}
}
