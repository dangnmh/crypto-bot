package batonex

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

type batonexTicker struct {
	Time         int64  `json:"time"`
	Symbol       string `json:"symbol"`
	BestBidPrice string `json:"bestBidPrice"`
	BestAskPrice string `json:"bestAskPrice"`
	LastPrice    string `json:"lastPrice"`
	Volume       string `json:"volume"`
	QuoteVolume  string `json:"quoteVolume"`
}

func (c *Client) request(ctx context.Context, method, path string, query map[string]string) ([]byte, error) {
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

	req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP %s %s: %w", method, path, err)
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

// GetTickers returns 24hr ticker price change statistics.
func (c *Client) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	query := make(map[string]string)
	if symbol != "" {
		query["symbol"] = toBatonexSymbol(symbol)
	}

	body, err := c.request(ctx, http.MethodGet, "/openapi/quote/v1/contract/ticker/24hr", query)
	if err != nil {
		return nil, err
	}

	var rawTickers []batonexTicker
	if symbol != "" {
		var single batonexTicker
		if err := xjson.Unmarshal(body, &single); err != nil {
			return nil, fmt.Errorf("unmarshal single ticker: %w", err)
		}
		rawTickers = []batonexTicker{single}
	} else {
		if err := xjson.Unmarshal(body, &rawTickers); err != nil {
			return nil, fmt.Errorf("unmarshal tickers: %w", err)
		}
	}

	tickers := make([]exchange.Ticker, 0, len(rawTickers))
	for i := range rawTickers {
		item := &rawTickers[i]
		last, _ := strconv.ParseFloat(item.LastPrice, 64)

		bid := last
		if item.BestBidPrice != "" {
			bid, _ = strconv.ParseFloat(item.BestBidPrice, 64)
		}

		ask := last
		if item.BestAskPrice != "" {
			ask, _ = strconv.ParseFloat(item.BestAskPrice, 64)
		}

		vol, _ := strconv.ParseFloat(item.Volume, 64)
		amt, _ := strconv.ParseFloat(item.QuoteVolume, 64)

		tickers = append(tickers, exchange.Ticker{
			Symbol:       toStandardSymbol(item.Symbol),
			LastPrice:    last,
			Bid1:         bid,
			Ask1:         ask,
			Volume24:     vol,
			AmountUSDT24: amt,
			Timestamp:    item.Time,
		})
	}

	return tickers, nil
}

type batonexContract struct {
	Symbol            string `json:"symbol"`
	FundingRate       string `json:"fundingRate"`
	NextFundingRateTs int64  `json:"nextFundingRateTs"`
}

// GetFundingRates returns funding rates for specific standard symbols.
func (c *Client) GetFundingRates(ctx context.Context, symbols []string) ([]exchange.FundingRateResult, error) {
	if len(symbols) == 0 {
		return nil, nil
	}

	body, err := c.request(ctx, http.MethodGet, "/openapi/v1/contracts", nil)
	if err != nil {
		return nil, err
	}

	var rawList []batonexContract
	if err := xjson.Unmarshal(body, &rawList); err != nil {
		return nil, fmt.Errorf("unmarshal contracts: %w", err)
	}

	rateMap := make(map[string]*batonexContract)
	for i := range rawList {
		item := &rawList[i]
		stdSym := toStandardSymbol(item.Symbol)
		rateMap[stdSym] = item
	}

	results := make([]exchange.FundingRateResult, 0, len(symbols))
	for _, sym := range symbols {
		stdSym := toStandardSymbol(sym)
		item, exists := rateMap[stdSym]
		if !exists {
			continue
		}

		rate, _ := strconv.ParseFloat(item.FundingRate, 64)
		settleTime := item.NextFundingRateTs * 1000

		results = append(results, exchange.FundingRateResult{
			Symbol:     sym,
			Rate:       rate,
			SettleTime: settleTime,
		})
	}

	return results, nil
}

func toStandardSymbol(s string) string {
	upper := strings.ToUpper(s)
	upper = strings.ReplaceAll(upper, "-PERP", "")
	upper = strings.ReplaceAll(upper, "-SWAP", "")
	upper = strings.ReplaceAll(upper, "-", "")
	upper = strings.ReplaceAll(upper, "_", "")
	return upper
}

func toBatonexSymbol(s string) string {
	upper := strings.ToUpper(s)
	if strings.Contains(upper, "-PERP") || strings.Contains(upper, "-SWAP") {
		return upper
	}
	if before, ok := strings.CutSuffix(upper, "USDT"); ok {
		return before + "-PERP-USDT"
	}
	if before, ok := strings.CutSuffix(upper, "USD"); ok {
		return before + "-USD"
	}
	return upper
}

func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	tickers, err := c.GetTickers(ctx, "")
	if err != nil {
		return nil, err
	}

	whitelistMap := make(map[string]bool)
	for _, sym := range whitelist {
		whitelistMap[toStandardSymbol(sym)] = true
	}

	blacklistMap := make(map[string]bool)
	for _, sym := range blacklist {
		blacklistMap[toStandardSymbol(sym)] = true
	}

	var filteredSymbols []string
	volMap := make(map[string]float64)
	priceMap := make(map[string]float64)

	for _, t := range tickers {
		stdSym := toStandardSymbol(t.Symbol)
		if blacklistMap[stdSym] {
			continue
		}
		if len(whitelistMap) > 0 && !whitelistMap[stdSym] {
			continue
		}
		if t.AmountUSDT24 < minVol24h {
			continue
		}
		if maxVol24h > 0 && t.AmountUSDT24 > maxVol24h {
			continue
		}

		filteredSymbols = append(filteredSymbols, t.Symbol)
		volMap[stdSym] = t.AmountUSDT24
		priceMap[stdSym] = t.LastPrice
	}

	if len(filteredSymbols) == 0 {
		return nil, nil
	}

	rates, err := c.GetFundingRates(ctx, filteredSymbols)
	if err != nil {
		return nil, err
	}

	var results []exchange.PotentialFundingResult
	for _, r := range rates {
		stdSym := toStandardSymbol(r.Symbol)
		results = append(results, exchange.PotentialFundingResult{
			Symbol:     r.Symbol,
			Rate:       r.Rate,
			SettleTime: r.SettleTime,
			Volume24h:  volMap[stdSym],
			Price:      priceMap[stdSym],
		})
	}

	return results, nil
}
