package dydx

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type dydxMarketInfo struct {
	Ticker          string `json:"ticker"`
	Status          string `json:"status"`
	OraclePrice     string `json:"oraclePrice"`
	Volume24H       string `json:"volume24H"`
	NextFundingRate string `json:"nextFundingRate"`
}

type dydxResponse struct {
	Markets map[string]dydxMarketInfo `json:"markets"`
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
	s = strings.ReplaceAll(s, "-USD", "USDC")
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "_", "")
	return s
}

func getNextHourTime() int64 {
	now := time.Now().UTC()
	nextHour := now.Truncate(time.Hour).Add(time.Hour)
	return nextHour.UnixMilli()
}

// GetPotentialFundingSymbols satisfies the ScannerClient interface.
func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	body, err := c.request(ctx, "/v4/perpetualMarkets")
	if err != nil {
		return nil, fmt.Errorf("dydx get perpetual markets: %w", err)
	}

	var resp dydxResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal dydx markets: %w", err)
	}

	whitelistMap := make(map[string]bool)
	for _, sym := range whitelist {
		whitelistMap[toStandardSymbol(sym)] = true
	}

	blacklistMap := make(map[string]bool)
	for _, sym := range blacklist {
		blacklistMap[toStandardSymbol(sym)] = true
	}

	nextSettleTime := getNextHourTime()

	var results []exchange.PotentialFundingResult
	for _, item := range resp.Markets {
		if res, ok := matchAndFilter(&item, whitelistMap, blacklistMap, minVol24h, maxVol24h, nextSettleTime); ok {
			results = append(results, res)
		}
	}

	return results, nil
}

func matchAndFilter(
	item *dydxMarketInfo,
	whitelistMap, blacklistMap map[string]bool,
	minVol24h, maxVol24h float64,
	nextSettleTime int64,
) (exchange.PotentialFundingResult, bool) {
	if !strings.EqualFold(item.Status, "ACTIVE") {
		return exchange.PotentialFundingResult{}, false
	}

	stdSym := toStandardSymbol(item.Ticker)
	if blacklistMap[stdSym] {
		return exchange.PotentialFundingResult{}, false
	}
	if len(whitelistMap) > 0 && !whitelistMap[stdSym] {
		return exchange.PotentialFundingResult{}, false
	}

	vol24h, _ := strconv.ParseFloat(item.Volume24H, 64)
	if minVol24h > 0 && vol24h < minVol24h {
		return exchange.PotentialFundingResult{}, false
	}
	if maxVol24h > 0 && vol24h > maxVol24h {
		return exchange.PotentialFundingResult{}, false
	}

	price, _ := strconv.ParseFloat(item.OraclePrice, 64)
	rate, _ := strconv.ParseFloat(item.NextFundingRate, 64)

	return exchange.PotentialFundingResult{
		Symbol:     stdSym,
		Rate:       rate,
		SettleTime: nextSettleTime,
		Volume24h:  vol24h,
		Price:      price,
	}, true
}
