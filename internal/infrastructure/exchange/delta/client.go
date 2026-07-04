package delta

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/httpclient"
	"crypto-bot/pkg/xjson"
)

// Client is the Delta Exchange public REST API client for scanner integrations.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	apiSecret  string
	logger     *slog.Logger
}

type deltaTicker struct {
	Symbol       string       `json:"symbol"`
	MarkPrice    xjson.Number `json:"mark_price"`
	TurnoverUSD  xjson.Number `json:"turnover_usd"`
	FundingRate  xjson.Number `json:"funding_rate"`
	ContractType string       `json:"contract_type"` // e.g. "perpetual_futures"
	Timestamp    int64        `json:"timestamp"`     // Microseconds
}

type deltaTickersResponse struct {
	Success bool          `json:"success"`
	Result  []deltaTicker `json:"result"`
}

// NewClient creates a new Delta Exchange client.
func NewClient(httpClient *http.Client, baseURL, apiKey, apiSecret string, logCfg config.LoggingConfig) *Client {
	logger := slog.Default().With("component", "exchange").With("exchange", "delta")
	if baseURL == "" {
		baseURL = "https://api.delta.exchange/v2"
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
	body, err := c.request(ctx, http.MethodGet, "/tickers?contract_types=perpetual_futures")
	if err != nil {
		return nil, err
	}

	var resp deltaTickersResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal tickers: %w", err)
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
	for _, ticker := range resp.Result {
		// Delta perpetual contracts have contract_type == "perpetual_futures"
		if ticker.ContractType != "perpetual_futures" {
			continue
		}

		symbolUpper := strings.ToUpper(ticker.Symbol)

		if blacklistMap[symbolUpper] {
			continue
		}
		if len(whitelistMap) > 0 && !whitelistMap[symbolUpper] {
			continue
		}

		lastPrice := xjson.ToFloat64(ticker.MarkPrice)
		volumeUSD := xjson.ToFloat64(ticker.TurnoverUSD)

		if volumeUSD < minVol24h {
			continue
		}
		if maxVol24h > 0 && volumeUSD > maxVol24h {
			continue
		}

		rateVal := xjson.ToFloat64(ticker.FundingRate)
		settleTime := getNextFundingTime(ticker.Timestamp)

		results = append(results, exchange.PotentialFundingResult{
			Symbol:     symbolUpper,
			Rate:       rateVal,
			SettleTime: settleTime,
			Volume24h:  volumeUSD,
			Price:      lastPrice,
		})
	}

	return results, nil
}

func getNextFundingTime(timestampUs int64) int64 {
	secs := timestampUs / 1000000
	if secs == 0 {
		secs = time.Now().Unix()
	}
	// Round up to the next 8-hour interval in UTC
	interval := int64(8 * 3600)
	nextSecs := ((secs / interval) + 1) * interval
	return nextSecs * 1000
}
