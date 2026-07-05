package grvt

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"crypto-bot/internal/infrastructure/exchange"
)

type grvtInstrument struct {
	Instrument string `json:"instrument"`
	Base       string `json:"base"`
	Quote      string `json:"quote"`
	Kind       string `json:"kind"`
}

type grvtAllInstrumentsResponse struct {
	Result []grvtInstrument `json:"result"`
}

type grvtTicker struct {
	Instrument      string `json:"instrument"`
	LastPrice       string `json:"last_price"`
	BuyVolume24hQ   string `json:"buy_volume_24h_q"`
	SellVolume24hQ  string `json:"sell_volume_24h_q"`
	FundingRate     string `json:"funding_rate"`
	NextFundingTime string `json:"next_funding_time"`
}

type grvtTickerResponse struct {
	Result grvtTicker `json:"result"`
}

// GetPotentialFundingSymbols fetches all perpetual contracts, their tickers, and estimated funding rates.
func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	// 1. Fetch active instruments
	payload := map[string]bool{"is_active": true}
	instBytes, err := c.request(ctx, http.MethodPost, "/full/v1/all_instruments", payload)
	if err != nil {
		return nil, fmt.Errorf("fetch all_instruments: %w", err)
	}

	var instResp grvtAllInstrumentsResponse
	if err := json.Unmarshal(instBytes, &instResp); err != nil {
		return nil, fmt.Errorf("unmarshal all_instruments: %w", err)
	}

	whitelistMap := make(map[string]bool)
	for _, sym := range whitelist {
		whitelistMap[strings.ToUpper(sym)] = true
	}
	blacklistMap := make(map[string]bool)
	for _, sym := range blacklist {
		blacklistMap[strings.ToUpper(sym)] = true
	}

	// 2. Filter for perpetual instruments and allowed symbols
	targetedInstruments := c.filterInstruments(instResp.Result, whitelistMap, blacklistMap)

	if len(targetedInstruments) == 0 {
		return nil, nil
	}

	// 3. Fetch tickers in parallel
	type fetchResult struct {
		res exchange.PotentialFundingResult
		err error
	}

	resultsChan := make(chan fetchResult, len(targetedInstruments))
	var wg sync.WaitGroup

	for _, inst := range targetedInstruments {
		wg.Add(1)
		go func(item grvtInstrument) {
			defer wg.Done()
			res, err := c.fetchSingleTicker(ctx, item)
			resultsChan <- fetchResult{res: res, err: err}
		}(inst)
	}

	wg.Wait()
	close(resultsChan)

	// 4. Collect results and apply volume checks
	var results []exchange.PotentialFundingResult
	for r := range resultsChan {
		if r.err != nil {
			c.logger.Debug("failed to fetch grvt ticker", "error", r.err)
			continue
		}

		if r.res.Symbol == "" {
			continue
		}

		if r.res.Volume24h < minVol24h {
			continue
		}
		if maxVol24h > 0 && r.res.Volume24h > maxVol24h {
			continue
		}

		results = append(results, r.res)
	}

	return results, nil
}

func (c *Client) filterInstruments(
	instruments []grvtInstrument,
	whitelistMap, blacklistMap map[string]bool,
) []grvtInstrument {
	var targeted []grvtInstrument
	for _, inst := range instruments {
		if !strings.EqualFold(inst.Kind, "PERPETUAL") {
			continue
		}

		symbolUpper := strings.ToUpper(inst.Instrument)
		baseUpper := strings.ToUpper(inst.Base)

		if !isAllowedSymbol(symbolUpper, baseUpper, whitelistMap, blacklistMap) {
			continue
		}

		targeted = append(targeted, inst)
	}
	return targeted
}

func (c *Client) fetchSingleTicker(ctx context.Context, inst grvtInstrument) (exchange.PotentialFundingResult, error) {
	payload := map[string]string{"instrument": inst.Instrument}
	tickerBytes, err := c.request(ctx, http.MethodPost, "/full/v1/ticker", payload)
	if err != nil {
		return exchange.PotentialFundingResult{}, err
	}

	var tickerResp grvtTickerResponse
	if err := json.Unmarshal(tickerBytes, &tickerResp); err != nil {
		return exchange.PotentialFundingResult{}, fmt.Errorf("unmarshal ticker: %w", err)
	}

	ticker := tickerResp.Result

	// Parse last price
	lastPrice, _ := strconv.ParseFloat(ticker.LastPrice, 64)

	// Volume24h is the sum of buy and sell quote volume
	buyVol, _ := strconv.ParseFloat(ticker.BuyVolume24hQ, 64)
	sellVol, _ := strconv.ParseFloat(ticker.SellVolume24hQ, 64)
	vol24h := buyVol + sellVol

	// Parse funding rate (returns as percent, e.g. "0.01", convert to absolute)
	fundingPercent, _ := strconv.ParseFloat(ticker.FundingRate, 64)
	fundingRate := fundingPercent / 100.0

	// Parse settle time (returns in nanoseconds, convert to milliseconds)
	var settleTime int64
	if ticker.NextFundingTime != "" {
		if nft, err := strconv.ParseInt(ticker.NextFundingTime, 10, 64); err == nil {
			settleTime = nft / 1_000_000
		}
	}

	return exchange.PotentialFundingResult{
		Symbol:     strings.ToUpper(inst.Instrument),
		Rate:       fundingRate,
		SettleTime: settleTime,
		Volume24h:  vol24h,
		Price:      lastPrice,
	}, nil
}

func isAllowedSymbol(symbolUpper, baseUpper string, whitelistMap, blacklistMap map[string]bool) bool {
	if symbolUpper == "" {
		return false
	}

	// Strip suffixes to form normalized trade symbol, e.g., BTC_USDT_PERP -> BTC_USDT
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
