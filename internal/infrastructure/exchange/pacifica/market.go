package pacifica

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"crypto-bot/internal/infrastructure/exchange"
)

type pacificaInfo struct {
	Symbol         string `json:"symbol"`
	InstrumentType string `json:"instrument_type"`
	BaseAsset      string `json:"base_asset"`
}

type pacificaInfoResponse struct {
	Data []pacificaInfo `json:"data"`
}

type pacificaPrice struct {
	Symbol    string `json:"symbol"`
	Mark      string `json:"mark"`
	Funding   string `json:"funding"`
	Volume24h string `json:"volume_24h"`
}

type pacificaPricesResponse struct {
	Success bool            `json:"success"`
	Data    []pacificaPrice `json:"data"`
}

// GetPotentialFundingSymbols fetches all perpetual contracts, their tickers, and estimated funding rates.
func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	// 1. Fetch market info metadata
	infoBytes, err := c.request(ctx, http.MethodGet, "/api/v1/info", nil)
	if err != nil {
		return nil, fmt.Errorf("fetch info: %w", err)
	}

	var infoResp pacificaInfoResponse
	if err := json.Unmarshal(infoBytes, &infoResp); err != nil {
		return nil, fmt.Errorf("unmarshal info: %w", err)
	}

	// 2. Fetch prices/tickers
	priceBytes, err := c.request(ctx, http.MethodGet, "/api/v1/info/prices", nil)
	if err != nil {
		return nil, fmt.Errorf("fetch prices: %w", err)
	}

	var priceResp pacificaPricesResponse
	if err := json.Unmarshal(priceBytes, &priceResp); err != nil {
		return nil, fmt.Errorf("unmarshal prices: %w", err)
	}

	whitelistMap := make(map[string]bool)
	for _, sym := range whitelist {
		whitelistMap[strings.ToUpper(sym)] = true
	}
	blacklistMap := make(map[string]bool)
	for _, sym := range blacklist {
		blacklistMap[strings.ToUpper(sym)] = true
	}

	// Index info elements by symbol
	infoMap := make(map[string]pacificaInfo)
	for _, info := range infoResp.Data {
		infoMap[strings.ToUpper(info.Symbol)] = info
	}

	var results []exchange.PotentialFundingResult
	for _, p := range priceResp.Data {
		symbolUpper := strings.ToUpper(p.Symbol)
		info, ok := infoMap[symbolUpper]
		if !ok {
			continue
		}

		if !strings.EqualFold(info.InstrumentType, "perpetual") {
			continue
		}

		baseUpper := strings.ToUpper(info.BaseAsset)
		if !isAllowedSymbol(symbolUpper, baseUpper, whitelistMap, blacklistMap) {
			continue
		}

		markPrice, _ := strconv.ParseFloat(p.Mark, 64)
		vol24h, _ := strconv.ParseFloat(p.Volume24h, 64)
		fundingRate, _ := strconv.ParseFloat(p.Funding, 64)

		if vol24h < minVol24h {
			continue
		}
		if maxVol24h > 0 && vol24h > maxVol24h {
			continue
		}

		results = append(results, exchange.PotentialFundingResult{
			Symbol:     symbolUpper,
			Rate:       fundingRate,
			SettleTime: 0, // Hourly continuous borrow/funding model
			Volume24h:  vol24h,
			Price:      markPrice,
		})
	}

	return results, nil
}

func isAllowedSymbol(symbolUpper, baseUpper string, whitelistMap, blacklistMap map[string]bool) bool {
	if symbolUpper == "" {
		return false
	}

	normSymbol := symbolUpper
	for _, suffix := range []string{"_PERP", "-PERP", "PERP"} {
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
