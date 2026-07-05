package lighter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/httpclient"
)

// Client is the Lighter public REST API client for scanner integrations.
type Client struct {
	httpClient *http.Client
	baseURL    string
	logger     *slog.Logger
}

// NewClient creates a new Lighter client.
func NewClient(httpClient *http.Client, baseURL string, logCfg config.LoggingConfig) *Client {
	logger := slog.Default().With("component", "exchange").With("exchange", "lighter")
	if baseURL == "" {
		baseURL = "https://mainnet.zklighter.elliot.ai"
	}
	var clientCopy http.Client
	if httpClient != nil {
		clientCopy = *httpClient
	}
	if clientCopy.Transport == nil {
		clientCopy.Transport = http.DefaultTransport
	}
	clientCopy.Transport = httpclient.WrapWithRequestID(clientCopy.Transport)

	return &Client{
		httpClient: &clientCopy,
		baseURL:    strings.TrimRight(baseURL, "/"),
		logger:     logger,
	}
}

func (c *Client) request(ctx context.Context, method, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		var code int
		var msg string
		if json.Unmarshal(body, &errResp) == nil {
			code = errResp.Code
			msg = errResp.Message
		} else {
			msg = string(body)
		}
		return nil, &exchange.APIError{
			StatusCode: resp.StatusCode,
			Code:       code,
			Message:    msg,
			Path:       path,
		}
	}

	return body, nil
}
