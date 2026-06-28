package orangex

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

type orangexContract struct {
	TickerID                 string `json:"ticker_id"`
	BaseCurrency             string `json:"base_currency"`
	TargetCurrency           string `json:"target_currency"`
	LastPrice                string `json:"last_price"`
	TargetVolume             string `json:"target_volume"`
	ProductType              string `json:"product_type"`
	FundingRate              string `json:"funding_rate"`
	NextFundingRateTimestamp int64  `json:"next_funding_rate_timestamp"`
}

type orangexResponse struct {
	JsonRpc string            `json:"jsonrpc"`
	Result  []orangexContract `json:"result"`
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
	s = strings.TrimSuffix(s, "-PERPETUAL")
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
	body, err := c.request(ctx, "/public/coin_gecko_contracts")
	if err != nil {
		return nil, fmt.Errorf("orangex list coin gecko contracts: %w", err)
	}

	var resp orangexResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal orangex contracts: %w", err)
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
	for i := range resp.Result {
		item := &resp.Result[i]
		if res, ok := matchAndFilter(item, whitelistMap, blacklistMap, minVol24h, maxVol24h); ok {
			results = append(results, res)
		}
	}

	return results, nil
}

func matchAndFilter(
	item *orangexContract,
	whitelistMap, blacklistMap map[string]bool,
	minVol24h, maxVol24h float64,
) (exchange.PotentialFundingResult, bool) {
	if !strings.EqualFold(item.ProductType, "perpetual") {
		return exchange.PotentialFundingResult{}, false
	}
	if !strings.EqualFold(item.TargetCurrency, "USDT") && !strings.EqualFold(item.TargetCurrency, "USDC") {
		return exchange.PotentialFundingResult{}, false
	}

	stdSym := toStandardSymbol(item.TickerID)
	if blacklistMap[stdSym] {
		return exchange.PotentialFundingResult{}, false
	}
	if len(whitelistMap) > 0 && !whitelistMap[stdSym] {
		return exchange.PotentialFundingResult{}, false
	}

	vol24h, _ := strconv.ParseFloat(item.TargetVolume, 64)
	if minVol24h > 0 && vol24h < minVol24h {
		return exchange.PotentialFundingResult{}, false
	}
	if maxVol24h > 0 && vol24h > maxVol24h {
		return exchange.PotentialFundingResult{}, false
	}

	price, _ := strconv.ParseFloat(item.LastPrice, 64)
	rate, _ := strconv.ParseFloat(item.FundingRate, 64)

	return exchange.PotentialFundingResult{
		Symbol:     stdSym,
		Rate:       rate,
		SettleTime: item.NextFundingRateTimestamp,
		Volume24h:  vol24h,
		Price:      price,
	}, true
}
