package aster

import (
	"log/slog"
	"net/http"
	"strings"

	"crypto-bot/internal/infrastructure/config"
)

// Client is the Aster REST API client for public market scanning.
type Client struct {
	httpClient *http.Client
	baseURL    string
	logger     *slog.Logger
}

// NewClient creates a new Aster client.
func NewClient(httpClient *http.Client, baseURL string, _ config.LoggingConfig) *Client {
	logger := slog.Default().With("component", "exchange").With("exchange", "aster")

	var finalClient *http.Client
	if httpClient != nil {
		finalClient = httpClient
	} else {
		finalClient = &http.Client{}
	}

	return &Client{
		httpClient: finalClient,
		baseURL:    strings.TrimRight(baseURL, "/"),
		logger:     logger,
	}
}
