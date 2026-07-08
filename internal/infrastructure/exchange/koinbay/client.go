package koinbay

import (
	"log/slog"
	"net/http"
	"strings"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/pkg/httpclient"
	"crypto-bot/pkg/ratelimit"

	"golang.org/x/time/rate"
)

type Client struct {
	httpClient *http.Client
	baseURL    string
	logger     *slog.Logger
	limiter    *ratelimit.ExchangeRateLimiter
}

func NewClient(httpClient *http.Client, baseURL string, logCfg config.LoggingConfig) *Client {
	logger := slog.Default().With("component", "exchange").With("exchange", "koinbay")
	if baseURL == "" {
		baseURL = "https://futuresopenapi.koinbay.com"
	}
	var clientCopy http.Client
	if httpClient != nil {
		clientCopy = *httpClient
	}
	if clientCopy.Transport == nil {
		clientCopy.Transport = http.DefaultTransport
	}
	clientCopy.Transport = httpclient.WrapWithRequestID(clientCopy.Transport)

	// Configure limits: 10 requests per second.
	limiter := ratelimit.NewExchangeRateLimiter(rate.Limit(10), 2, nil)

	return &Client{
		httpClient: &clientCopy,
		baseURL:    strings.TrimRight(baseURL, "/"),
		logger:     logger,
		limiter:    limiter,
	}
}
