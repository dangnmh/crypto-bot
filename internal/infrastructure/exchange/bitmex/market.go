package bitmex

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type bitmexInstrument struct {
	Symbol             string  `json:"symbol"`
	Typ                string  `json:"typ"`
	State              string  `json:"state"`
	QuoteCurrency      string  `json:"quoteCurrency"`
	LastPrice          float64 `json:"lastPrice"`
	ForeignNotional24h float64 `json:"foreignNotional24h"`
	FundingRate        float64 `json:"fundingRate"`
	FundingTimestamp   string  `json:"fundingTimestamp"`
}

func (c *Client) request(ctx context.Context, path string, query map[string]string) ([]byte, error) {
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
	s = strings.ReplaceAll(s, "XBT", "BTC")
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
	body, err := c.request(ctx, "/api/v1/instrument", map[string]string{"filter": `{"state":"Open"}`})
	if err != nil {
		return nil, fmt.Errorf("bitmex get instruments: %w", err)
	}

	var instruments []bitmexInstrument
	if err := xjson.Unmarshal(body, &instruments); err != nil {
		return nil, fmt.Errorf("unmarshal bitmex instruments: %w", err)
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
	for i := range instruments {
		item := &instruments[i]
		if res, ok := matchAndFilter(item, whitelistMap, blacklistMap, minVol24h, maxVol24h); ok {
			results = append(results, res)
		}
	}

	return results, nil
}

func matchAndFilter(
	item *bitmexInstrument,
	whitelistMap, blacklistMap map[string]bool,
	minVol24h, maxVol24h float64,
) (exchange.PotentialFundingResult, bool) {
	// typ == "FFWCSX" is the perpetual swap indicator on BitMEX
	if !strings.EqualFold(item.Typ, "FFWCSX") || !strings.EqualFold(item.State, "Open") {
		return exchange.PotentialFundingResult{}, false
	}
	if !strings.EqualFold(item.QuoteCurrency, "USDT") && !strings.EqualFold(item.QuoteCurrency, "USD") && !strings.EqualFold(item.QuoteCurrency, "USDC") {
		return exchange.PotentialFundingResult{}, false
	}

	stdSym := toStandardSymbol(item.Symbol)
	if blacklistMap[stdSym] {
		return exchange.PotentialFundingResult{}, false
	}
	if len(whitelistMap) > 0 && !whitelistMap[stdSym] {
		return exchange.PotentialFundingResult{}, false
	}

	vol24h := item.ForeignNotional24h
	if minVol24h > 0 && vol24h < minVol24h {
		return exchange.PotentialFundingResult{}, false
	}
	if maxVol24h > 0 && vol24h > maxVol24h {
		return exchange.PotentialFundingResult{}, false
	}

	settleTime := int64(0)
	if item.FundingTimestamp != "" {
		if t, err := time.Parse(time.RFC3339, item.FundingTimestamp); err == nil {
			settleTime = t.UnixMilli()
		}
	}

	return exchange.PotentialFundingResult{
		Symbol:     stdSym,
		Rate:       item.FundingRate,
		SettleTime: settleTime,
		Volume24h:  vol24h,
		Price:      item.LastPrice,
	}, true
}
