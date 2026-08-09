package mandala

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/httpclient"
	"crypto-bot/pkg/xjson"
)

// Client is the Mandala Exchange public REST API client for scanner integrations.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	apiSecret  string
	logger     *slog.Logger
}

type mandalaFuturesInfo struct {
	ContractType    string       `json:"contract_type"`
	FundingRate     xjson.Number `json:"funding_rate"`
	NextFundingTime string       `json:"next_funding_time"`
}

type mandalaTicker struct {
	Last        xjson.Number `json:"last"`
	VolumeQuote xjson.Number `json:"volume_quote"`
}

// NewClient creates a new Mandala client.
func NewClient(httpClient *http.Client, baseURL, apiKey, apiSecret string, logCfg config.LoggingConfig) *Client {
	logger := slog.Default().With("component", "exchange").With("exchange", "mandala")
	if baseURL == "" {
		baseURL = "https://api.wallet.mandala.exchange/api/3/public"
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
	tickers, futuresInfo, err := c.fetchMarketData(ctx)
	if err != nil {
		return nil, err
	}

	return c.filterAndCombine(tickers, futuresInfo, minVol24h, maxVol24h, whitelist, blacklist), nil
}

func (c *Client) fetchMarketData(ctx context.Context) (map[string]mandalaTicker, map[string]mandalaFuturesInfo, error) {
	var (
		tickerData  map[string]mandalaTicker
		futuresData map[string]mandalaFuturesInfo
		errTicker   error
		errFutures  error
		wg          sync.WaitGroup
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		var body []byte
		body, errTicker = c.request(ctx, http.MethodGet, "/ticker")
		if errTicker != nil {
			return
		}
		if err := json.Unmarshal(body, &tickerData); err != nil {
			errTicker = fmt.Errorf("unmarshal tickers: %w", err)
		}
	}()

	go func() {
		defer wg.Done()
		var body []byte
		body, errFutures = c.request(ctx, http.MethodGet, "/futures/info")
		if errFutures != nil {
			return
		}
		if err := json.Unmarshal(body, &futuresData); err != nil {
			errFutures = fmt.Errorf("unmarshal futures info: %w", err)
		}
	}()

	wg.Wait()

	if errTicker != nil {
		return nil, nil, errTicker
	}
	if errFutures != nil {
		return nil, nil, errFutures
	}

	return tickerData, futuresData, nil
}

func (c *Client) filterAndCombine(
	tickers map[string]mandalaTicker,
	futuresInfo map[string]mandalaFuturesInfo,
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
	for symbol, pricing := range futuresInfo {
		if pricing.ContractType != "perpetual" {
			continue
		}

		symbolUpper := strings.ToUpper(symbol)

		if blacklistMap[symbolUpper] {
			continue
		}
		if len(whitelistMap) > 0 && !whitelistMap[symbolUpper] {
			continue
		}

		ticker, found := tickers[symbol]
		if !found {
			continue
		}

		lastPrice := xjson.ToFloat64(ticker.Last)
		volumeUSD := xjson.ToFloat64(ticker.VolumeQuote)

		if volumeUSD < minVol24h {
			continue
		}
		if maxVol24h > 0 && volumeUSD > maxVol24h {
			continue
		}

		rateVal := xjson.ToFloat64(pricing.FundingRate)
		var settleTime int64
		if pricing.NextFundingTime != "" {
			if parsed, err := time.Parse(time.RFC3339, pricing.NextFundingTime); err == nil {
				settleTime = parsed.UnixMilli()
			}
		}

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

func (c *Client) GetTopGainer(_ context.Context, _ exchange.TopGainerRequest) ([]exchange.TopGainerResult, error) {
	return nil, nil
}
