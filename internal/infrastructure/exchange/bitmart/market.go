package bitmart

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

type bitmartSymbolDetail struct {
	Symbol       string `json:"symbol"`
	LastPrice    string `json:"last_price"`
	Volume24h    string `json:"volume_24h"`
	Turnover24h  string `json:"turnover_24h"`
	ContractSize string `json:"contract_size"`
	FundingRate  string `json:"funding_rate"`
	FundingTime  int64  `json:"funding_time"`
}

type bitmartData struct {
	Symbols []bitmartSymbolDetail `json:"symbols"`
}

type bitmartResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    bitmartData `json:"data"`
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
		query["symbol"] = toBitmartSymbol(symbol)
	}

	body, err := c.request(ctx, http.MethodGet, "/contract/public/details", query)
	if err != nil {
		return nil, err
	}

	var resp bitmartResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal bitmart response: %w", err)
	}

	if resp.Code != 1000 {
		return nil, fmt.Errorf("bitmart API error: %d - %s", resp.Code, resp.Message)
	}

	tickers := make([]exchange.Ticker, 0, len(resp.Data.Symbols))
	for i := range resp.Data.Symbols {
		item := &resp.Data.Symbols[i]
		last, _ := strconv.ParseFloat(item.LastPrice, 64)

		bid := last
		ask := last

		vol, _ := strconv.ParseFloat(item.Volume24h, 64)
		if contractSize, err := strconv.ParseFloat(item.ContractSize, 64); err == nil && contractSize > 0 {
			vol *= contractSize
		}

		amt, _ := strconv.ParseFloat(item.Turnover24h, 64)

		tickers = append(tickers, exchange.Ticker{
			Symbol:       toStandardSymbol(item.Symbol),
			LastPrice:    last,
			Bid1:         bid,
			Ask1:         ask,
			Volume24:     vol,
			AmountUSDT24: amt,
			Timestamp:    item.FundingTime,
		})
	}

	return tickers, nil
}

// GetFundingRates returns funding rates for specific standard symbols.
func (c *Client) GetFundingRates(ctx context.Context, symbols []string) ([]exchange.FundingRateResult, error) {
	if len(symbols) == 0 {
		return nil, nil
	}

	body, err := c.request(ctx, http.MethodGet, "/contract/public/details", nil)
	if err != nil {
		return nil, err
	}

	var resp bitmartResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal bitmart response: %w", err)
	}

	if resp.Code != 1000 {
		return nil, fmt.Errorf("bitmart API error: %d - %s", resp.Code, resp.Message)
	}

	rateMap := make(map[string]*bitmartSymbolDetail)
	for i := range resp.Data.Symbols {
		item := &resp.Data.Symbols[i]
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

		results = append(results, exchange.FundingRateResult{
			Symbol:     sym,
			Rate:       rate,
			SettleTime: item.FundingTime,
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

func toBitmartSymbol(s string) string {
	upper := strings.ToUpper(s)
	upper = strings.ReplaceAll(upper, "-PERP", "")
	upper = strings.ReplaceAll(upper, "-SWAP", "")
	upper = strings.ReplaceAll(upper, "-", "")
	upper = strings.ReplaceAll(upper, "_", "")
	return upper
}

func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	body, err := c.request(ctx, http.MethodGet, "/contract/public/details", nil)
	if err != nil {
		return nil, err
	}

	var resp bitmartResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal bitmart response: %w", err)
	}

	if resp.Code != 1000 {
		return nil, fmt.Errorf("bitmart API error: %d - %s", resp.Code, resp.Message)
	}

	whitelistMap := make(map[string]bool)
	for _, sym := range whitelist {
		whitelistMap[toStandardSymbol(sym)] = true
	}

	blacklistMap := make(map[string]bool)
	for _, sym := range blacklist {
		blacklistMap[toStandardSymbol(sym)] = true
	}

	var results []exchange.PotentialFundingResult
	for i := range resp.Data.Symbols {
		item := &resp.Data.Symbols[i]
		stdSym := toStandardSymbol(item.Symbol)
		if blacklistMap[stdSym] {
			continue
		}
		if len(whitelistMap) > 0 && !whitelistMap[stdSym] {
			continue
		}

		amt, _ := strconv.ParseFloat(item.Turnover24h, 64)
		if amt < minVol24h {
			continue
		}
		if maxVol24h > 0 && amt > maxVol24h {
			continue
		}

		rate, _ := strconv.ParseFloat(item.FundingRate, 64)
		price, _ := strconv.ParseFloat(item.LastPrice, 64)
		results = append(results, exchange.PotentialFundingResult{
			Symbol:     toStandardSymbol(item.Symbol),
			Rate:       rate,
			SettleTime: item.FundingTime,
			Volume24h:  amt,
			Price:      price,
		})
	}

	return results, nil
}
