package lighter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
)

type lighterOrderBookStat struct {
	Symbol                string  `json:"symbol"`
	LastTradePrice        float64 `json:"last_trade_price"`
	DailyQuoteTokenVolume float64 `json:"daily_quote_token_volume"`
}

type lighterExchangeStats struct {
	Code           int                    `json:"code"`
	Message        string                 `json:"message"`
	OrderBookStats []lighterOrderBookStat `json:"order_book_stats"`
}

type lighterFundingRateItem struct {
	Symbol string  `json:"symbol"`
	Rate   float64 `json:"rate"`
}

type lighterFundingRatesResponse struct {
	Code         int                      `json:"code"`
	Message      string                   `json:"message"`
	FundingRates []lighterFundingRateItem `json:"funding_rates"`
}

// GetPotentialFundingSymbols fetches all perpetual contracts, their tickers, and estimated funding rates.
func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	// 1. Fetch exchangeStats for tickers (price, volume)
	statsBytes, err := c.request(ctx, http.MethodGet, "/api/v1/exchangeStats")
	if err != nil {
		return nil, fmt.Errorf("fetch exchangeStats: %w", err)
	}

	var statsResp lighterExchangeStats
	if err := json.Unmarshal(statsBytes, &statsResp); err != nil {
		return nil, fmt.Errorf("unmarshal exchangeStats: %w", err)
	}

	// 2. Fetch funding rates
	ratesBytes, err := c.request(ctx, http.MethodGet, "/api/v1/funding-rates")
	if err != nil {
		return nil, fmt.Errorf("fetch funding-rates: %w", err)
	}

	var ratesResp lighterFundingRatesResponse
	if err := json.Unmarshal(ratesBytes, &ratesResp); err != nil {
		return nil, fmt.Errorf("unmarshal funding-rates: %w", err)
	}

	// 3. Build maps
	fundingMap := make(map[string]float64)
	for _, rateItem := range ratesResp.FundingRates {
		fundingMap[strings.ToUpper(rateItem.Symbol)] = rateItem.Rate
	}

	whitelistMap := make(map[string]bool)
	for _, sym := range whitelist {
		whitelistMap[strings.ToUpper(sym)] = true
	}
	blacklistMap := make(map[string]bool)
	for _, sym := range blacklist {
		blacklistMap[strings.ToUpper(sym)] = true
	}

	// Settle time is hourly (next top of the hour)
	settleTime := time.Now().Truncate(time.Hour).Add(time.Hour).UnixMilli()

	// 4. Build results
	var results []exchange.PotentialFundingResult
	for _, statItem := range statsResp.OrderBookStats {
		symbolUpper := strings.ToUpper(statItem.Symbol)
		// Spot symbols on Lighter contain "/", e.g., LIT/USDC. Skip them.
		if strings.Contains(symbolUpper, "/") {
			continue
		}

		if !isAllowedSymbol(symbolUpper, whitelistMap, blacklistMap) {
			continue
		}

		vol := statItem.DailyQuoteTokenVolume
		if vol < minVol24h {
			continue
		}
		if maxVol24h > 0 && vol > maxVol24h {
			continue
		}

		rateVal, exists := fundingMap[symbolUpper]
		if !exists {
			continue
		}

		results = append(results, exchange.PotentialFundingResult{
			Symbol:     symbolUpper,
			Rate:       rateVal,
			SettleTime: settleTime,
			Volume24h:  vol,
			Price:      statItem.LastTradePrice,
		})
	}

	return results, nil
}

func isAllowedSymbol(symbolUpper string, whitelistMap, blacklistMap map[string]bool) bool {
	if symbolUpper == "" {
		return false
	}

	// Strip common quote/perp suffixes to check base asset
	baseAsset := symbolUpper
	for _, suffix := range []string{"_USDT", "USDT", "_USDC", "USDC", "-PERP", "PERP"} {
		if before, ok := strings.CutSuffix(symbolUpper, suffix); ok {
			baseAsset = before
			break
		}
	}

	if blacklistMap[symbolUpper] || blacklistMap[baseAsset] {
		return false
	}
	if len(whitelistMap) > 0 && !whitelistMap[symbolUpper] && !whitelistMap[baseAsset] {
		return false
	}
	return true
}

func (c *Client) GetTopGainer(_ context.Context, _ exchange.TopGainerRequest) ([]exchange.TopGainerResult, error) {
	return nil, nil
}
