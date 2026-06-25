package bitunix

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type bitunixTicker struct {
	Symbol    string `json:"symbol"`
	MarkPrice string `json:"markPrice"`
	LastPrice string `json:"lastPrice"`
	QuoteVol  string `json:"quoteVol"`
}

type bitunixTickersResponse struct {
	Data []bitunixTicker `json:"data"`
	Msg  string          `json:"msg"`
}

type bitunixFundingRate struct {
	Symbol          string `json:"symbol"`
	FundingRate     string `json:"fundingRate"`
	NextFundingTime string `json:"nextFundingTime"`
	MarkPrice       string `json:"markPrice"`
}

type bitunixFundingRateResponse struct {
	Data []bitunixFundingRate `json:"data"`
	Msg  string               `json:"msg"`
}

func (c *Client) fetchTickersData(ctx context.Context) (map[string]float64, map[string]float64, error) {
	tickersBody, err := c.request(ctx, http.MethodGet, "/api/v1/futures/market/tickers", nil)
	if err != nil {
		return nil, nil, fmt.Errorf("bitunix fetch tickers: %w", err)
	}

	var tickersResp bitunixTickersResponse
	if err := xjson.Unmarshal(tickersBody, &tickersResp); err != nil {
		return nil, nil, fmt.Errorf("bitunix unmarshal tickers: %w", err)
	}

	volMap := make(map[string]float64)
	priceMap := make(map[string]float64)
	for i := range tickersResp.Data {
		item := &tickersResp.Data[i]
		vol, _ := strconv.ParseFloat(item.QuoteVol, 64)
		price, _ := strconv.ParseFloat(item.MarkPrice, 64)
		if price == 0 {
			price, _ = strconv.ParseFloat(item.LastPrice, 64)
		}
		volMap[item.Symbol] = vol
		priceMap[item.Symbol] = price
	}

	return volMap, priceMap, nil
}

// GetPotentialFundingSymbols fetches and returns active perpetual contracts with funding rates and 24h volumes.
func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	// 1. Fetch 24h tickers to get volume and price mapping
	volMap, priceMap, err := c.fetchTickersData(ctx)
	if err != nil {
		return nil, err
	}

	// 2. Fetch funding rates in batch
	ratesBody, err := c.request(ctx, http.MethodGet, "/api/v1/futures/market/funding_rate/batch", nil)
	if err != nil {
		return nil, fmt.Errorf("bitunix fetch funding rates batch: %w", err)
	}

	var ratesResp bitunixFundingRateResponse
	if err := xjson.Unmarshal(ratesBody, &ratesResp); err != nil {
		return nil, fmt.Errorf("bitunix unmarshal funding rates batch: %w", err)
	}

	whitelistMap := make(map[string]bool)
	for _, sym := range whitelist {
		whitelistMap[sym] = true
	}

	blacklistMap := make(map[string]bool)
	for _, sym := range blacklist {
		blacklistMap[sym] = true
	}

	var results []exchange.PotentialFundingResult
	for i := range ratesResp.Data {
		item := &ratesResp.Data[i]

		if blacklistMap[item.Symbol] {
			continue
		}
		if len(whitelistMap) > 0 && !whitelistMap[item.Symbol] {
			continue
		}

		vol := volMap[item.Symbol]
		if vol < minVol24h {
			continue
		}
		if maxVol24h > 0 && vol > maxVol24h {
			continue
		}

		rate, _ := strconv.ParseFloat(item.FundingRate, 64)
		// Bitunix returns funding rate in percent (e.g. 0.01 for 0.01%).
		// Convert to raw rate (0.01% -> 0.0001).
		rate /= 100.0

		price := priceMap[item.Symbol]
		if price == 0 {
			price, _ = strconv.ParseFloat(item.MarkPrice, 64)
		}

		nextSettleStr := item.NextFundingTime
		nextSettle, _ := strconv.ParseInt(nextSettleStr, 10, 64)

		results = append(results, exchange.PotentialFundingResult{
			Symbol:     item.Symbol,
			Rate:       rate,
			SettleTime: nextSettle,
			Volume24h:  vol,
			Price:      price,
		})
	}

	return results, nil
}
