package bydfi

import (
	"log/slog"
	"net/http"

	"crypto-bot/pkg/ratelimit"

	"golang.org/x/time/rate"
)

type Client struct {
	httpClient *http.Client
	baseURL    string
	logger     *slog.Logger
	limiter    *ratelimit.ExchangeRateLimiter
}

func NewClient(httpClient *http.Client, baseURL string, logger *slog.Logger) *Client {
	limiter := ratelimit.NewExchangeRateLimiter(rate.Limit(10), 2, nil)

	return &Client{
		httpClient: httpClient,
		baseURL:    baseURL,
		logger:     logger.With("exchange", "bydfi"),
		limiter:    limiter,
	}
}

const symbolKey = "symbol"
