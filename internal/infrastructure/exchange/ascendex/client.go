package ascendex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/httpclient"
	"crypto-bot/pkg/xjson"
)

// Client is the AscendEX public REST API client for scanner integrations.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	apiSecret  string
	logger     *slog.Logger
}

type ascendexTicker struct {
	Symbol  string       `json:"symbol"`
	BaseVol xjson.Number `json:"baseVol"`
}

type ascendexTickerResponse struct {
	Code int              `json:"code"`
	Data []ascendexTicker `json:"data"`
}

type ascendexContractPricing struct {
	Symbol          string       `json:"symbol"`
	MarkPrice       xjson.Number `json:"markPrice"`
	IndexPrice      xjson.Number `json:"indexPrice"`
	LastPrice       xjson.Number `json:"lastPrice"`
	FundingRate     xjson.Number `json:"fundingRate"`
	NextFundingTime int64        `json:"nextFundingTime"`
}

type ascendexPricingData struct {
	Contracts []ascendexContractPricing `json:"contracts"`
}

type ascendexPricingResponse struct {
	Code int                 `json:"code"`
	Data ascendexPricingData `json:"data"`
}

// NewClient creates a new AscendEX client.
func NewClient(httpClient *http.Client, baseURL, apiKey, apiSecret string, logCfg config.LoggingConfig) *Client {
	logger := slog.Default().With("component", "exchange").With("exchange", "ascendex")
	if baseURL == "" {
		baseURL = "https://ascendex.com/api/pro/v2"
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
	tickers, pricing, err := c.fetchMarketData(ctx)
	if err != nil {
		return nil, err
	}

	return c.filterAndCombine(tickers, pricing, minVol24h, maxVol24h, whitelist, blacklist), nil
}

func (c *Client) fetchMarketData(ctx context.Context) ([]ascendexTicker, []ascendexContractPricing, error) {
	var (
		tickerData  []ascendexTicker
		pricingData []ascendexContractPricing
		errTicker   error
		errPricing  error
		wg          sync.WaitGroup
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		var body []byte
		body, errTicker = c.request(ctx, http.MethodGet, "/futures/ticker")
		if errTicker != nil {
			return
		}
		var resp ascendexTickerResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			errTicker = fmt.Errorf("unmarshal tickers: %w", err)
			return
		}
		if resp.Code != 0 {
			errTicker = fmt.Errorf("API error code %d", resp.Code)
			return
		}
		tickerData = resp.Data
	}()

	go func() {
		defer wg.Done()
		var body []byte
		body, errPricing = c.request(ctx, http.MethodGet, "/futures/pricing-data")
		if errPricing != nil {
			return
		}
		var resp ascendexPricingResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			errPricing = fmt.Errorf("unmarshal pricing-data: %w", err)
			return
		}
		if resp.Code != 0 {
			errPricing = fmt.Errorf("API error code %d", resp.Code)
			return
		}
		pricingData = resp.Data.Contracts
	}()

	wg.Wait()

	if errTicker != nil {
		return nil, nil, errTicker
	}
	if errPricing != nil {
		return nil, nil, errPricing
	}

	return tickerData, pricingData, nil
}

func (c *Client) filterAndCombine(
	tickerData []ascendexTicker,
	pricingData []ascendexContractPricing,
	minVol24h, maxVol24h float64,
	whitelist, blacklist []string,
) []exchange.PotentialFundingResult {
	tickerMap := make(map[string]ascendexTicker)
	for _, item := range tickerData {
		tickerMap[strings.ToUpper(item.Symbol)] = item
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
	for _, pricing := range pricingData {
		symbolUpper := strings.ToUpper(pricing.Symbol)

		if blacklistMap[symbolUpper] {
			continue
		}
		if len(whitelistMap) > 0 && !whitelistMap[symbolUpper] {
			continue
		}

		ticker, found := tickerMap[symbolUpper]
		if !found {
			continue
		}

		lastPrice := xjson.ToFloat64(pricing.LastPrice)
		baseVol := xjson.ToFloat64(ticker.BaseVol)
		volumeUSDT := baseVol * lastPrice

		if volumeUSDT < minVol24h {
			continue
		}
		if maxVol24h > 0 && volumeUSDT > maxVol24h {
			continue
		}

		rateVal := xjson.ToFloat64(pricing.FundingRate)

		results = append(results, exchange.PotentialFundingResult{
			Symbol:     symbolUpper,
			Rate:       rateVal,
			SettleTime: pricing.NextFundingTime,
			Volume24h:  volumeUSDT,
			Price:      lastPrice,
		})
	}

	return results
}
