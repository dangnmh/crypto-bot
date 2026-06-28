package aster

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

type asterTicker struct {
	Symbol      string `json:"symbol"`
	LastPrice   string `json:"lastPrice"`
	QuoteVolume string `json:"quoteVolume"`
}

type asterPremiumIndex struct {
	Symbol          string `json:"symbol"`
	LastFundingRate string `json:"lastFundingRate"`
	NextFundingTime int64  `json:"nextFundingTime"`
}

func (c *Client) request(ctx context.Context, path string) ([]byte, error) {
	reqURL, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP GET %s: %w", path, err)
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

func toStandardSymbol(sym string) string {
	s := strings.ToUpper(sym)
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "_", "")
	return s
}

// GetPotentialFundingSymbols satisfies the ScannerClient interface.
func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	tickerBody, err := c.request(ctx, "/fapi/v1/ticker/24hr")
	if err != nil {
		return nil, fmt.Errorf("aster get tickers: %w", err)
	}

	var tickers []asterTicker
	if err := xjson.Unmarshal(tickerBody, &tickers); err != nil {
		return nil, fmt.Errorf("unmarshal aster tickers: %w", err)
	}

	indexBody, err := c.request(ctx, "/fapi/v1/premiumIndex")
	if err != nil {
		return nil, fmt.Errorf("aster get premium index: %w", err)
	}

	var indexes []asterPremiumIndex
	if err := xjson.Unmarshal(indexBody, &indexes); err != nil {
		return nil, fmt.Errorf("unmarshal aster premium index: %w", err)
	}

	indexMap := make(map[string]asterPremiumIndex)
	for i := range indexes {
		indexMap[indexes[i].Symbol] = indexes[i]
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
	for i := range tickers {
		ticker := &tickers[i]
		if res, ok := matchAndFilter(ticker, indexMap, whitelistMap, blacklistMap, minVol24h, maxVol24h); ok {
			results = append(results, res)
		}
	}

	return results, nil
}

func matchAndFilter(
	ticker *asterTicker,
	indexMap map[string]asterPremiumIndex,
	whitelistMap, blacklistMap map[string]bool,
	minVol24h, maxVol24h float64,
) (exchange.PotentialFundingResult, bool) {
	stdSym := toStandardSymbol(ticker.Symbol)
	if blacklistMap[stdSym] {
		return exchange.PotentialFundingResult{}, false
	}
	if len(whitelistMap) > 0 && !whitelistMap[stdSym] {
		return exchange.PotentialFundingResult{}, false
	}

	vol24h, _ := strconv.ParseFloat(ticker.QuoteVolume, 64)
	if minVol24h > 0 && vol24h < minVol24h {
		return exchange.PotentialFundingResult{}, false
	}
	if maxVol24h > 0 && vol24h > maxVol24h {
		return exchange.PotentialFundingResult{}, false
	}

	idxItem, ok := indexMap[ticker.Symbol]
	if !ok {
		return exchange.PotentialFundingResult{}, false
	}

	price, _ := strconv.ParseFloat(ticker.LastPrice, 64)
	rate, _ := strconv.ParseFloat(idxItem.LastFundingRate, 64)

	return exchange.PotentialFundingResult{
		Symbol:     stdSym,
		Rate:       rate,
		SettleTime: idxItem.NextFundingTime,
		Volume24h:  vol24h,
		Price:      price,
	}, true
}
