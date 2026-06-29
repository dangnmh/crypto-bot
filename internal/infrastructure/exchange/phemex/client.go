package phemex

import (
	"log/slog"
	"net/http"
)

type Client struct {
	httpClient *http.Client
	baseURL    string
	logger     *slog.Logger
}

func NewClient(httpClient *http.Client, baseURL string, logger *slog.Logger) *Client {
	return &Client{
		httpClient: httpClient,
		baseURL:    baseURL,
		logger:     logger.With("exchange", "phemex"),
	}
}
