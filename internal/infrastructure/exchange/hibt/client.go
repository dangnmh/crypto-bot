package hibt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"golang.org/x/time/rate"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/httpclient"
	"crypto-bot/pkg/ratelimit"
	"crypto-bot/pkg/xjson"
)

// Client is the Hibt public REST API client for scanner integrations.
type Client struct {
	httpClient *http.Client
	baseURL    string
	logger     *slog.Logger
	limiter    *ratelimit.ExchangeRateLimiter
}

type hibtTicker struct {
	Symbol    string       `json:"symbol"`
	Volume    xjson.Number `json:"volume"`
	LastPrice xjson.Number `json:"lastPrice"`
}

type hibtTickersResponse struct {
	Code int          `json:"code"`
	Msg  string       `json:"msg"`
	Data []hibtTicker `json:"data"`
}

type hibtFundingRate struct {
	Symbol string       `json:"symbol"`
	Rate   xjson.Number `json:"rate"`
	Time   int64        `json:"time"`
}

type hibtFundingResponse struct {
	Code int               `json:"code"`
	Msg  string            `json:"msg"`
	Data []hibtFundingRate `json:"data"`
}

// NewClient creates a new Hibt client.
func NewClient(httpClient *http.Client, baseURL string, logCfg config.LoggingConfig) *Client {
	logger := slog.Default().With("component", "exchange").With("exchange", "hibt")
	if baseURL == "" {
		baseURL = "https://fapi.hibt0.com/open-api"
	}
	var clientCopy http.Client
	if httpClient != nil {
		clientCopy = *httpClient
	}
	if clientCopy.Transport == nil {
		clientCopy.Transport = http.DefaultTransport
	}
	clientCopy.Transport = httpclient.WrapWithRequestID(clientCopy.Transport)

	// Configure limits: Global limit of 3 req/s.
	// Endpoint limit for /v2/market/fundingRate is 2 req/s.
	configs := map[string]ratelimit.EndpointConfig{
		"/v2/market/fundingRate": {Limit: rate.Limit(2), Burst: 1, Weight: 1},
	}
	limiter := ratelimit.NewExchangeRateLimiter(rate.Limit(3), 2, configs)

	return &Client{
		httpClient: &clientCopy,
		baseURL:    strings.TrimRight(baseURL, "/"),
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
		var errResp struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		var code int
		var msg string
		if json.Unmarshal(body, &errResp) == nil {
			code = errResp.Code
			msg = errResp.Msg
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

// GetPotentialFundingSymbols fetches all perpetual contracts, their tickers, and estimated funding rates.
func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	body, err := c.request(ctx, http.MethodGet, "/v2/market/tickers")
	if err != nil {
		return nil, err
	}

	var resp hibtTickersResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal tickers: %w", err)
	}

	if resp.Code != 0 {
		return nil, fmt.Errorf("API error: code=%d msg=%s", resp.Code, resp.Msg)
	}

	filteredTickers := c.filterTickers(resp.Data, minVol24h, maxVol24h, whitelist, blacklist)

	type fetchResult struct {
		res exchange.PotentialFundingResult
		err error
	}

	resultsChan := make(chan fetchResult, len(filteredTickers))
	var wg sync.WaitGroup

	for _, ticker := range filteredTickers {
		wg.Add(1)
		go func(t hibtTicker) {
			defer wg.Done()
			res, err := c.fetchSingleSymbolFunding(ctx, t)
			resultsChan <- fetchResult{res: res, err: err}
		}(ticker)
	}

	wg.Wait()
	close(resultsChan)

	var finalResults []exchange.PotentialFundingResult
	for r := range resultsChan {
		if r.err != nil {
			c.logger.Debug("failed to fetch hibt funding rate", "error", r.err)
			continue
		}
		if r.res.Symbol != "" {
			finalResults = append(finalResults, r.res)
		}
	}

	return finalResults, nil
}

func (c *Client) filterTickers(
	tickers []hibtTicker,
	minVol24h, maxVol24h float64,
	whitelist, blacklist []string,
) []hibtTicker {
	whitelistMap := make(map[string]bool)
	for _, sym := range whitelist {
		whitelistMap[strings.ToUpper(sym)] = true
	}
	blacklistMap := make(map[string]bool)
	for _, sym := range blacklist {
		blacklistMap[strings.ToUpper(sym)] = true
	}

	var filtered []hibtTicker
	for _, item := range tickers {
		symbolUpper := strings.ToUpper(item.Symbol)

		if blacklistMap[symbolUpper] {
			continue
		}
		if len(whitelistMap) > 0 && !whitelistMap[symbolUpper] {
			continue
		}

		vol := xjson.ToFloat64(item.Volume)
		if vol < minVol24h {
			continue
		}
		if maxVol24h > 0 && vol > maxVol24h {
			continue
		}

		filtered = append(filtered, item)
	}
	return filtered
}

func (c *Client) fetchSingleSymbolFunding(ctx context.Context, ticker hibtTicker) (exchange.PotentialFundingResult, error) {
	path := fmt.Sprintf("/v2/market/fundingRate?symbol=%s", ticker.Symbol)
	body, err := c.request(ctx, http.MethodGet, path)
	if err != nil {
		if apiErr, ok := errors.AsType[*exchange.APIError](err); ok {
			if apiErr.StatusCode == http.StatusBadRequest && apiErr.Code == 210001 {
				// Param error for this symbol from Hibt perp fundingRate endpoint means it is unsupported.
				// We return an empty result and no error to skip it.
				return exchange.PotentialFundingResult{}, nil
			}
		}
		return exchange.PotentialFundingResult{}, fmt.Errorf("fetch funding rate for %s: %w", ticker.Symbol, err)
	}

	var resp hibtFundingResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return exchange.PotentialFundingResult{}, fmt.Errorf("unmarshal funding for %s: %w", ticker.Symbol, err)
	}

	if resp.Code != 0 {
		return exchange.PotentialFundingResult{}, fmt.Errorf("API error: code=%d msg=%s", resp.Code, resp.Msg)
	}

	if len(resp.Data) == 0 {
		return exchange.PotentialFundingResult{}, nil
	}

	latest := resp.Data[0]
	rateVal := xjson.ToFloat64(latest.Rate)
	priceVal := xjson.ToFloat64(ticker.LastPrice)
	volVal := xjson.ToFloat64(ticker.Volume)

	return exchange.PotentialFundingResult{
		Symbol:     strings.ToUpper(ticker.Symbol),
		Rate:       rateVal,
		SettleTime: latest.Time,
		Volume24h:  volVal,
		Price:      priceVal,
	}, nil
}

func (c *Client) GetTopGainer(_ context.Context, _ exchange.TopGainerRequest) ([]exchange.TopGainerResult, error) {
	return nil, nil
}
