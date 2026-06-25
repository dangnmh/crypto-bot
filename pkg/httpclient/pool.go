package httpclient

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptrace"
	"time"

	"crypto-bot/pkg/tracectx"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

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

// requestIDRoundTripper injects request tracing headers extracted from context.
type requestIDRoundTripper struct {
	next http.RoundTripper
}

func (r *requestIDRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	reqID := tracectx.RequestID(ctx)

	if reqID != "" {
		req = req.Clone(ctx)
		req.Header.Set("X-Request-ID", reqID)
	}
	return r.next.RoundTrip(req)
}

// WrapWithRequestID wraps a RoundTripper to inject request ID headers from context.
func WrapWithRequestID(next http.RoundTripper) http.RoundTripper {
	if next == nil {
		return nil
	}
	return &requestIDRoundTripper{next: next}
}

// traceRoundTripper injects request tracing headers extracted from context and logs connection trace details.
type traceRoundTripper struct {
	next   http.RoundTripper
	logger *slog.Logger
}

func (t *traceRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	reqID := tracectx.RequestID(ctx)

	if reqID != "" {
		req = req.Clone(ctx)
		req.Header.Set("X-Request-ID", reqID)
		ctx = req.Context()
	}

	if t.logger != nil {
		trace := &httptrace.ClientTrace{
			GotConn: func(connInfo httptrace.GotConnInfo) {
				if !connInfo.Reused {
					t.logger.DebugContext(ctx, "HTTP new connection",
						"method", req.Method,
						"url", req.URL.String(),
						"was_idle", connInfo.WasIdle,
						"idle_time", connInfo.IdleTime,
					)
				}
			},
		}
		req = req.WithContext(httptrace.WithClientTrace(ctx, trace))
	}

	return t.next.RoundTrip(req)
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

	traceTransport := &traceRoundTripper{next: transport, logger: cfg.Logger}
	otelTransport := otelhttp.NewTransport(traceTransport)

	if !cfg.EnableRetry {
		return &http.Client{
			Transport: otelTransport,
			Timeout:   cfg.Timeout,
		}
	}

	retryClient := retryablehttp.NewClient()
	retryClient.RetryMax = cfg.RetryMaxAttempts
	retryClient.Backoff = retryablehttp.RateLimitLinearJitterBackoff
	retryClient.CheckRetry = checkRetry
	retryClient.ErrorHandler = retryablehttp.PassthroughErrorHandler
	retryClient.HTTPClient.Transport = otelTransport
	retryClient.Logger = nil // Skip log from httpretry since client already has log transport

	return &http.Client{
		Transport: &retryablehttp.RoundTripper{Client: retryClient},
		Timeout:   cfg.Timeout,
	}
}
