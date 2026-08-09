package extended

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"crypto-bot/internal/infrastructure/exchange"
)

type extendedMarketStats struct {
	DailyVolume     string `json:"dailyVolume"`
	LastPrice       string `json:"lastPrice"`
	FundingRate     string `json:"fundingRate"`
	NextFundingRate int64  `json:"nextFundingRate"`
}

type extendedMarket struct {
	Name        string              `json:"name"`
	Type        string              `json:"type"`
	Status      string              `json:"status"`
	AssetName   string              `json:"assetName"`
	MarketStats extendedMarketStats `json:"marketStats"`
}

type extendedMarketsResponse struct {
	Status string           `json:"status"`
	Data   []extendedMarket `json:"data"`
}

// GetPotentialFundingSymbols fetches all perpetual contracts, their tickers, and estimated funding rates.
func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	// Query bulk markets stats
	body, err := c.request(ctx, http.MethodGet, "/api/v1/info/markets", nil)
	if err != nil {
		return nil, fmt.Errorf("fetch markets: %w", err)
	}

	var marketsResp extendedMarketsResponse
	if err := json.Unmarshal(body, &marketsResp); err != nil {
		return nil, fmt.Errorf("unmarshal markets: %w", err)
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
	for _, m := range marketsResp.Data {
		if !strings.EqualFold(m.Type, "PERPETUAL") {
			continue
		}
		if !strings.EqualFold(m.Status, "ACTIVE") {
			continue
		}

		symbolUpper := strings.ToUpper(m.Name)
		baseUpper := strings.ToUpper(m.AssetName)

		if !isAllowedSymbol(symbolUpper, baseUpper, whitelistMap, blacklistMap) {
			continue
		}

		lastPrice, _ := strconv.ParseFloat(m.MarketStats.LastPrice, 64)
		vol24h, _ := strconv.ParseFloat(m.MarketStats.DailyVolume, 64)
		fundingRate, _ := strconv.ParseFloat(m.MarketStats.FundingRate, 64)

		if vol24h < minVol24h {
			continue
		}
		if maxVol24h > 0 && vol24h > maxVol24h {
			continue
		}

		results = append(results, exchange.PotentialFundingResult{
			Symbol:     symbolUpper,
			Rate:       fundingRate,
			SettleTime: m.MarketStats.NextFundingRate,
			Volume24h:  vol24h,
			Price:      lastPrice,
		})
	}

	return results, nil
}

func isAllowedSymbol(symbolUpper, baseUpper string, whitelistMap, blacklistMap map[string]bool) bool {
	if symbolUpper == "" {
		return false
	}

	normSymbol := symbolUpper
	for _, suffix := range []string{"-USD", "-USDT", "-USDC"} {
		if before, ok := strings.CutSuffix(symbolUpper, suffix); ok {
			normSymbol = before
			break
		}
	}

	if blacklistMap[symbolUpper] || blacklistMap[normSymbol] || blacklistMap[baseUpper] {
		return false
	}
	if len(whitelistMap) > 0 && !whitelistMap[symbolUpper] && !whitelistMap[normSymbol] && !whitelistMap[baseUpper] {
		return false
	}
	return true
}

func (c *Client) GetTopGainer(_ context.Context, _ exchange.TopGainerRequest) ([]exchange.TopGainerResult, error) {
	return nil, nil
}
