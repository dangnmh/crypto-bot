package koinbay

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

const paramContractName = "contractName"

type koinbayContract struct {
	Symbol string `json:"symbol"`
	Status int    `json:"status"` // 1 = active
}

type koinbayTicker struct {
	Vol  xjson.Number `json:"vol"`  // 24h volume
	Last xjson.Number `json:"last"` // last price
}

type koinbayIndex struct {
	CurrentFundRate xjson.Number `json:"currentFundRate"`
}

func (c *Client) request(ctx context.Context, path string, query map[string]string) ([]byte, error) {
	if err := c.limiter.Acquire(ctx, path); err != nil {
		return nil, fmt.Errorf("rate limit acquire: %w", err)
	}

	reqURL, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}

	if len(query) > 0 {
		q := reqURL.Query()
		for k, v := range query {
			q.Set(k, v)
		}
		reqURL.RawQuery = q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP GET %s: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

func toStandardSymbol(sym string) string {
	s := strings.ToUpper(sym)
	s = strings.TrimPrefix(s, "E-")
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "_", "")
	return s
}

func getNextFundingTime() int64 {
	now := time.Now().UTC()
	hour := now.Hour()
	var nextHour int
	switch {
	case hour < 8:
		nextHour = 8
	case hour < 16:
		nextHour = 16
	default:
		nextHour = 24
	}
	nextSettle := time.Date(now.Year(), now.Month(), now.Day(), nextHour, 0, 0, 0, time.UTC)
	return nextSettle.UnixMilli()
}

func (c *Client) fetchSingleSymbol(
	ctx context.Context,
	rawSymbol string,
	stdSym string,
	minVol24h, maxVol24h float64,
) (exchange.PotentialFundingResult, error) {
	// 1. Fetch ticker
	tickerBody, err := c.request(ctx, "/fapi/v1/ticker", map[string]string{paramContractName: rawSymbol})
	if err != nil {
		return exchange.PotentialFundingResult{}, fmt.Errorf("ticker for %s: %w", rawSymbol, err)
	}

	var ticker koinbayTicker
	if err := json.Unmarshal(tickerBody, &ticker); err != nil {
		return exchange.PotentialFundingResult{}, fmt.Errorf("unmarshal ticker for %s: %w", rawSymbol, err)
	}

	vol, _ := ticker.Vol.Float64()
	if minVol24h > 0 && vol < minVol24h {
		return exchange.PotentialFundingResult{}, nil
	}
	if maxVol24h > 0 && vol > maxVol24h {
		return exchange.PotentialFundingResult{}, nil
	}

	// 2. Fetch index details (funding rate)
	indexBody, err := c.request(ctx, "/fapi/v1/index", map[string]string{paramContractName: rawSymbol})
	if err != nil {
		return exchange.PotentialFundingResult{}, fmt.Errorf("index for %s: %w", rawSymbol, err)
	}

	var index koinbayIndex
	if err := json.Unmarshal(indexBody, &index); err != nil {
		return exchange.PotentialFundingResult{}, fmt.Errorf("unmarshal index for %s: %w", rawSymbol, err)
	}

	price, _ := ticker.Last.Float64()
	rate, _ := index.CurrentFundRate.Float64()

	return exchange.PotentialFundingResult{
		Symbol:     stdSym,
		Rate:       rate,
		SettleTime: getNextFundingTime(),
		Volume24h:  vol,
		Price:      price,
	}, nil
}

// GetPotentialFundingSymbols satisfies the ScannerClient interface.
func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist, blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	body, err := c.request(ctx, "/fapi/v1/contracts", nil)
	if err != nil {
		return nil, fmt.Errorf("koinbay get contracts: %w", err)
	}

	var contracts []koinbayContract
	if err := json.Unmarshal(body, &contracts); err != nil {
		return nil, fmt.Errorf("unmarshal koinbay contracts: %w", err)
	}

	filtered := c.filterContracts(contracts, whitelist, blacklist)
	results := c.fetchContractsData(ctx, filtered, minVol24h, maxVol24h)

	return results, nil
}

func (c *Client) filterContracts(contracts []koinbayContract, whitelist, blacklist []string) []koinbayContract {
	whitelistMap := make(map[string]bool)
	for _, sym := range whitelist {
		whitelistMap[toStandardSymbol(sym)] = true
	}

	blacklistMap := make(map[string]bool)
	for _, sym := range blacklist {
		blacklistMap[toStandardSymbol(sym)] = true
	}

	var filtered []koinbayContract
	for _, contract := range contracts {
		if contract.Status != 1 {
			continue
		}
		stdSym := toStandardSymbol(contract.Symbol)
		if blacklistMap[stdSym] {
			continue
		}
		if len(whitelistMap) > 0 && !whitelistMap[stdSym] {
			continue
		}
		filtered = append(filtered, contract)
	}
	return filtered
}

type fetchResult struct {
	res exchange.PotentialFundingResult
	err error
}

func (c *Client) fetchContractsData(
	ctx context.Context,
	contracts []koinbayContract,
	minVol24h, maxVol24h float64,
) []exchange.PotentialFundingResult {
	resultsChan := make(chan fetchResult, len(contracts))
	var wg sync.WaitGroup

	// Use a semaphore to limit concurrency to 10
	sem := make(chan struct{}, 10)

	for _, item := range contracts {
		wg.Add(1)
		go func(rawSym, stdSym string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			res, err := c.fetchSingleSymbol(ctx, rawSym, stdSym, minVol24h, maxVol24h)
			resultsChan <- fetchResult{res: res, err: err}
		}(item.Symbol, toStandardSymbol(item.Symbol))
	}

	wg.Wait()
	close(resultsChan)

	var results []exchange.PotentialFundingResult
	for r := range resultsChan {
		if r.err != nil {
			c.logger.Error("failed to fetch koinbay symbol data", "error", r.err)
			continue
		}
		if r.res.Symbol != "" {
			results = append(results, r.res)
		}
	}

	return results
}
