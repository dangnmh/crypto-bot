package bydfi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type bydfiInstrument struct {
	Symbol         string       `json:"symbol"`
	ContractFactor xjson.Number `json:"contractFactor"`
	Status         string       `json:"status"` // e.g. "NORMAL"
}

type bydfiExchangeInfoResponse struct {
	Code int               `json:"code"`
	Data []bydfiInstrument `json:"data"`
}

type bydfiTicker struct {
	Symbol string       `json:"symbol"`
	Last   xjson.Number `json:"last"`
	Vol    xjson.Number `json:"vol"`
}

type bydfiTickersResponse struct {
	Code int           `json:"code"`
	Data []bydfiTicker `json:"data"`
}

type bydfiFundingRateInfo struct {
	Symbol          string       `json:"symbol"`
	LastFundingRate xjson.Number `json:"lastFundingRate"`
	NextFundingTime xjson.Number `json:"nextFundingTime"`
}

type bydfiFundingRateResponse struct {
	Code int                  `json:"code"`
	Data bydfiFundingRateInfo `json:"data"`
}

func (c *Client) request(ctx context.Context, path string, query map[string]string) ([]byte, error) {
	if c.limiter != nil {
		if err := c.limiter.Acquire(ctx, path); err != nil {
			return nil, fmt.Errorf("rate limit acquire: %w", err)
		}
	}

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

func (c *Client) fetchExchangeInfo(ctx context.Context) (map[string]float64, error) {
	body, err := c.request(ctx, "/v1/fapi/market/exchange_info", nil)
	if err != nil {
		return nil, fmt.Errorf("bydfi get exchange info: %w", err)
	}

	var resp bydfiExchangeInfoResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal bydfi exchange info: %w", err)
	}

	factors := make(map[string]float64)
	for i := range resp.Data {
		item := &resp.Data[i]
		if strings.EqualFold(item.Status, "NORMAL") {
			factor, _ := item.ContractFactor.Float64()
			if factor == 0 {
				factor = 1.0 // Fallback
			}
			factors[item.Symbol] = factor
		}
	}
	return factors, nil
}

func (c *Client) fetchTickers(ctx context.Context) ([]bydfiTicker, error) {
	body, err := c.request(ctx, "/v1/fapi/market/ticker/24hr", nil)
	if err != nil {
		return nil, fmt.Errorf("bydfi get tickers: %w", err)
	}

	var resp bydfiTickersResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal bydfi tickers: %w", err)
	}

	return resp.Data, nil
}

func (c *Client) fetchFundingRateForSymbol(ctx context.Context, symbol string) (*bydfiFundingRateInfo, error) {
	body, err := c.request(ctx, "/v1/fapi/market/funding_rate", map[string]string{
		symbolKey: symbol,
	})
	if err != nil {
		return nil, fmt.Errorf("bydfi get funding rate for %s: %w", symbol, err)
	}

	var resp bydfiFundingRateResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal funding rate: %w", err)
	}

	return &resp.Data, nil
}

type bydfiCandidate struct {
	ticker    *bydfiTicker
	stdSym    string
	price     float64
	volume24h float64
}

func (c *Client) filterCandidates(
	tickers []bydfiTicker,
	factors map[string]float64,
	minVol24h, maxVol24h float64,
	whitelistMap, blacklistMap map[string]bool,
) []bydfiCandidate {
	var candidates []bydfiCandidate
	for i := range tickers {
		ticker := &tickers[i]
		factor, ok := factors[ticker.Symbol]
		if !ok {
			continue // Exclude inactive or missing symbols
		}

		stdSym := toStandardSymbol(ticker.Symbol)
		if blacklistMap[stdSym] {
			continue
		}
		if len(whitelistMap) > 0 && !whitelistMap[stdSym] {
			continue
		}

		price, _ := ticker.Last.Float64()
		baseVol, _ := ticker.Vol.Float64()
		vol24h := baseVol * factor * price

		if minVol24h > 0 && vol24h < minVol24h {
			continue
		}
		if maxVol24h > 0 && vol24h > maxVol24h {
			continue
		}

		candidates = append(candidates, bydfiCandidate{
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
	factors, err := c.fetchExchangeInfo(ctx)
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

	candidates := c.filterCandidates(tickers, factors, minVol24h, maxVol24h, whitelistMap, blacklistMap)

	// Sort candidates by volume descending
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].volume24h > candidates[j].volume24h
	})

	// Limit to top 30 symbols to avoid hitting rate limits
	limit := min(len(candidates), 30)

	var results []exchange.PotentialFundingResult
	for i := range limit {
		cand := candidates[i]
		val, err := c.fetchFundingRateForSymbol(ctx, cand.ticker.Symbol)
		var rate float64
		var settleTime int64

		if err == nil {
			rate, _ = val.LastFundingRate.Float64()
			settleStr := val.NextFundingTime.String()
			settleTime, _ = strconv.ParseInt(settleStr, 10, 64)
		} else {
			c.logger.Error("Failed to fetch funding rate for BYDFi symbol", "symbol", cand.ticker.Symbol, "error", err)
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
