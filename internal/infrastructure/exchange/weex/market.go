package weex

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

type weexTicker struct {
	Symbol             string      `json:"symbol"`
	PriceChange        string      `json:"priceChange"`
	PriceChangePercent string      `json:"priceChangePercent"`
	LastPrice          string      `json:"lastPrice"`
	OpenPrice          string      `json:"openPrice"`
	HighPrice          string      `json:"highPrice"`
	LowPrice           string      `json:"lowPrice"`
	Volume             string      `json:"volume"`
	QuoteVolume        string      `json:"quoteVolume"`
	MarkPrice          string      `json:"markPrice"`
	IndexPrice         string      `json:"indexPrice"`
	OpenTime           json.Number `json:"openTime"`
	CloseTime          json.Number `json:"closeTime"`
}

type weexPremiumIndex struct {
	Symbol              string      `json:"symbol"`
	MarkPrice           string      `json:"markPrice"`
	IndexPrice          string      `json:"indexPrice"`
	LastFundingRate     string      `json:"lastFundingRate"`
	ForecastFundingRate string      `json:"forecastFundingRate"`
	InterestRate        string      `json:"interestRate"`
	NextFundingTime     json.Number `json:"nextFundingTime"`
	Time                json.Number `json:"time"`
	CollectCycle        json.Number `json:"collectCycle"`
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

	req.Header.Set("Content-Type", "application/json")

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
		query["symbol"] = strings.ToUpper(symbol)
	}

	body, err := c.request(ctx, http.MethodGet, "/capi/v3/market/ticker/24hr", query)
	if err != nil {
		return nil, err
	}

	var rawList []weexTicker
	if err := json.Unmarshal(body, &rawList); err != nil {
		return nil, fmt.Errorf("unmarshal tickers: %w", err)
	}

	tickers := make([]exchange.Ticker, 0, len(rawList))
	for i := range rawList {
		item := &rawList[i]
		last, _ := strconv.ParseFloat(item.LastPrice, 64)
		bid, _ := strconv.ParseFloat(item.LastPrice, 64) // Weex docs don't show best bid/ask, so fallback to lastPrice
		ask, _ := strconv.ParseFloat(item.LastPrice, 64)
		vol, _ := strconv.ParseFloat(item.Volume, 64)
		amt, _ := strconv.ParseFloat(item.QuoteVolume, 64)
		ts, _ := item.CloseTime.Int64()

		tickers = append(tickers, exchange.Ticker{
			Symbol:       strings.ToUpper(item.Symbol),
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

	body, err := c.request(ctx, http.MethodGet, "/capi/v3/market/premiumIndex", nil)
	if err != nil {
		return nil, err
	}

	var rawList []weexPremiumIndex
	if err := json.Unmarshal(body, &rawList); err != nil {
		return nil, fmt.Errorf("unmarshal funding rates: %w", err)
	}

	// Index rates by standardized symbol name
	rateMap := make(map[string]*weexPremiumIndex)
	for i := range rawList {
		item := &rawList[i]
		stdSym := strings.ToUpper(item.Symbol)
		rateMap[stdSym] = item
	}

	results := make([]exchange.FundingRateResult, 0, len(symbols))
	for _, sym := range symbols {
		stdSym := strings.ToUpper(sym)
		item, exists := rateMap[stdSym]
		if !exists {
			continue
		}

		rate, _ := strconv.ParseFloat(item.LastFundingRate, 64)
		settleTime, _ := item.NextFundingTime.Int64()

		results = append(results, exchange.FundingRateResult{
			Symbol:     sym,
			Rate:       rate,
			SettleTime: settleTime,
		})
	}

	return results, nil
}
