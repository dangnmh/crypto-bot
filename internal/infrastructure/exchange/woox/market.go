package woox

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

type wooV3FutureInfo struct {
	Symbol          string       `json:"symbol"`
	MarkPrice       xjson.Number `json:"markPrice"`
	LastFundingRate xjson.Number `json:"lastFundingRate"`
	NextFundingTime int64        `json:"nextFundingTime"`
	Amount24h       xjson.Number `json:"24hAmount"` // 24h volume in USDT
}

type wooV3FuturesResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Rows []wooV3FutureInfo `json:"rows"`
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
	s = strings.ReplaceAll(s, "PERP_", "")
	s = strings.ReplaceAll(s, "SPOT_", "")
	s = strings.ReplaceAll(s, "-PERP", "")
	s = strings.ReplaceAll(s, "-SWAP", "")
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "_", "")
	return s
}

func (c *Client) fetchFuturesInfo(ctx context.Context) ([]wooV3FutureInfo, error) {
	futuresBody, err := c.request(ctx, "/v3/public/futures", nil)
	if err != nil {
		return nil, fmt.Errorf("woo get futures: %w", err)
	}

	var futResp wooV3FuturesResponse
	if err := xjson.Unmarshal(futuresBody, &futResp); err != nil {
		return nil, fmt.Errorf("unmarshal woo futures: %w", err)
	}

	if !futResp.Success {
		return nil, fmt.Errorf("woo futures api error")
	}

	return futResp.Data.Rows, nil
}

func (c *Client) processFutureInfo(
	fut *wooV3FutureInfo,
	minVol24h, maxVol24h float64,
	whitelistMap map[string]bool,
	blacklistMap map[string]bool,
) (exchange.PotentialFundingResult, bool) {
	// Only process perpetual swap symbols (usually prefixed with PERP_)
	if !strings.HasPrefix(strings.ToUpper(fut.Symbol), "PERP_") {
		return exchange.PotentialFundingResult{}, false
	}

	stdSym := toStandardSymbol(fut.Symbol)
	if blacklistMap[stdSym] {
		return exchange.PotentialFundingResult{}, false
	}
	if len(whitelistMap) > 0 && !whitelistMap[stdSym] {
		return exchange.PotentialFundingResult{}, false
	}

	vol24h, _ := fut.Amount24h.Float64()
	if minVol24h > 0 && vol24h < minVol24h {
		return exchange.PotentialFundingResult{}, false
	}
	if maxVol24h > 0 && vol24h > maxVol24h {
		return exchange.PotentialFundingResult{}, false
	}

	rate, _ := fut.LastFundingRate.Float64()
	price, _ := fut.MarkPrice.Float64()

	return exchange.PotentialFundingResult{
		Symbol:     stdSym,
		Rate:       rate,
		SettleTime: fut.NextFundingTime,
		Volume24h:  vol24h,
		Price:      price,
	}, true
}

func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	rows, err := c.fetchFuturesInfo(ctx)
	if err != nil {
		return nil, err
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
	for i := range rows {
		res, ok := c.processFutureInfo(&rows[i], minVol24h, maxVol24h, whitelistMap, blacklistMap)
		if ok {
			results = append(results, res)
		}
	}

	return results, nil
}
