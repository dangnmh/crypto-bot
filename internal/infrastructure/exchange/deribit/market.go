package deribit

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

type deribitBookSummaryItem struct {
	InstrumentName string  `json:"instrument_name"`
	Last           float64 `json:"last"`
	VolumeUSD      float64 `json:"volume_usd"`
	Funding8h      float64 `json:"funding_8h"`
}

type deribitBookSummaryResponse struct {
	Result []deribitBookSummaryItem `json:"result"`
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
	s = strings.TrimSuffix(s, "-PERPETUAL")
	s = strings.ReplaceAll(s, "_", "")
	s = strings.ReplaceAll(s, "-", "")
	return s
}

func getNextFundingTime() int64 {
	now := time.Now().UTC()
	hour := now.Hour()
	var nextHour int
	switch {
	case hour < 8:
		nextHour = 8
	case hour < 16:
		nextHour = 16
	default:
		nextHour = 24
	}
	// Note: nextHour = 24 triggers Date constructor to automatically roll over to next day.
	nextSettle := time.Date(now.Year(), now.Month(), now.Day(), nextHour, 0, 0, 0, time.UTC)
	return nextSettle.UnixMilli()
}

// GetPotentialFundingSymbols satisfies the ScannerClient interface.
func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	query := map[string]string{
		"currency": "USDC",
	}
	body, err := c.request(ctx, "/api/v2/public/get_book_summary_by_currency", query)
	if err != nil {
		return nil, fmt.Errorf("deribit get book summary: %w", err)
	}

	var resp deribitBookSummaryResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal deribit summary: %w", err)
	}

	whitelistMap := make(map[string]bool)
	for _, sym := range whitelist {
		whitelistMap[toStandardSymbol(sym)] = true
	}

	blacklistMap := make(map[string]bool)
	for _, sym := range blacklist {
		blacklistMap[toStandardSymbol(sym)] = true
	}

	nextSettleTime := getNextFundingTime()

	var results []exchange.PotentialFundingResult
	for i := range resp.Result {
		item := &resp.Result[i]
		if res, ok := matchAndFilter(item, whitelistMap, blacklistMap, minVol24h, maxVol24h, nextSettleTime); ok {
			results = append(results, res)
		}
	}

	return results, nil
}

func matchAndFilter(
	item *deribitBookSummaryItem,
	whitelistMap, blacklistMap map[string]bool,
	minVol24h, maxVol24h float64,
	nextSettleTime int64,
) (exchange.PotentialFundingResult, bool) {
	if !strings.HasSuffix(item.InstrumentName, "-PERPETUAL") {
		return exchange.PotentialFundingResult{}, false
	}

	stdSym := toStandardSymbol(item.InstrumentName)
	if blacklistMap[stdSym] {
		return exchange.PotentialFundingResult{}, false
	}
	if len(whitelistMap) > 0 && !whitelistMap[stdSym] {
		return exchange.PotentialFundingResult{}, false
	}

	vol24h := item.VolumeUSD
	if minVol24h > 0 && vol24h < minVol24h {
		return exchange.PotentialFundingResult{}, false
	}
	if maxVol24h > 0 && vol24h > maxVol24h {
		return exchange.PotentialFundingResult{}, false
	}

	return exchange.PotentialFundingResult{
		Symbol:     stdSym,
		Rate:       item.Funding8h,
		SettleTime: nextSettleTime,
		Volume24h:  vol24h,
		Price:      item.Last,
	}, true
}

func (c *Client) GetTopGainer(_ context.Context, _ exchange.TopGainerRequest) ([]exchange.TopGainerResult, error) {
	return nil, nil
}
