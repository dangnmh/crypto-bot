package toobit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"crypto-bot/internal/infrastructure/exchange"
)

type toobitTicker struct {
	T   int64  `json:"t"`   // time
	A   string `json:"a"`   // highest selling price
	B   string `json:"b"`   // highest bid
	S   string `json:"s"`   // symbol
	C   string `json:"c"`   // latest transaction price
	O   string `json:"o"`   // open price
	H   string `json:"h"`   // high price
	L   string `json:"l"`   // low price
	V   string `json:"v"`   // Base asset volume
	Qv  string `json:"qv"`  // total trade volume (in quote asset)
	Pc  string `json:"pc"`  // priceChange
	Pcp string `json:"pcp"` // priceChangePercent
}

type toobitFundingRate struct {
	Symbol           string      `json:"symbol"`
	Rate             string      `json:"rate"`
	Period           string      `json:"period"`
	NextFundingTime  json.Number `json:"nextFundingTime"`
	Interest         string      `json:"interest"`
	FundingRateCap   string      `json:"fundingRateCap"`
	FundingRateFloor string      `json:"fundingRateFloor"`
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

// GetTickers returns 24hr ticker price change statistics for all or a specific symbol.
func (c *Client) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	query := make(map[string]string)
	if symbol != "" {
		query["symbol"] = toToobitSymbol(symbol)
	}

	body, err := c.request(ctx, http.MethodGet, "/quote/v1/contract/ticker/24hr", query)
	if err != nil {
		return nil, err
	}

	var rawList []toobitTicker
	if err := json.Unmarshal(body, &rawList); err != nil {
		return nil, fmt.Errorf("unmarshal tickers: %w", err)
	}

	tickers := make([]exchange.Ticker, 0, len(rawList))
	for i := range rawList {
		item := &rawList[i]
		last, _ := strconv.ParseFloat(item.C, 64)
		bid, _ := strconv.ParseFloat(item.B, 64)
		ask, _ := strconv.ParseFloat(item.A, 64)
		vol, _ := strconv.ParseFloat(item.V, 64)
		amt, _ := strconv.ParseFloat(item.Qv, 64)

		tickers = append(tickers, exchange.Ticker{
			Symbol:       toStandardSymbol(item.S),
			LastPrice:    last,
			Bid1:         bid,
			Ask1:         ask,
			Volume24:     vol,
			AmountUSDT24: amt,
			Timestamp:    item.T,
		})
	}

	return tickers, nil
}

// GetFundingRates returns funding rates for specific standard symbols.
func (c *Client) GetFundingRates(ctx context.Context, symbols []string) ([]exchange.FundingRateResult, error) {
	if len(symbols) == 0 {
		return nil, nil
	}

	body, err := c.request(ctx, http.MethodGet, "/api/v1/futures/fundingRate", nil)
	if err != nil {
		return nil, err
	}

	var rawList []toobitFundingRate
	if err := json.Unmarshal(body, &rawList); err != nil {
		return nil, fmt.Errorf("unmarshal funding rates: %w", err)
	}

	// Index rates by standardized symbol
	rateMap := make(map[string]*toobitFundingRate)
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

		ts, _ := item.NextFundingTime.Int64()

		rate, _ := strconv.ParseFloat(item.Rate, 64)
		results = append(results, exchange.FundingRateResult{
			Symbol:     sym, // return matching the requested symbol format
			Rate:       rate,
			SettleTime: ts,
		})
	}

	return results, nil
}

func toToobitSymbol(s string) string {
	upper := strings.ToUpper(s)
	if strings.Contains(upper, "SWAP") {
		return upper
	}
	if before, ok := strings.CutSuffix(upper, "USDT"); ok {
		return before + "-SWAP-USDT"
	}
	if before, ok := strings.CutSuffix(upper, "USDC"); ok {
		return before + "-SWAP-USDC"
	}
	return upper
}

func toStandardSymbol(s string) string {
	upper := strings.ToUpper(s)
	upper = strings.ReplaceAll(upper, "-SWAP", "")
	upper = strings.ReplaceAll(upper, "-", "")
	upper = strings.ReplaceAll(upper, "_", "")
	return upper
}
