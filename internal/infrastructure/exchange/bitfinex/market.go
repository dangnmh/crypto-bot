package bitfinex

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type bitfinexTickerItem []any
type bitfinexDerivStatusItem []any

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
	s = strings.TrimPrefix(s, "T")
	s = strings.ReplaceAll(s, "F0:USTF0", "USDT")
	s = strings.ReplaceAll(s, "F0:BTCF0", "BTC")
	s = strings.ReplaceAll(s, "F0:ETHF0", "ETH")
	s = strings.ReplaceAll(s, ":", "")
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "_", "")
	return s
}

func parseString(val any) string {
	if s, ok := val.(string); ok {
		return s
	}
	return ""
}

func parseFloat(val any) float64 {
	if f, ok := val.(float64); ok {
		return f
	}
	return 0
}

func parseInt64(val any) int64 {
	if f, ok := val.(float64); ok {
		return int64(f)
	}
	return 0
}

// GetPotentialFundingSymbols satisfies the ScannerClient interface.
func (c *Client) getFuturesSymbols(ctx context.Context) ([]string, error) {
	configBody, err := c.request(ctx, "/v2/conf/pub:list:pair:futures", nil)
	if err != nil {
		return nil, fmt.Errorf("bitfinex get config pairs: %w", err)
	}

	var pairs [][]string
	if err := xjson.Unmarshal(configBody, &pairs); err != nil {
		return nil, fmt.Errorf("unmarshal config pairs: %w", err)
	}

	if len(pairs) == 0 || len(pairs[0]) == 0 {
		return nil, fmt.Errorf("no futures pairs returned from config")
	}

	var symbols []string
	for _, pair := range pairs[0] {
		// Only select symbols with Tether USTF0 base (i.e. USDT linear swaps)
		if strings.HasSuffix(pair, ":USTF0") {
			symbols = append(symbols, "t"+pair)
		}
	}
	return symbols, nil
}

// GetPotentialFundingSymbols satisfies the ScannerClient interface.
func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	symbols, err := c.getFuturesSymbols(ctx)
	if err != nil {
		return nil, err
	}

	if len(symbols) == 0 {
		return nil, nil
	}

	symsParam := strings.Join(symbols, ",")

	tickersBody, err := c.request(ctx, "/v2/tickers", map[string]string{"symbols": symsParam})
	if err != nil {
		return nil, fmt.Errorf("bitfinex get tickers: %w", err)
	}

	var tickers []bitfinexTickerItem
	if err := xjson.Unmarshal(tickersBody, &tickers); err != nil {
		return nil, fmt.Errorf("unmarshal tickers: %w", err)
	}

	statusBody, err := c.request(ctx, "/v2/status/deriv", map[string]string{"keys": symsParam})
	if err != nil {
		return nil, fmt.Errorf("bitfinex get status: %w", err)
	}

	var statuses []bitfinexDerivStatusItem
	if err := xjson.Unmarshal(statusBody, &statuses); err != nil {
		return nil, fmt.Errorf("unmarshal statuses: %w", err)
	}

	statusMap := make(map[string]bitfinexDerivStatusItem)
	for i := range statuses {
		item := statuses[i]
		if len(item) > 0 {
			if sym := parseString(item[0]); sym != "" {
				statusMap[sym] = item
			}
		}
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
		ticker := tickers[i]
		if res, ok := matchAndFilter(ticker, statusMap, whitelistMap, blacklistMap, minVol24h, maxVol24h); ok {
			results = append(results, res)
		}
	}

	return results, nil
}

func matchAndFilter(
	ticker bitfinexTickerItem,
	statusMap map[string]bitfinexDerivStatusItem,
	whitelistMap, blacklistMap map[string]bool,
	minVol24h, maxVol24h float64,
) (exchange.PotentialFundingResult, bool) {
	if len(ticker) < 9 {
		return exchange.PotentialFundingResult{}, false
	}

	sym := parseString(ticker[0])
	stdSym := toStandardSymbol(sym)
	if blacklistMap[stdSym] {
		return exchange.PotentialFundingResult{}, false
	}
	if len(whitelistMap) > 0 && !whitelistMap[stdSym] {
		return exchange.PotentialFundingResult{}, false
	}

	price := parseFloat(ticker[7])
	rawVol := parseFloat(ticker[8])
	vol24h := rawVol * price

	if minVol24h > 0 && vol24h < minVol24h {
		return exchange.PotentialFundingResult{}, false
	}
	if maxVol24h > 0 && vol24h > maxVol24h {
		return exchange.PotentialFundingResult{}, false
	}

	statusItem, ok := statusMap[sym]
	if !ok || len(statusItem) < 10 {
		return exchange.PotentialFundingResult{}, false
	}

	settleTime := parseInt64(statusItem[8])
	rate := parseFloat(statusItem[9])

	return exchange.PotentialFundingResult{
		Symbol:     stdSym,
		Rate:       rate,
		SettleTime: settleTime,
		Volume24h:  vol24h,
		Price:      price,
	}, true
}

func (c *Client) GetTopGainer(_ context.Context, _ exchange.TopGainerRequest) ([]exchange.TopGainerResult, error) {
	return nil, nil
}
