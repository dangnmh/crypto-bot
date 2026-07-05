package aevo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"crypto-bot/internal/infrastructure/exchange"
)

type aevoStatsItem struct {
	TickerID                 string `json:"ticker_id"`
	BaseCurrency             string `json:"base_currency"`
	TargetCurrency           string `json:"target_currency"`
	TargetVolume             string `json:"target_volume"`
	ProductType              string `json:"product_type"`
	OpenInterest             string `json:"open_interest"`
	IndexPrice               string `json:"index_price"`
	NextFundingRateTimestamp string `json:"next_funding_rate_timestamp"`
	FundingRate              string `json:"funding_rate"`
}

// GetPotentialFundingSymbols fetches all perpetual contracts, their tickers, and estimated funding rates.
func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	body, err := c.request(ctx, http.MethodGet, "/coingecko-statistics")
	if err != nil {
		return nil, fmt.Errorf("fetch coingecko-statistics: %w", err)
	}

	var items []aevoStatsItem
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("unmarshal coingecko-statistics: %w", err)
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
	for i := range items {
		if res, ok := parseStatsItem(&items[i], minVol24h, maxVol24h, whitelistMap, blacklistMap); ok {
			results = append(results, res)
		}
	}

	return results, nil
}

func parseStatsItem(
	item *aevoStatsItem,
	minVol24h, maxVol24h float64,
	whitelistMap, blacklistMap map[string]bool,
) (exchange.PotentialFundingResult, bool) {
	if !strings.EqualFold(item.ProductType, "Perpetual") {
		return exchange.PotentialFundingResult{}, false
	}

	symbolUpper := strings.ToUpper(item.TickerID)
	baseUpper := strings.ToUpper(item.BaseCurrency)

	if blacklistMap[symbolUpper] || blacklistMap[baseUpper] {
		return exchange.PotentialFundingResult{}, false
	}
	if len(whitelistMap) > 0 && !whitelistMap[symbolUpper] && !whitelistMap[baseUpper] {
		return exchange.PotentialFundingResult{}, false
	}

	lastPrice, err := strconv.ParseFloat(item.IndexPrice, 64)
	if err != nil || lastPrice <= 0 {
		return exchange.PotentialFundingResult{}, false
	}

	volumeUSD, _ := strconv.ParseFloat(item.TargetVolume, 64)
	if volumeUSD < minVol24h {
		return exchange.PotentialFundingResult{}, false
	}
	if maxVol24h > 0 && volumeUSD > maxVol24h {
		return exchange.PotentialFundingResult{}, false
	}

	rateVal, _ := strconv.ParseFloat(item.FundingRate, 64)

	var settleTime int64
	if item.NextFundingRateTimestamp != "" && item.NextFundingRateTimestamp != "0" {
		if sec, err := strconv.ParseInt(item.NextFundingRateTimestamp, 10, 64); err == nil {
			settleTime = sec * 1000
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
