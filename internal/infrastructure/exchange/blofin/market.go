package blofin

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

type blofinInstrument struct {
	InstID   string `json:"instId"`
	InstType string `json:"instType"` // e.g. "SWAP"
}

type blofinInstrumentsResponse struct {
	Code string             `json:"code"`
	Msg  string             `json:"msg"`
	Data []blofinInstrument `json:"data"`
}

type blofinTicker struct {
	InstID         string       `json:"instId"`
	Last           xjson.Number `json:"last"`
	VolCurrency24h xjson.Number `json:"volCurrency24h"` // 24h volume in base asset
}

type blofinTickersResponse struct {
	Code string         `json:"code"`
	Msg  string         `json:"msg"`
	Data []blofinTicker `json:"data"`
}

type blofinFundingInfo struct {
	InstID      string       `json:"instId"`
	FundingRate xjson.Number `json:"fundingRate"`
	FundingTime xjson.Number `json:"fundingTime"`
}

type blofinFundingResponse struct {
	Code string              `json:"code"`
	Msg  string              `json:"msg"`
	Data []blofinFundingInfo `json:"data"`
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
	s = strings.ReplaceAll(s, "-PERP", "")
	s = strings.ReplaceAll(s, "-SWAP", "")
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "_", "")
	return s
}

func (c *Client) fetchPerpetualSymbols(ctx context.Context) (map[string]bool, error) {
	body, err := c.request(ctx, "/api/v1/market/instruments", nil)
	if err != nil {
		return nil, fmt.Errorf("blofin get instruments: %w", err)
	}

	var resp blofinInstrumentsResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal blofin instruments: %w", err)
	}

	perpSymbols := make(map[string]bool)
	for i := range resp.Data {
		item := &resp.Data[i]
		if strings.EqualFold(item.InstType, "SWAP") {
			perpSymbols[item.InstID] = true
		}
	}
	return perpSymbols, nil
}

func (c *Client) fetchTickers(ctx context.Context) ([]blofinTicker, error) {
	body, err := c.request(ctx, "/api/v1/market/tickers", nil)
	if err != nil {
		return nil, fmt.Errorf("blofin get tickers: %w", err)
	}

	var resp blofinTickersResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal blofin tickers: %w", err)
	}

	if resp.Code != "0" {
		return nil, fmt.Errorf("blofin tickers api error: code=%s msg=%s", resp.Code, resp.Msg)
	}
	return resp.Data, nil
}

func (c *Client) fetchFundingRates(ctx context.Context) (map[string]blofinFundingInfo, error) {
	body, err := c.request(ctx, "/api/v1/market/funding-rate", nil)
	if err != nil {
		return nil, fmt.Errorf("blofin get funding rates: %w", err)
	}

	var resp blofinFundingResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal blofin funding rates: %w", err)
	}

	if resp.Code != "0" {
		return nil, fmt.Errorf("blofin funding api error: code=%s msg=%s", resp.Code, resp.Msg)
	}

	fundingMap := make(map[string]blofinFundingInfo)
	for i := range resp.Data {
		fundingMap[resp.Data[i].InstID] = resp.Data[i]
	}
	return fundingMap, nil
}

func (c *Client) processTicker(
	ticker *blofinTicker,
	perpSymbols map[string]bool,
	fundingMap map[string]blofinFundingInfo,
	minVol24h, maxVol24h float64,
	whitelistMap map[string]bool,
	blacklistMap map[string]bool,
) (exchange.PotentialFundingResult, bool) {
	if !perpSymbols[ticker.InstID] {
		return exchange.PotentialFundingResult{}, false
	}

	stdSym := toStandardSymbol(ticker.InstID)
	if blacklistMap[stdSym] {
		return exchange.PotentialFundingResult{}, false
	}
	if len(whitelistMap) > 0 && !whitelistMap[stdSym] {
		return exchange.PotentialFundingResult{}, false
	}

	price, _ := ticker.Last.Float64()
	baseVol, _ := ticker.VolCurrency24h.Float64()
	vol24h := baseVol * price

	if minVol24h > 0 && vol24h < minVol24h {
		return exchange.PotentialFundingResult{}, false
	}
	if maxVol24h > 0 && vol24h > maxVol24h {
		return exchange.PotentialFundingResult{}, false
	}

	funding, ok := fundingMap[ticker.InstID]
	var rate float64
	var settleTime int64
	if ok {
		rate, _ = funding.FundingRate.Float64()
		settleTime = xjson.ToInt64(funding.FundingTime)
	}

	return exchange.PotentialFundingResult{
		Symbol:     stdSym,
		Rate:       rate,
		SettleTime: settleTime,
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
	perpSymbols, err := c.fetchPerpetualSymbols(ctx)
	if err != nil {
		return nil, err
	}

	tickers, err := c.fetchTickers(ctx)
	if err != nil {
		return nil, err
	}

	fundingMap, err := c.fetchFundingRates(ctx)
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
	for i := range tickers {
		res, ok := c.processTicker(&tickers[i], perpSymbols, fundingMap, minVol24h, maxVol24h, whitelistMap, blacklistMap)
		if ok {
			results = append(results, res)
		}
	}

	return results, nil
}
