package zoomex

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

const (
	categoryLinear = "linear"
	paramCategory  = "category"
)

type zoomexTicker struct {
	Symbol          string `json:"symbol"`
	LastPrice       string `json:"lastPrice"`
	Volume24h       string `json:"volume24h"`
	Turnover24h     string `json:"turnover24h"`
	FundingRate     string `json:"fundingRate"`
	NextFundingTime string `json:"nextFundingTime"`
	Bid1Price       string `json:"bid1Price"`
	Ask1Price       string `json:"ask1Price"`
}

type zoomexResponse struct {
	RetCode int    `json:"retCode"`
	RetMsg  string `json:"retMsg"`
	Result  struct {
		Category string         `json:"category"`
		List     []zoomexTicker `json:"list"`
	} `json:"result"`
	Time int64 `json:"time"`
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
	query := map[string]string{
		paramCategory: categoryLinear,
	}
	if symbol != "" {
		query["symbol"] = symbol
	}

	body, err := c.request(ctx, http.MethodGet, "/cloud/trade/v3/market/tickers", query)
	if err != nil {
		return nil, err
	}

	var resp zoomexResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal zoomex response: %w", err)
	}

	if resp.RetCode != 0 {
		return nil, fmt.Errorf("zoomex API error: %d - %s", resp.RetCode, resp.RetMsg)
	}

	tickers := make([]exchange.Ticker, 0, len(resp.Result.List))
	for i := range resp.Result.List {
		item := &resp.Result.List[i]
		last, _ := strconv.ParseFloat(item.LastPrice, 64)

		bid := last
		if item.Bid1Price != "" {
			bid, _ = strconv.ParseFloat(item.Bid1Price, 64)
		}

		ask := last
		if item.Ask1Price != "" {
			ask, _ = strconv.ParseFloat(item.Ask1Price, 64)
		}

		vol, _ := strconv.ParseFloat(item.Volume24h, 64)
		amt, _ := strconv.ParseFloat(item.Turnover24h, 64)

		ts := resp.Time
		if item.NextFundingTime != "" {
			if fundingTs, err := strconv.ParseInt(item.NextFundingTime, 10, 64); err == nil && fundingTs > 0 {
				ts = fundingTs
			}
		}

		tickers = append(tickers, exchange.Ticker{
			Symbol:       toStandardSymbol(item.Symbol),
			LastPrice:    last,
			Bid1:         bid,
			Ask1:         ask,
			Volume24:     vol,
			AmountUSDT24: amt,
			Timestamp:    ts,
		})
	}

	return tickers, nil
}

// GetFundingRates returns funding rates for specific standard symbols.
func (c *Client) GetFundingRates(ctx context.Context, symbols []string) ([]exchange.FundingRateResult, error) {
	if len(symbols) == 0 {
		return nil, nil
	}

	body, err := c.request(ctx, http.MethodGet, "/cloud/trade/v3/market/tickers", map[string]string{
		paramCategory: categoryLinear,
	})
	if err != nil {
		return nil, err
	}

	var resp zoomexResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal zoomex response: %w", err)
	}

	if resp.RetCode != 0 {
		return nil, fmt.Errorf("zoomex API error: %d - %s", resp.RetCode, resp.RetMsg)
	}

	rateMap := make(map[string]*zoomexTicker)
	for i := range resp.Result.List {
		item := &resp.Result.List[i]
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
		settleTime, _ := strconv.ParseInt(item.NextFundingTime, 10, 64)

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

func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	query := map[string]string{
		paramCategory: categoryLinear,
	}

	body, err := c.request(ctx, http.MethodGet, "/cloud/trade/v3/market/tickers", query)
	if err != nil {
		return nil, err
	}

	var resp zoomexResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal zoomex response: %w", err)
	}

	if resp.RetCode != 0 {
		return nil, fmt.Errorf("zoomex API error: %d - %s", resp.RetCode, resp.RetMsg)
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
	for i := range resp.Result.List {
		item := &resp.Result.List[i]
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
		settleTime, _ := strconv.ParseInt(item.NextFundingTime, 10, 64)
		price, _ := strconv.ParseFloat(item.LastPrice, 64)

		results = append(results, exchange.PotentialFundingResult{
			Symbol:     toStandardSymbol(item.Symbol),
			Rate:       rate,
			SettleTime: settleTime,
			Volume24h:  amt,
			Price:      price,
		})
	}

	return results, nil
}
