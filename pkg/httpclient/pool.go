package httpclient

import (
	"net/http"
	"time"
)

// PoolConfig defines the configuration for a connection pool.
type PoolConfig struct {
	MaxIdleConns        int
	MaxIdleConnsPerHost int
	IdleConnTimeout     time.Duration
	TLSHandshakeTimeout time.Duration
	DisableCompression  bool
	Timeout             time.Duration
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
	}
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

	return &http.Client{
		Transport: transport,
		Timeout:   cfg.Timeout,
	}
}
