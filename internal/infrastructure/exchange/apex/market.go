package apex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
)

type apexTickerItem struct {
	Symbol          string `json:"symbol"`
	FundingRate     string `json:"fundingRate"`
	NextFundingTime string `json:"nextFundingTime"`
	MarkPrice       string `json:"markPrice"`
	IndexPrice      string `json:"indexPrice"`
	LastPrice       string `json:"lastPrice"`
	Turnover24h     string `json:"turnover24h"`
	Volume24h       string `json:"volume24h"`
}

type apexTickerResponse struct {
	Data []apexTickerItem `json:"data"`
}

// GetPotentialFundingSymbols fetches all perpetual contracts, their tickers, and estimated funding rates.
func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	body, err := c.request(ctx, http.MethodGet, "/api/v3/data/all-ticker-info")
	if err != nil {
		return nil, fmt.Errorf("fetch all-ticker-info: %w", err)
	}

	var response apexTickerResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("unmarshal all-ticker-info: %w", err)
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
	for i := range response.Data {
		if res, ok := parseStatsItem(&response.Data[i], minVol24h, maxVol24h, whitelistMap, blacklistMap); ok {
			results = append(results, res)
		}
	}

	return results, nil
}

func parseStatsItem(
	item *apexTickerItem,
	minVol24h, maxVol24h float64,
	whitelistMap, blacklistMap map[string]bool,
) (exchange.PotentialFundingResult, bool) {
	symbolUpper := strings.ToUpper(item.Symbol)
	if !isAllowedSymbol(symbolUpper, whitelistMap, blacklistMap) {
		return exchange.PotentialFundingResult{}, false
	}

	lastPrice, err := parsePrice(item)
	if err != nil {
		return exchange.PotentialFundingResult{}, false
	}

	volumeUSD, _ := strconv.ParseFloat(item.Turnover24h, 64)
	if volumeUSD < minVol24h {
		return exchange.PotentialFundingResult{}, false
	}
	if maxVol24h > 0 && volumeUSD > maxVol24h {
		return exchange.PotentialFundingResult{}, false
	}

	rateVal, _ := strconv.ParseFloat(item.FundingRate, 64)

	var settleTime int64
	if item.NextFundingTime != "" {
		if parsed, err := time.Parse(time.RFC3339, item.NextFundingTime); err == nil {
			settleTime = parsed.UnixMilli()
		}
	}

	return exchange.PotentialFundingResult{
		Symbol:     symbolUpper,
		Rate:       rateVal,
		SettleTime: settleTime,
		Volume24h:  volumeUSD,
		Price:      lastPrice,
	}, true
}

func isAllowedSymbol(symbolUpper string, whitelistMap, blacklistMap map[string]bool) bool {
	if symbolUpper == "" {
		return false
	}

	var baseAsset string
	if before, ok := strings.CutSuffix(symbolUpper, "USDT"); ok {
		baseAsset = before
	} else if before, ok := strings.CutSuffix(symbolUpper, "USDC"); ok {
		baseAsset = before
	}

	if blacklistMap[symbolUpper] || (baseAsset != "" && blacklistMap[baseAsset]) {
		return false
	}
	if len(whitelistMap) > 0 && !whitelistMap[symbolUpper] && !whitelistMap[baseAsset] {
		return false
	}
	return true
}

func parsePrice(item *apexTickerItem) (float64, error) {
	if p, err := strconv.ParseFloat(item.LastPrice, 64); err == nil && p > 0 {
		return p, nil
	}
	if p, err := strconv.ParseFloat(item.MarkPrice, 64); err == nil && p > 0 {
		return p, nil
	}
	if p, err := strconv.ParseFloat(item.IndexPrice, 64); err == nil && p > 0 {
		return p, nil
	}
	return 0, fmt.Errorf("no valid price found")
}

func (c *Client) GetTopGainer(_ context.Context, _ exchange.TopGainerRequest) ([]exchange.TopGainerResult, error) {
	return nil, nil
}
