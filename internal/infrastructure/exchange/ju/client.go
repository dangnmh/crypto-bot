package ju

import (
	"log/slog"
	"net/http"
	"strings"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/pkg/ratelimit"

	"golang.org/x/time/rate"
)

// Client is the Jucoin REST API client for public market scanning.
type Client struct {
	httpClient *http.Client
	baseURL    string
	logger     *slog.Logger
	limiter    *ratelimit.ExchangeRateLimiter
}

// NewClient creates a new Jucoin client.
func NewClient(httpClient *http.Client, baseURL string, _ config.LoggingConfig) *Client {
	logger := slog.Default().With("component", "exchange").With("exchange", "ju")

	var finalClient *http.Client
	if httpClient != nil {
		finalClient = httpClient
	} else {
		finalClient = &http.Client{}
	}

	limiter := ratelimit.NewExchangeRateLimiter(rate.Limit(10), 2, nil)

	return &Client{
		httpClient: finalClient,
		baseURL:    strings.TrimRight(baseURL, "/"),
		logger:     logger,
		limiter:    limiter,
	}
}
