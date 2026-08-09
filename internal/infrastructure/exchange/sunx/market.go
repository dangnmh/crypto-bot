package sunx

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

type sunxFundingRateItem struct {
	FundingRate  string `json:"funding_rate"`
	ContractCode string `json:"contract_code"`
	Symbol       string `json:"symbol"`
	FeeAsset     string `json:"fee_asset"`
	FundingTime  string `json:"funding_time"`
}

type sunxFundingRateResponse struct {
	Status string                `json:"status"`
	Data   []sunxFundingRateItem `json:"data"`
}

type sunxTickerItem struct {
	ContractCode  string `json:"contract_code"`
	Close         string `json:"close"`
	TradeTurnover string `json:"trade_turnover"`
}

type sunxTickerResponse struct {
	Status string           `json:"status"`
	Ticks  []sunxTickerItem `json:"ticks"`
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

func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	ratesBody, err := c.request(ctx, "/sapi/v1/public/batch_funding_rate")
	if err != nil {
		return nil, fmt.Errorf("sunx list funding rates: %w", err)
	}
	var ratesResp sunxFundingRateResponse
	if err := xjson.Unmarshal(ratesBody, &ratesResp); err != nil {
		return nil, fmt.Errorf("unmarshal sunx funding rates: %w", err)
	}

	tickersBody, err := c.request(ctx, "/sapi/v1/market/detail/batch_merged")
	if err != nil {
		return nil, fmt.Errorf("sunx list tickers: %w", err)
	}
	var tickersResp sunxTickerResponse
	if err := xjson.Unmarshal(tickersBody, &tickersResp); err != nil {
		return nil, fmt.Errorf("unmarshal sunx tickers: %w", err)
	}

	tickerMap := make(map[string]*sunxTickerItem)
	for i := range tickersResp.Ticks {
		item := &tickersResp.Ticks[i]
		tickerMap[item.ContractCode] = item
	}

	results := c.filterFundingRates(&ratesResp, tickerMap, minVol24h, maxVol24h, whitelist, blacklist)
	return results, nil
}

func (c *Client) filterFundingRates(
	ratesResp *sunxFundingRateResponse,
	tickerMap map[string]*sunxTickerItem,
	minVol24h, maxVol24h float64,
	whitelist, blacklist []string,
) []exchange.PotentialFundingResult {
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
		if !strings.EqualFold(rateItem.FeeAsset, "USDT") && !strings.EqualFold(rateItem.FeeAsset, "USDC") {
			continue
		}
		stdSym := toStandardSymbol(rateItem.ContractCode)
		if blacklistMap[stdSym] {
			continue
		}
		if len(whitelistMap) > 0 && !whitelistMap[stdSym] {
			continue
		}

		ticker := tickerMap[rateItem.ContractCode]
		if ticker == nil {
			continue
		}

		vol24h, _ := strconv.ParseFloat(ticker.TradeTurnover, 64)
		if minVol24h > 0 && vol24h < minVol24h {
			continue
		}
		if maxVol24h > 0 && vol24h > maxVol24h {
			continue
		}

		price, _ := strconv.ParseFloat(ticker.Close, 64)
		rate, _ := strconv.ParseFloat(rateItem.FundingRate, 64)
		settleTimeVal, _ := strconv.ParseInt(rateItem.FundingTime, 10, 64)

		results = append(results, exchange.PotentialFundingResult{
			Symbol:     stdSym,
			Rate:       rate,
			SettleTime: settleTimeVal,
			Volume24h:  vol24h,
			Price:      price,
		})
	}
	return results
}

func (c *Client) GetTopGainer(_ context.Context, _ exchange.TopGainerRequest) ([]exchange.TopGainerResult, error) {
	return nil, nil
}
