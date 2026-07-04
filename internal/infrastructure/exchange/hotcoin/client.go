package hotcoin

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
	"crypto-bot/pkg/xjson"
)

// Client is the Hotcoin public REST API client for scanner integrations.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	apiSecret  string
	logger     *slog.Logger
}

type hotcoinContract struct {
	TickerID                 string       `json:"tickerId"`
	LastPrice                xjson.Number `json:"lastPrice"`
	NextFundingRate          xjson.Number `json:"nextFundingRate"`
	NextFundingRateTimestamp int64        `json:"nextFundingRateTimestamp"`
	TargetVolume             xjson.Number `json:"targetVolume"`
}

type hotcoinResponse struct {
	Code int               `json:"code"`
	Data []hotcoinContract `json:"data"`
	Msg  string            `json:"msg"`
}

// NewClient creates a new Hotcoin client.
func NewClient(httpClient *http.Client, baseURL, apiKey, apiSecret string, logCfg config.LoggingConfig) *Client {
	logger := slog.Default().With("component", "exchange").With("exchange", "hotcoin")
	if baseURL == "" {
		baseURL = "https://api-ct.hotcoin.fit"
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
		apiKey:     apiKey,
		apiSecret:  apiSecret,
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
		return nil, fmt.Errorf("HTTP error status %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

// GetPotentialFundingSymbols fetches all perpetual contracts, their tickers, and estimated funding rates.
func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	body, err := c.request(ctx, http.MethodGet, "/api/v1/perpetual/public/contracts")
	if err != nil {
		return nil, err
	}

	var resp hotcoinResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal contracts: %w", err)
	}

	if resp.Code != 200 && resp.Msg != "success" {
		return nil, fmt.Errorf("API error: code=%d msg=%s", resp.Code, resp.Msg)
	}

	whitelistMap := make(map[string]bool)
	for _, sym := range whitelist {
		whitelistMap[strings.ToUpper(sym)] = true
	}
	blacklistMap := make(map[string]bool)
	for _, sym := range blacklist {
		blacklistMap[strings.ToUpper(sym)] = true
	}

	var results []exchange.PotentialFundingResult
	for _, item := range resp.Data {
		symbol := strings.ToUpper(item.TickerID)

		if blacklistMap[symbol] {
			continue
		}
		if len(whitelistMap) > 0 && !whitelistMap[symbol] {
			continue
		}

		vol := xjson.ToFloat64(item.TargetVolume)
		if vol < minVol24h {
			continue
		}
		if maxVol24h > 0 && vol > maxVol24h {
			continue
		}

		price := xjson.ToFloat64(item.LastPrice)
		rate := xjson.ToFloat64(item.NextFundingRate)

		results = append(results, exchange.PotentialFundingResult{
			Symbol:     symbol,
			Rate:       rate,
			SettleTime: item.NextFundingRateTimestamp,
			Volume24h:  vol,
			Price:      price,
		})
	}

	return results, nil
}
