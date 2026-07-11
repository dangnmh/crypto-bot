package avantis

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"crypto-bot/internal/infrastructure/exchange"
)

type avantisFeedAttributes struct {
	Symbol    string `json:"symbol"`
	AssetType string `json:"asset_type"`
}

type avantisFeed struct {
	FeedID     string                `json:"feedId"`
	Attributes avantisFeedAttributes `json:"attributes"`
}

type avantisMarginFee struct {
	Long  float64 `json:"long"`
	Short float64 `json:"short"`
}

type avantisPairInfo struct {
	Feed         avantisFeed      `json:"feed"`
	From         string           `json:"from"`
	To           string           `json:"to"`
	PairOI       float64          `json:"pairOI"`
	MarginFee    avantisMarginFee `json:"marginFee"`
	IsPairListed bool             `json:"isPairListed"`
}

type avantisTradingResponse struct {
	PairInfos map[string]avantisPairInfo `json:"pairInfos"`
}

// GetPotentialFundingSymbols fetches all perpetual contracts, their tickers, and estimated funding rates.
func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	// 1. Fetch current trading data configurations
	body, err := c.request(ctx, http.MethodGet, c.baseURL+"/v2/trading", nil)
	if err != nil {
		return nil, fmt.Errorf("fetch trading data: %w", err)
	}

	var tradingResp avantisTradingResponse
	if err := json.Unmarshal(body, &tradingResp); err != nil {
		return nil, fmt.Errorf("unmarshal trading data: %w", err)
	}

	targetedPairs, feedIDs := c.filterAvantisPairs(&tradingResp, whitelist, blacklist)
	if len(targetedPairs) == 0 {
		return nil, nil
	}

	// 2. Fetch prices from Pyth Network in batch
	prices, err := c.fetchPythPrices(ctx, feedIDs)
	if err != nil {
		c.logger.Debug("failed to fetch pyth prices, using 0", "error", err)
	}

	results := c.buildAvantisResults(targetedPairs, prices, minVol24h, maxVol24h)
	return results, nil
}

func (c *Client) filterAvantisPairs(
	tradingResp *avantisTradingResponse,
	whitelist, blacklist []string,
) ([]avantisPairInfo, []string) {
	whitelistMap := make(map[string]bool)
	for _, sym := range whitelist {
		whitelistMap[strings.ToUpper(sym)] = true
	}
	blacklistMap := make(map[string]bool)
	for _, sym := range blacklist {
		blacklistMap[strings.ToUpper(sym)] = true
	}

	var targetedPairs []avantisPairInfo
	var feedIDs []string

	for _, pair := range tradingResp.PairInfos {
		if !pair.IsPairListed {
			continue
		}

		symbolUpper := strings.ToUpper(pair.From + "_" + pair.To)
		baseUpper := strings.ToUpper(pair.From)

		if !isAllowedSymbol(symbolUpper, baseUpper, whitelistMap, blacklistMap) {
			continue
		}

		targetedPairs = append(targetedPairs, pair)
		if pair.Feed.FeedID != "" {
			cleanID := strings.TrimPrefix(strings.ToLower(pair.Feed.FeedID), "0x")
			if strings.Trim(cleanID, "0") != "" {
				feedIDs = append(feedIDs, cleanID)
			}
		}
	}
	return targetedPairs, feedIDs
}

func (c *Client) buildAvantisResults(
	targetedPairs []avantisPairInfo,
	prices map[string]float64,
	minVol24h, maxVol24h float64,
) []exchange.PotentialFundingResult {
	var results []exchange.PotentialFundingResult
	for _, pair := range targetedPairs {
		cleanID := strings.TrimPrefix(strings.ToLower(pair.Feed.FeedID), "0x")
		price := prices[cleanID]

		// Dynamic borrow rate (marginFee.long) is returned as percent per hour (e.g. 0.00171216), convert to absolute decimal
		rate := pair.MarginFee.Long / 100.0

		// Since 24h volume is not public via API, use total pair Open Interest as proxy
		vol24h := pair.PairOI

		if vol24h < minVol24h {
			continue
		}
		if maxVol24h > 0 && vol24h > maxVol24h {
			continue
		}

		symbolUpper := strings.ToUpper(pair.From + "_" + pair.To)

		results = append(results, exchange.PotentialFundingResult{
			Symbol:     symbolUpper,
			Rate:       rate,
			SettleTime: 0,
			Volume24h:  vol24h,
			Price:      price,
		})
	}
	return results
}

func (c *Client) fetchPythPrices(ctx context.Context, feedIDs []string) (map[string]float64, error) {
	prices := make(map[string]float64)
	if len(feedIDs) == 0 {
		return prices, nil
	}

	batchSize := 40
	for i := 0; i < len(feedIDs); i += batchSize {
		end := min(i+batchSize, len(feedIDs))
		batch := feedIDs[i:end]

		var queryParams []string
		for _, id := range batch {
			queryParams = append(queryParams, "ids[]="+id)
		}
		urlStr := c.pythURL + "/v2/updates/price/latest?" + strings.Join(queryParams, "&")

		body, err := c.request(ctx, http.MethodGet, urlStr, nil)
		if err != nil {
			return nil, err
		}

		type pythPriceParsed struct {
			ID    string `json:"id"`
			Price struct {
				Price string `json:"price"`
				Expo  int    `json:"expo"`
			} `json:"price"`
		}
		type pythResponse struct {
			Parsed []pythPriceParsed `json:"parsed"`
		}

		var resp pythResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, err
		}

		for _, p := range resp.Parsed {
			rawVal, _ := strconv.ParseFloat(p.Price.Price, 64)
			factor := math.Pow10(p.Price.Expo)
			prices[strings.ToLower(p.ID)] = rawVal * factor
		}
	}
	return prices, nil
}

func isAllowedSymbol(symbolUpper, baseUpper string, whitelistMap, blacklistMap map[string]bool) bool {
	if symbolUpper == "" {
		return false
	}

	normSymbol := symbolUpper
	for _, suffix := range []string{"_USD", "USD", "_USDC", "USDC", "_USDT", "USDT"} {
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
