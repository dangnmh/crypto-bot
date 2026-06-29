package digifinex

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type digifinexInstrument struct {
	InstrumentID string `json:"instrument_id"`
	Status       string `json:"status"` // e.g. "ONLINE"
}

type digifinexInstrumentsResponse struct {
	Code int                   `json:"code"`
	Msg  string                `json:"msg"`
	Data []digifinexInstrument `json:"data"`
}

type digifinexTicker struct {
	InstrumentID string       `json:"instrument_id"`
	Last         xjson.Number `json:"last"`
	Volume24h    xjson.Number `json:"volume_24h"` // 24h volume in USDT
	Timestamp    int64        `json:"timestamp"`
}

type digifinexTickersResponse struct {
	Code int               `json:"code"`
	Msg  string            `json:"msg"`
	Data []digifinexTicker `json:"data"`
}

type digifinexFundingInfo struct {
	InstrumentID    string       `json:"instrument_id"`
	FundingRate     xjson.Number `json:"funding_rate"`
	FundingTime     int64        `json:"funding_time"`
	NextFundingRate xjson.Number `json:"next_funding_rate"`
	NextFundingTime int64        `json:"next_funding_time"`
}

type digifinexFundingResponse struct {
	Code int                  `json:"code"`
	Msg  string               `json:"msg"`
	Data digifinexFundingInfo `json:"data"`
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
	s = strings.ReplaceAll(s, "USDTPERP", "")
	s = strings.ReplaceAll(s, "USDPERP", "")
	s = strings.ReplaceAll(s, "-PERP", "")
	s = strings.ReplaceAll(s, "-SWAP", "")
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "_", "")
	return s
}

func (c *Client) fetchInstruments(ctx context.Context) (map[string]bool, error) {
	body, err := c.request(ctx, "/swap/v2/public/instruments", nil)
	if err != nil {
		return nil, fmt.Errorf("digifinex get instruments: %w", err)
	}

	var resp digifinexInstrumentsResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal digifinex instruments: %w", err)
	}

	if resp.Code != 0 {
		return nil, fmt.Errorf("digifinex instruments api error: code=%d msg=%s", resp.Code, resp.Msg)
	}

	activeInstruments := make(map[string]bool)
	for i := range resp.Data {
		item := &resp.Data[i]
		if strings.EqualFold(item.Status, "ONLINE") {
			activeInstruments[item.InstrumentID] = true
		}
	}
	return activeInstruments, nil
}

func (c *Client) fetchTickers(ctx context.Context) ([]digifinexTicker, error) {
	body, err := c.request(ctx, "/swap/v2/public/tickers", nil)
	if err != nil {
		return nil, fmt.Errorf("digifinex get tickers: %w", err)
	}

	var resp digifinexTickersResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal digifinex tickers: %w", err)
	}

	if resp.Code != 0 {
		return nil, fmt.Errorf("digifinex tickers api error: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return resp.Data, nil
}

func (c *Client) fetchFundingRateForSymbol(ctx context.Context, instrumentID string) (*digifinexFundingInfo, error) {
	body, err := c.request(ctx, "/swap/v2/public/funding_rate", map[string]string{
		"instrument_id": instrumentID,
	})
	if err != nil {
		return nil, fmt.Errorf("digifinex get funding rate for %s: %w", instrumentID, err)
	}

	var resp digifinexFundingResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal funding rate: %w", err)
	}

	if resp.Code != 0 {
		return nil, fmt.Errorf("funding rate api error: code=%d msg=%s", resp.Code, resp.Msg)
	}

	return &resp.Data, nil
}

type digifinexCandidate struct {
	ticker    *digifinexTicker
	stdSym    string
	price     float64
	volume24h float64
}

func (c *Client) filterCandidates(
	tickers []digifinexTicker,
	activeInstruments map[string]bool,
	minVol24h, maxVol24h float64,
	whitelistMap, blacklistMap map[string]bool,
) []digifinexCandidate {
	var candidates []digifinexCandidate
	for i := range tickers {
		ticker := &tickers[i]
		if !activeInstruments[ticker.InstrumentID] {
			continue
		}

		stdSym := toStandardSymbol(ticker.InstrumentID)
		if blacklistMap[stdSym] {
			continue
		}
		if len(whitelistMap) > 0 && !whitelistMap[stdSym] {
			continue
		}

		price, _ := ticker.Last.Float64()
		vol24h, _ := ticker.Volume24h.Float64()

		if minVol24h > 0 && vol24h < minVol24h {
			continue
		}
		if maxVol24h > 0 && vol24h > maxVol24h {
			continue
		}

		candidates = append(candidates, digifinexCandidate{
			ticker:    ticker,
			stdSym:    stdSym,
			price:     price,
			volume24h: vol24h,
		})
	}
	return candidates
}

func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	activeInstruments, err := c.fetchInstruments(ctx)
	if err != nil {
		return nil, err
	}

	tickers, err := c.fetchTickers(ctx)
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

	candidates := c.filterCandidates(tickers, activeInstruments, minVol24h, maxVol24h, whitelistMap, blacklistMap)

	// Sort candidates by volume descending
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].volume24h > candidates[j].volume24h
	})

	// Limit to top 30 symbols to avoid hitting rate limits
	limit := min(len(candidates), 30)

	var results []exchange.PotentialFundingResult
	for i := range limit {
		cand := candidates[i]
		val, err := c.fetchFundingRateForSymbol(ctx, cand.ticker.InstrumentID)
		var rate float64
		var settleTime int64

		if err == nil {
			rate, _ = val.FundingRate.Float64()
			settleTime = val.NextFundingTime
		} else {
			c.logger.Error("Failed to fetch funding rate for DigiFinex symbol", "symbol", cand.ticker.InstrumentID, "error", err)
		}

		results = append(results, exchange.PotentialFundingResult{
			Symbol:     cand.stdSym,
			Rate:       rate,
			SettleTime: settleTime,
			Volume24h:  cand.volume24h,
			Price:      cand.price,
		})
	}

	return results, nil
}
