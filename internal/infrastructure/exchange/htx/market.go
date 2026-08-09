package htx

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

type htxFundingRateItem struct {
	FundingRate  string `json:"funding_rate"`
	ContractCode string `json:"contract_code"`
	Symbol       string `json:"symbol"`
	FeeAsset     string `json:"fee_asset"`
	FundingTime  string `json:"funding_time"`
}

type htxFundingRateResponse struct {
	Status string               `json:"status"`
	Data   []htxFundingRateItem `json:"data"`
}

type htxTickerItem struct {
	ContractCode  string `json:"contract_code"`
	Close         string `json:"close"`
	TradeTurnover string `json:"trade_turnover"`
}

type htxTickerResponse struct {
	Status string          `json:"status"`
	Ticks  []htxTickerItem `json:"ticks"`
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
	ratesBody, err := c.request(ctx, "/linear-swap-api/v1/swap_batch_funding_rate")
	if err != nil {
		return nil, fmt.Errorf("htx list funding rates: %w", err)
	}

	var ratesResp htxFundingRateResponse
	if err := xjson.Unmarshal(ratesBody, &ratesResp); err != nil {
		return nil, fmt.Errorf("unmarshal htx funding rates: %w", err)
	}

	tickersBody, err := c.request(ctx, "/linear-swap-ex/market/detail/batch_merged")
	if err != nil {
		return nil, fmt.Errorf("htx list tickers: %w", err)
	}

	var tickersResp htxTickerResponse
	if err := xjson.Unmarshal(tickersBody, &tickersResp); err != nil {
		return nil, fmt.Errorf("unmarshal htx tickers: %w", err)
	}

	tickerMap := make(map[string]*htxTickerItem)
	for i := range tickersResp.Ticks {
		item := &tickersResp.Ticks[i]
		tickerMap[item.ContractCode] = item
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
	for i := range ratesResp.Data {
		rateItem := &ratesResp.Data[i]
		if res, ok := matchAndFilter(rateItem, tickerMap, whitelistMap, blacklistMap, minVol24h, maxVol24h); ok {
			results = append(results, res)
		}
	}

	return results, nil
}

func matchAndFilter(
	rateItem *htxFundingRateItem,
	tickerMap map[string]*htxTickerItem,
	whitelistMap, blacklistMap map[string]bool,
	minVol24h, maxVol24h float64,
) (exchange.PotentialFundingResult, bool) {
	// Filter fee asset USDT or USDC
	if !strings.EqualFold(rateItem.FeeAsset, "USDT") && !strings.EqualFold(rateItem.FeeAsset, "USDC") {
		return exchange.PotentialFundingResult{}, false
	}

	stdSym := toStandardSymbol(rateItem.ContractCode)
	if blacklistMap[stdSym] {
		return exchange.PotentialFundingResult{}, false
	}
	if len(whitelistMap) > 0 && !whitelistMap[stdSym] {
		return exchange.PotentialFundingResult{}, false
	}

	ticker := tickerMap[rateItem.ContractCode]
	if ticker == nil {
		return exchange.PotentialFundingResult{}, false
	}

	vol24h, _ := strconv.ParseFloat(ticker.TradeTurnover, 64)
	if minVol24h > 0 && vol24h < minVol24h {
		return exchange.PotentialFundingResult{}, false
	}
	if maxVol24h > 0 && vol24h > maxVol24h {
		return exchange.PotentialFundingResult{}, false
	}

	price, _ := strconv.ParseFloat(ticker.Close, 64)
	rate, _ := strconv.ParseFloat(rateItem.FundingRate, 64)
	settleTimeVal, _ := strconv.ParseInt(rateItem.FundingTime, 10, 64)

	return exchange.PotentialFundingResult{
		Symbol:     stdSym,
		Rate:       rate,
		SettleTime: settleTimeVal,
		Volume24h:  vol24h,
		Price:      price,
	}, true
}

func (c *Client) GetTopGainer(_ context.Context, _ exchange.TopGainerRequest) ([]exchange.TopGainerResult, error) {
	return nil, nil
}
