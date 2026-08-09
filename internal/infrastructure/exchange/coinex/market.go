package coinex

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type coinexTickerItem struct {
	Market string `json:"market"`
	Last   string `json:"last"`
	Value  string `json:"value"`
}

type coinexTickerResponse struct {
	Code int                `json:"code"`
	Data []coinexTickerItem `json:"data"`
}

type coinexFundingItem struct {
	Market          string `json:"market"`
	NextFundingRate string `json:"next_funding_rate"`
	NextFundingTime int64  `json:"next_funding_time"`
}

type coinexFundingResponse struct {
	Code int                 `json:"code"`
	Data []coinexFundingItem `json:"data"`
}

func (c *Client) request(ctx context.Context, path string) ([]byte, error) {
	reqURL, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
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
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "_", "")
	return s
}

// GetPotentialFundingSymbols satisfies the ScannerClient interface.
func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	tickersBody, err := c.request(ctx, "/futures/ticker")
	if err != nil {
		return nil, fmt.Errorf("coinex get tickers: %w", err)
	}

	var tickersResp coinexTickerResponse
	if err := xjson.Unmarshal(tickersBody, &tickersResp); err != nil {
		return nil, fmt.Errorf("unmarshal coinex tickers: %w", err)
	}

	if tickersResp.Code != 0 {
		return nil, fmt.Errorf("coinex api error code: %d", tickersResp.Code)
	}

	fundingBody, err := c.request(ctx, "/futures/funding-rate")
	if err != nil {
		return nil, fmt.Errorf("coinex get funding rate: %w", err)
	}

	var fundingResp coinexFundingResponse
	if err := xjson.Unmarshal(fundingBody, &fundingResp); err != nil {
		return nil, fmt.Errorf("unmarshal coinex funding: %w", err)
	}

	if fundingResp.Code != 0 {
		return nil, fmt.Errorf("coinex api error code: %d", fundingResp.Code)
	}

	fundingMap := make(map[string]*coinexFundingItem)
	for i := range fundingResp.Data {
		item := &fundingResp.Data[i]
		fundingMap[item.Market] = item
	}

	whitelistMap := make(map[string]bool)
	for _, sym := range whitelist {
		whitelistMap[toStandardSymbol(sym)] = true
	}

	blacklistMap := make(map[string]bool)
	for _, sym := range blacklist {
		blacklistMap[toStandardSymbol(sym)] = true
	}

	var results []exchange.PotentialFundingResult
	for i := range tickersResp.Data {
		item := &tickersResp.Data[i]
		if res, ok := matchAndFilter(item, fundingMap, whitelistMap, blacklistMap, minVol24h, maxVol24h); ok {
			results = append(results, res)
		}
	}

	return results, nil
}

func matchAndFilter(
	tickerItem *coinexTickerItem,
	fundingMap map[string]*coinexFundingItem,
	whitelistMap, blacklistMap map[string]bool,
	minVol24h, maxVol24h float64,
) (exchange.PotentialFundingResult, bool) {
	// Only support USDT/USDC pairs
	if !strings.HasSuffix(tickerItem.Market, "USDT") && !strings.HasSuffix(tickerItem.Market, "USDC") {
		return exchange.PotentialFundingResult{}, false
	}

	stdSym := toStandardSymbol(tickerItem.Market)
	if blacklistMap[stdSym] {
		return exchange.PotentialFundingResult{}, false
	}
	if len(whitelistMap) > 0 && !whitelistMap[stdSym] {
		return exchange.PotentialFundingResult{}, false
	}

	funding := fundingMap[tickerItem.Market]
	if funding == nil {
		return exchange.PotentialFundingResult{}, false
	}

	vol24h, _ := strconv.ParseFloat(tickerItem.Value, 64)
	if minVol24h > 0 && vol24h < minVol24h {
		return exchange.PotentialFundingResult{}, false
	}
	if maxVol24h > 0 && vol24h > maxVol24h {
		return exchange.PotentialFundingResult{}, false
	}

	price, _ := strconv.ParseFloat(tickerItem.Last, 64)
	rate, _ := strconv.ParseFloat(funding.NextFundingRate, 64)

	return exchange.PotentialFundingResult{
		Symbol:     stdSym,
		Rate:       rate,
		SettleTime: funding.NextFundingTime,
		Volume24h:  vol24h,
		Price:      price,
	}, true
}

func (c *Client) GetTopGainer(_ context.Context, _ exchange.TopGainerRequest) ([]exchange.TopGainerResult, error) {
	return nil, nil
}
