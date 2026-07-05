package avantis

import (
	"bytes"
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

// Client is the Avantis public REST API client.
type Client struct {
	httpClient *http.Client
	baseURL    string
	pythURL    string
	logger     *slog.Logger
}

// NewClient creates a new Avantis client.
func NewClient(httpClient *http.Client, baseURL string, logCfg config.LoggingConfig) *Client {
	logger := slog.Default().With("component", "exchange").With("exchange", "avantis")
	if baseURL == "" {
		baseURL = "https://data.avantisfi.com"
	}
	var clientCopy http.Client
	if httpClient != nil {
		clientCopy = *httpClient
	}
	if clientCopy.Transport == nil {
		clientCopy.Transport = http.DefaultTransport
	}
	clientCopy.Transport = httpclient.WrapWithRequestID(clientCopy.Transport)

	trimmedBase := strings.TrimRight(baseURL, "/")
	pythURL := "https://hermes.pyth.network"
	if strings.Contains(trimmedBase, "127.0.0.1") || strings.Contains(trimmedBase, "localhost") {
		pythURL = trimmedBase
	}

	return &Client{
		httpClient: &clientCopy,
		baseURL:    trimmedBase,
		pythURL:    pythURL,
		logger:     logger,
	}
}

func (c *Client) request(ctx context.Context, method, urlStr string, payload any) ([]byte, error) {
	var bodyReader io.Reader
	if payload != nil {
		bodyBytes, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal request payload: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, urlStr, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

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
			Message    string `json:"message"`
			Error      string `json:"error"`
			StatusCode int    `json:"statusCode"`
		}
		var msg string
		if json.Unmarshal(body, &errResp) == nil {
			if errResp.Message != "" {
				msg = errResp.Message
			} else {
				msg = errResp.Error
			}
		} else {
			msg = string(body)
		}
		return nil, &exchange.APIError{
			StatusCode: resp.StatusCode,
			Message:    msg,
			Path:       urlStr,
		}
	}

	return body, nil
}
