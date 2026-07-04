package poloniex

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

// Client is the Poloniex Exchange public REST API client for scanner integrations.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	apiSecret  string
	logger     *slog.Logger
	limiter    *ratelimit.ExchangeRateLimiter
}

type poloniexInstrument struct {
	Symbol string `json:"symbol"`
	CtType string `json:"ctType"` // e.g. "LINEAR"
}

type poloniexInstrumentsResponse struct {
	Code int                  `json:"code"`
	Msg  string               `json:"msg"`
	Data []poloniexInstrument `json:"data"`
}

type poloniexTicker struct {
	S   string       `json:"s"`   // Symbol
	C   xjson.Number `json:"c"`   // Close / Last price
	Amt xjson.Number `json:"amt"` // Quote volume (USDT)
}

type poloniexTickersResponse struct {
	Code int              `json:"code"`
	Msg  string           `json:"msg"`
	Data []poloniexTicker `json:"data"`
}

type poloniexFundingRateData struct {
	S   string       `json:"s"`   // Symbol
	FR  xjson.Number `json:"fR"`  // Current funding rate
	FT  string       `json:"fT"`  // Current funding time (string-encoded int64)
	NFR xjson.Number `json:"nFR"` // Next funding rate
	NFT string       `json:"nFT"` // Next funding time (string-encoded int64)
}

type poloniexFundingRateResponse struct {
	Code int                     `json:"code"`
	Msg  string                  `json:"msg"`
	Data poloniexFundingRateData `json:"data"`
}

// NewClient creates a new Poloniex client.
func NewClient(httpClient *http.Client, baseURL, apiKey, apiSecret string, logCfg config.LoggingConfig) *Client {
	logger := slog.Default().With("component", "exchange").With("exchange", "poloniex")
	if baseURL == "" {
		baseURL = "https://api.poloniex.com/v3"
	}
	var clientCopy http.Client
	if httpClient != nil {
		clientCopy = *httpClient
	}
	if clientCopy.Transport == nil {
		clientCopy.Transport = http.DefaultTransport
	}
	clientCopy.Transport = httpclient.WrapWithRequestID(clientCopy.Transport)

	// Configure a global limit of 10 requests per second to stay well within Poloniex limits.
	limiter := ratelimit.NewExchangeRateLimiter(rate.Limit(10), 2, nil)

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
		// Acquire blocks to throttle request pacing.
		// Strip query parameters so resolveConfig matches /market/fundingRate pattern.
		cleanPath := path
		if before, _, ok := strings.Cut(path, "?"); ok {
			cleanPath = before
		}
		if err := c.limiter.Acquire(ctx, cleanPath); err != nil {
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

// GetPotentialFundingSymbols fetches all perpetual contracts, their tickers, and estimated funding rates.
func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	tickers, instruments, err := c.fetchMarketData(ctx)
	if err != nil {
		return nil, err
	}

	// Filter perpetual instruments
	var perpInstruments []poloniexInstrument
	whitelistMap := make(map[string]bool)
	for _, sym := range whitelist {
		whitelistMap[strings.ToUpper(sym)] = true
	}
	blacklistMap := make(map[string]bool)
	for _, sym := range blacklist {
		blacklistMap[strings.ToUpper(sym)] = true
	}

	for _, inst := range instruments {
		// Poloniex perpetual contracts have ctType == "LINEAR" and end with _PERP
		if inst.CtType != "LINEAR" || !strings.HasSuffix(inst.Symbol, "_PERP") {
			continue
		}

		symbolUpper := strings.ToUpper(inst.Symbol)
		if blacklistMap[symbolUpper] {
			continue
		}
		if len(whitelistMap) > 0 && !whitelistMap[symbolUpper] {
			continue
		}

		perpInstruments = append(perpInstruments, inst)
	}

	// Fetch funding rates for each perp instrument concurrently
	fundingRates := make(map[string]*poloniexFundingRateData)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, inst := range perpInstruments {
		wg.Add(1)
		go func(symbol string) {
			defer wg.Done()
			data, err := c.fetchFundingRate(ctx, symbol)
			if err != nil {
				c.logger.Warn("Failed to fetch funding rate for symbol", "symbol", symbol, "error", err)
				return
			}
			mu.Lock()
			fundingRates[symbol] = data
			mu.Unlock()
		}(inst.Symbol)
	}

	wg.Wait()

	return c.combineResults(tickers, fundingRates, minVol24h, maxVol24h), nil
}

func (c *Client) fetchFundingRate(ctx context.Context, symbol string) (*poloniexFundingRateData, error) {
	body, err := c.request(ctx, http.MethodGet, "/market/fundingRate?symbol="+symbol)
	if err != nil {
		return nil, err
	}

	var resp poloniexFundingRateResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal funding rate: %w", err)
	}

	return &resp.Data, nil
}

func (c *Client) fetchMarketData(ctx context.Context) (map[string]poloniexTicker, []poloniexInstrument, error) {
	var (
		tickerMap      map[string]poloniexTicker
		instrumentList []poloniexInstrument
		errTicker      error
		errInstrument  error
		wg             sync.WaitGroup
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		var body []byte
		body, errTicker = c.request(ctx, http.MethodGet, "/market/tickers")
		if errTicker != nil {
			return
		}
		var resp poloniexTickersResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			errTicker = fmt.Errorf("unmarshal tickers: %w", err)
			return
		}
		tickerMap = make(map[string]poloniexTicker)
		for _, t := range resp.Data {
			tickerMap[t.S] = t
		}
	}()

	go func() {
		defer wg.Done()
		var body []byte
		body, errInstrument = c.request(ctx, http.MethodGet, "/market/allInstruments")
		if errInstrument != nil {
			return
		}
		var resp poloniexInstrumentsResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			errInstrument = fmt.Errorf("unmarshal instruments: %w", err)
			return
		}
		instrumentList = resp.Data
	}()

	wg.Wait()

	if errTicker != nil {
		return nil, nil, errTicker
	}
	if errInstrument != nil {
		return nil, nil, errInstrument
	}

	return tickerMap, instrumentList, nil
}

func (c *Client) combineResults(
	tickers map[string]poloniexTicker,
	fundingRates map[string]*poloniexFundingRateData,
	minVol24h, maxVol24h float64,
) []exchange.PotentialFundingResult {
	var results []exchange.PotentialFundingResult

	for symbol, rateData := range fundingRates {
		ticker, found := tickers[symbol]
		if !found {
			continue
		}

		lastPrice := xjson.ToFloat64(ticker.C)
		volumeUSD := xjson.ToFloat64(ticker.Amt)

		if volumeUSD < minVol24h {
			continue
		}
		if maxVol24h > 0 && volumeUSD > maxVol24h {
			continue
		}

		// Parse strings to int64 for settle timestamps
		rateVal := xjson.ToFloat64(rateData.NFR)
		timeStr := rateData.NFT
		if rateVal == 0 && timeStr == "" {
			rateVal = xjson.ToFloat64(rateData.FR)
			timeStr = rateData.FT
		}

		var settleTime int64
		if timeStr != "" {
			if parsed, err := strconv.ParseInt(timeStr, 10, 64); err == nil {
				settleTime = parsed
			}
		}

		results = append(results, exchange.PotentialFundingResult{
			Symbol:     strings.ToUpper(symbol),
			Rate:       rateVal,
			SettleTime: settleTime,
			Volume24h:  volumeUSD,
			Price:      lastPrice,
		})
	}

	return results
}
