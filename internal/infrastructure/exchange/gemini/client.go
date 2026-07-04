package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/time/rate"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/httpclient"
	"crypto-bot/pkg/ratelimit"
	"crypto-bot/pkg/xjson"
)

// Client is the Gemini public REST API client for scanner integrations.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	apiSecret  string
	logger     *slog.Logger
	limiter    *ratelimit.ExchangeRateLimiter
}

type geminiSymbolDetail struct {
	Symbol        string `json:"symbol"`
	QuoteCurrency string `json:"quote_currency"`
	Status        string `json:"status"`
	ProductType   string `json:"product_type"`
}

type geminiPubTicker struct {
	Bid    string                  `json:"bid"`
	Ask    string                  `json:"ask"`
	Last   string                  `json:"last"`
	Volume map[string]xjson.Number `json:"volume"`
}

type geminiFundingAmount struct {
	Symbol                 string  `json:"symbol"`
	NextFundingTimestamp   int64   `json:"nextFundingTimestamp"`
	EstimatedFundingAmount float64 `json:"estimatedFundingAmount"`
}

// NewClient creates a new Gemini client.
func NewClient(httpClient *http.Client, baseURL, apiKey, apiSecret string, logCfg config.LoggingConfig) *Client {
	logger := slog.Default().With("component", "exchange").With("exchange", "gemini")
	var clientCopy http.Client
	if httpClient != nil {
		clientCopy = *httpClient
	}
	if clientCopy.Transport == nil {
		clientCopy.Transport = http.DefaultTransport
	}
	clientCopy.Transport = httpclient.WrapWithRequestID(clientCopy.Transport)

	// Configure limits: Global limit of 3 req/s.
	limiter := ratelimit.NewExchangeRateLimiter(rate.Limit(3), 2, nil)

	return &Client{
		httpClient: &clientCopy,
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		apiSecret:  apiSecret,
		logger:     logger,
		limiter:    limiter,
	}
}

func (c *Client) request(ctx context.Context, method, path string) ([]byte, error) {
	if c.limiter != nil {
		if err := c.limiter.Acquire(ctx, path); err != nil {
			return nil, fmt.Errorf("rate limit: %w", err)
		}
	}

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

// fetchSingleSymbolFunding retrieves the details, ticker, and funding rate details for a single symbol.
func (c *Client) fetchSingleSymbolFunding(
	ctx context.Context,
	symbol string,
	minVol24h, maxVol24h float64,
) (exchange.PotentialFundingResult, error) {
	// 1. Fetch details
	detailsBody, err := c.request(ctx, http.MethodGet, fmt.Sprintf("/v1/symbols/details/%s", symbol))
	if err != nil {
		return exchange.PotentialFundingResult{}, fmt.Errorf("details for %s: %w", symbol, err)
	}
	var details geminiSymbolDetail
	if err := json.Unmarshal(detailsBody, &details); err != nil {
		return exchange.PotentialFundingResult{}, fmt.Errorf("unmarshal details for %s: %w", symbol, err)
	}

	if !strings.EqualFold(details.Status, "open") {
		return exchange.PotentialFundingResult{}, nil
	}

	// 2. Fetch ticker
	tickerBody, err := c.request(ctx, http.MethodGet, fmt.Sprintf("/v1/pubticker/%s", symbol))
	if err != nil {
		return exchange.PotentialFundingResult{}, fmt.Errorf("pubticker for %s: %w", symbol, err)
	}
	var ticker geminiPubTicker
	if err := json.Unmarshal(tickerBody, &ticker); err != nil {
		return exchange.PotentialFundingResult{}, fmt.Errorf("unmarshal ticker for %s: %w", symbol, err)
	}

	// 3. Fetch funding
	fundingBody, err := c.request(ctx, http.MethodGet, fmt.Sprintf("/v1/fundingamount/%s", symbol))
	if err != nil {
		return exchange.PotentialFundingResult{}, fmt.Errorf("funding for %s: %w", symbol, err)
	}
	var funding geminiFundingAmount
	if err := json.Unmarshal(fundingBody, &funding); err != nil {
		return exchange.PotentialFundingResult{}, fmt.Errorf("unmarshal funding for %s: %w", symbol, err)
	}

	lastPrice, _ := strconv.ParseFloat(ticker.Last, 64)
	if lastPrice <= 0 {
		return exchange.PotentialFundingResult{}, nil
	}

	fundingRate := funding.EstimatedFundingAmount / lastPrice

	quoteVol := parseTickerVolume(ticker.Volume, details.QuoteCurrency)

	if quoteVol < minVol24h {
		return exchange.PotentialFundingResult{}, nil
	}
	if maxVol24h > 0 && quoteVol > maxVol24h {
		return exchange.PotentialFundingResult{}, nil
	}

	return exchange.PotentialFundingResult{
		Symbol:     strings.ToUpper(symbol),
		Rate:       fundingRate,
		SettleTime: funding.NextFundingTimestamp,
		Volume24h:  quoteVol,
		Price:      lastPrice,
	}, nil
}

func parseTickerVolume(volume map[string]xjson.Number, quoteCurrency string) float64 {
	if volume == nil {
		return 0.0
	}
	volVal, ok := volume[quoteCurrency]
	if !ok {
		return 0.0
	}
	return xjson.ToFloat64(volVal)
}

// GetPotentialFundingSymbols fetches all perpetual contracts, their tickers, and estimated funding rates.
func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	body, err := c.request(ctx, http.MethodGet, "/v1/symbols")
	if err != nil {
		return nil, err
	}
	var symbols []string
	if err := json.Unmarshal(body, &symbols); err != nil {
		return nil, fmt.Errorf("unmarshal symbols: %w", err)
	}

	var perpSymbols []string
	for _, sym := range symbols {
		if strings.HasSuffix(strings.ToLower(sym), "perp") {
			perpSymbols = append(perpSymbols, sym)
		}
	}

	whitelistMap := make(map[string]bool)
	for _, sym := range whitelist {
		whitelistMap[strings.ToUpper(sym)] = true
	}
	blacklistMap := make(map[string]bool)
	for _, sym := range blacklist {
		blacklistMap[strings.ToUpper(sym)] = true
	}

	var filteredSymbols []string
	for _, sym := range perpSymbols {
		upperSym := strings.ToUpper(sym)
		if blacklistMap[upperSym] {
			continue
		}
		if len(whitelistMap) > 0 && !whitelistMap[upperSym] {
			continue
		}
		filteredSymbols = append(filteredSymbols, sym)
	}

	type fetchResult struct {
		res exchange.PotentialFundingResult
		err error
	}

	resultsChan := make(chan fetchResult, len(filteredSymbols))
	var wg sync.WaitGroup

	for _, sym := range filteredSymbols {
		wg.Add(1)
		go func(symbol string) {
			defer wg.Done()
			res, err := c.fetchSingleSymbolFunding(ctx, symbol, minVol24h, maxVol24h)
			resultsChan <- fetchResult{res: res, err: err}
		}(sym)
	}

	wg.Wait()
	close(resultsChan)

	var finalResults []exchange.PotentialFundingResult
	for r := range resultsChan {
		if r.err != nil {
			c.logger.Error("failed to fetch gemini symbol data", "error", r.err)
			continue
		}
		if r.res.Symbol != "" {
			finalResults = append(finalResults, r.res)
		}
	}

	return finalResults, nil
}
