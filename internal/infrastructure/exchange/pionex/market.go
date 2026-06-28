package pionex

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

type pionexIndexItem struct {
	Symbol          string `json:"symbol"`
	IndexPrice      string `json:"indexPrice"`
	MarkPrice       string `json:"markPrice"`
	NextFundingRate string `json:"nextFundingRate"`
	NextFundingTime int64  `json:"nextFundingTime"`
	UpdateTime      int64  `json:"updateTime"`
}

type pionexIndexesResponse struct {
	Result bool `json:"result"`
	Data   struct {
		Indexes []pionexIndexItem `json:"indexes"`
	} `json:"data"`
}

type pionexTickerItem struct {
	Symbol string `json:"symbol"`
	Close  string `json:"close"`
	Volume string `json:"volume"`
	Amount string `json:"amount"`
}

type pionexTickersResponse struct {
	Result bool `json:"result"`
	Data   struct {
		Tickers []pionexTickerItem `json:"tickers"`
	} `json:"data"`
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
	s = strings.TrimSuffix(s, "_PERP")
	s = strings.ReplaceAll(s, "_", "")
	s = strings.ReplaceAll(s, "-", "")
	return s
}

// GetPotentialFundingSymbols satisfies the ScannerClient interface.
func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	indexesBody, err := c.request(ctx, "/api/v1/market/indexes", nil)
	if err != nil {
		return nil, fmt.Errorf("pionex list indexes: %w", err)
	}

	var indexesResp pionexIndexesResponse
	if err := xjson.Unmarshal(indexesBody, &indexesResp); err != nil {
		return nil, fmt.Errorf("unmarshal pionex indexes: %w", err)
	}

	if !indexesResp.Result {
		return nil, fmt.Errorf("pionex api error: failed to fetch indexes")
	}

	query := map[string]string{
		"type": "PERP",
	}
	tickersBody, err := c.request(ctx, "/api/v1/market/tickers", query)
	if err != nil {
		return nil, fmt.Errorf("pionex list tickers: %w", err)
	}

	var tickersResp pionexTickersResponse
	if err := xjson.Unmarshal(tickersBody, &tickersResp); err != nil {
		return nil, fmt.Errorf("unmarshal pionex tickers: %w", err)
	}

	if !tickersResp.Result {
		return nil, fmt.Errorf("pionex api error: failed to fetch tickers")
	}

	tickerMap := make(map[string]*pionexTickerItem)
	for i := range tickersResp.Data.Tickers {
		item := &tickersResp.Data.Tickers[i]
		tickerMap[item.Symbol] = item
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
	for i := range indexesResp.Data.Indexes {
		item := &indexesResp.Data.Indexes[i]
		if res, ok := matchAndFilter(item, tickerMap, whitelistMap, blacklistMap, minVol24h, maxVol24h); ok {
			results = append(results, res)
		}
	}

	return results, nil
}

func matchAndFilter(
	indexItem *pionexIndexItem,
	tickerMap map[string]*pionexTickerItem,
	whitelistMap, blacklistMap map[string]bool,
	minVol24h, maxVol24h float64,
) (exchange.PotentialFundingResult, bool) {
	// Only support USDT/USDC perpetuals
	if !strings.HasSuffix(indexItem.Symbol, "_USDT_PERP") && !strings.HasSuffix(indexItem.Symbol, "_USDC_PERP") {
		return exchange.PotentialFundingResult{}, false
	}

	stdSym := toStandardSymbol(indexItem.Symbol)
	if blacklistMap[stdSym] {
		return exchange.PotentialFundingResult{}, false
	}
	if len(whitelistMap) > 0 && !whitelistMap[stdSym] {
		return exchange.PotentialFundingResult{}, false
	}

	ticker := tickerMap[indexItem.Symbol]
	if ticker == nil {
		return exchange.PotentialFundingResult{}, false
	}

	vol24h, _ := strconv.ParseFloat(ticker.Amount, 64)
	if minVol24h > 0 && vol24h < minVol24h {
		return exchange.PotentialFundingResult{}, false
	}
	if maxVol24h > 0 && vol24h > maxVol24h {
		return exchange.PotentialFundingResult{}, false
	}

	price, _ := strconv.ParseFloat(ticker.Close, 64)
	if price == 0 {
		price, _ = strconv.ParseFloat(indexItem.MarkPrice, 64)
	}

	rate, _ := strconv.ParseFloat(indexItem.NextFundingRate, 64)

	return exchange.PotentialFundingResult{
		Symbol:     stdSym,
		Rate:       rate,
		SettleTime: indexItem.NextFundingTime,
		Volume24h:  vol24h,
		Price:      price,
	}, true
}
