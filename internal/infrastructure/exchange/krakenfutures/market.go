package krakenfutures

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

type krakenTickersResponse struct {
	Result     string         `json:"result"`
	Tickers    []krakenTicker `json:"tickers"`
	ServerTime string         `json:"serverTime"`
}

type krakenTicker struct {
	Tag         string  `json:"tag"`
	Pair        string  `json:"pair"`
	Symbol      string  `json:"symbol"`
	MarkPrice   float64 `json:"markPrice"`
	Vol24h      float64 `json:"vol24h"`
	VolumeQuote float64 `json:"volumeQuote"`
	FundingRate float64 `json:"fundingRate"`
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

// toStandardSymbol maps e.g. "PF_XBTUSD" -> "BTCUSD".
func toStandardSymbol(s string) string {
	upper := strings.ToUpper(s)
	upper = strings.TrimPrefix(upper, "PF_")
	upper = strings.ReplaceAll(upper, "XBT", "BTC")
	upper = strings.ReplaceAll(upper, ":", "")
	upper = strings.ReplaceAll(upper, "_", "")
	upper = strings.ReplaceAll(upper, "-", "")
	return upper
}

func isPerpetual(item *krakenTicker) bool {
	return strings.EqualFold(item.Tag, "perpetual") || strings.HasPrefix(strings.ToUpper(item.Symbol), "PF_")
}

func calculateSettleTime(serverTime string) int64 {
	if serverTime == "" {
		return 0
	}
	parsedTime, err := time.Parse(time.RFC3339, serverTime)
	if err != nil {
		return 0
	}
	return parsedTime.Truncate(time.Hour).Add(time.Hour).UnixMilli()
}

// GetPotentialFundingSymbols fetches all perpetual instruments, their 24h volumes and predicted funding rates in exactly one call.
func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	body, err := c.request(ctx, http.MethodGet, "/derivatives/api/v3/tickers", nil)
	if err != nil {
		return nil, fmt.Errorf("krakenfutures get tickers: %w", err)
	}

	var resp krakenTickersResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal kraken tickers: %w", err)
	}

	whitelistMap := make(map[string]bool)
	for _, sym := range whitelist {
		whitelistMap[toStandardSymbol(sym)] = true
	}

	blacklistMap := make(map[string]bool)
	for _, sym := range blacklist {
		blacklistMap[toStandardSymbol(sym)] = true
	}

	settleTime := calculateSettleTime(resp.ServerTime)

	var results []exchange.PotentialFundingResult
	for i := range resp.Tickers {
		item := &resp.Tickers[i]

		if !isPerpetual(item) {
			continue
		}

		stdSym := toStandardSymbol(item.Symbol)

		if blacklistMap[stdSym] {
			continue
		}
		if len(whitelistMap) > 0 && !whitelistMap[stdSym] {
			continue
		}

		vol := item.VolumeQuote
		if vol < minVol24h {
			continue
		}
		if maxVol24h > 0 && vol > maxVol24h {
			continue
		}

		results = append(results, exchange.PotentialFundingResult{
			Symbol:     stdSym,
			Rate:       item.FundingRate / 100,
			SettleTime: settleTime,
			Volume24h:  vol,
			Price:      item.MarkPrice,
		})
	}

	return results, nil
}

// GetTickers returns tickers.
func (c *Client) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	return nil, fmt.Errorf("GetTickers not implemented for krakenfutures")
}

// GetFundingRates returns funding rates.
func (c *Client) GetFundingRates(ctx context.Context, symbols []string) ([]exchange.FundingRateResult, error) {
	return nil, fmt.Errorf("GetFundingRates not implemented for krakenfutures")
}

// GetContractDetails returns contract details.
func (c *Client) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	return nil, fmt.Errorf("GetContractDetails not implemented for krakenfutures")
}

// GetServerTime returns current exchange time.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	return time.Now().UnixMilli(), nil
}
