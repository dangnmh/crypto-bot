package btse

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

// Client is the BTSE public REST API client for scanner integrations.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	apiSecret  string
	logger     *slog.Logger
}

type btseMarketSummary struct {
	Symbol       string       `json:"symbol"`
	Last         xjson.Number `json:"last"`
	Volume       xjson.Number `json:"volume"`
	FundingRate  xjson.Number `json:"fundingRate"`
	FundingTime  xjson.Number `json:"fundingTime"`
	ContractSize xjson.Number `json:"contractSize"`
	Active       bool         `json:"active"`
}

// NewClient creates a new BTSE client.
func NewClient(httpClient *http.Client, baseURL, apiKey, apiSecret string, logCfg config.LoggingConfig) *Client {
	logger := slog.Default().With("component", "exchange").With("exchange", "btse")
	if baseURL == "" {
		baseURL = "https://api.btse.com/futures/api/v2.1"
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
	body, err := c.request(ctx, http.MethodGet, "/market_summary")
	if err != nil {
		return nil, err
	}

	var marketSummaries []btseMarketSummary
	if err := json.Unmarshal(body, &marketSummaries); err != nil {
		return nil, fmt.Errorf("unmarshal market_summary: %w", err)
	}

	return c.filterAndCombine(marketSummaries, minVol24h, maxVol24h, whitelist, blacklist), nil
}

func (c *Client) filterAndCombine(
	summaries []btseMarketSummary,
	minVol24h, maxVol24h float64,
	whitelist, blacklist []string,
) []exchange.PotentialFundingResult {
	whitelistMap := make(map[string]bool)
	for _, sym := range whitelist {
		whitelistMap[strings.ToUpper(sym)] = true
	}
	blacklistMap := make(map[string]bool)
	for _, sym := range blacklist {
		blacklistMap[strings.ToUpper(sym)] = true
	}

	var results []exchange.PotentialFundingResult
	for _, item := range summaries {
		if !item.Active {
			continue
		}

		symbolUpper := strings.ToUpper(item.Symbol)

		if blacklistMap[symbolUpper] {
			continue
		}
		if len(whitelistMap) > 0 && !whitelistMap[symbolUpper] {
			continue
		}

		lastPrice := xjson.ToFloat64(item.Last)
		contractSize := xjson.ToFloat64(item.ContractSize)
		volumeContracts := xjson.ToFloat64(item.Volume)

		// BTSE volume is in contract units. Calculate volume in USD/USDT
		volumeUSD := volumeContracts * contractSize * lastPrice

		if volumeUSD < minVol24h {
			continue
		}
		if maxVol24h > 0 && volumeUSD > maxVol24h {
			continue
		}

		rateVal := xjson.ToFloat64(item.FundingRate)
		settleTime := int64(xjson.ToFloat64(item.FundingTime))

		results = append(results, exchange.PotentialFundingResult{
			Symbol:     symbolUpper,
			Rate:       rateVal,
			SettleTime: settleTime,
			Volume24h:  volumeUSD,
			Price:      lastPrice,
		})
	}

	return results
}
