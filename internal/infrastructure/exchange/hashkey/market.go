package hashkey

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type hashkeyTicker struct {
	T  int64  `json:"t"`
	S  string `json:"s"`
	C  string `json:"c"`
	Qv string `json:"qv"`
	It string `json:"it"`
}

type hashkeyFundingRate struct {
	Symbol         string       `json:"symbol"`
	Rate           string       `json:"rate"`
	NextSettleTime xjson.Number `json:"nextSettleTime"`
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
	s = strings.ReplaceAll(s, "-PERPETUAL", "")
	s = strings.ReplaceAll(s, "-PERP", "")
	s = strings.ReplaceAll(s, "-SWAP", "")
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "_", "")
	return s
}

func (c *Client) fetchFundingRates(ctx context.Context) ([]hashkeyFundingRate, error) {
	// Add required millisecond timestamp parameter for this endpoint
	nowMs := strconv.FormatInt(time.Now().UnixMilli(), 10)
	fundingBody, err := c.request(ctx, "/api/v1/futures/fundingRate", map[string]string{
		"timestamp": nowMs,
	})
	if err != nil {
		return nil, fmt.Errorf("hashkey get funding rate: %w", err)
	}

	var fundRates []hashkeyFundingRate
	if err := xjson.Unmarshal(fundingBody, &fundRates); err != nil {
		return nil, fmt.Errorf("unmarshal hashkey funding rates: %w", err)
	}

	return fundRates, nil
}

func (c *Client) fetchTickerForSymbol(ctx context.Context, symbol string) (*hashkeyTicker, error) {
	tickerBody, err := c.request(ctx, "/quote/v1/ticker/24hr", map[string]string{
		"symbol": symbol,
	})
	if err != nil {
		return nil, fmt.Errorf("hashkey get ticker for %s: %w", symbol, err)
	}

	var tickers []hashkeyTicker
	if err := xjson.Unmarshal(tickerBody, &tickers); err != nil {
		return nil, fmt.Errorf("unmarshal hashkey tickers: %w", err)
	}

	if len(tickers) == 0 {
		return nil, fmt.Errorf("no ticker returned for symbol %s", symbol)
	}

	return &tickers[0], nil
}

func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	fundRates, err := c.fetchFundingRates(ctx)
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
	for i := range fundRates {
		fr := &fundRates[i]
		stdSym := toStandardSymbol(fr.Symbol)

		if blacklistMap[stdSym] {
			continue
		}
		if len(whitelistMap) > 0 && !whitelistMap[stdSym] {
			continue
		}

		// Fetch the ticker for this symbol
		ticker, err := c.fetchTickerForSymbol(ctx, fr.Symbol)
		if err != nil {
			c.logger.Error("Failed to fetch ticker for HashKey symbol", "symbol", fr.Symbol, "error", err)
			continue
		}

		vol24h, _ := strconv.ParseFloat(ticker.Qv, 64)
		if minVol24h > 0 && vol24h < minVol24h {
			continue
		}
		if maxVol24h > 0 && vol24h > maxVol24h {
			continue
		}

		price, _ := strconv.ParseFloat(ticker.C, 64)
		rate, _ := strconv.ParseFloat(fr.Rate, 64)

		results = append(results, exchange.PotentialFundingResult{
			Symbol:     stdSym,
			Rate:       rate,
			SettleTime: xjson.ToInt64(fr.NextSettleTime),
			Volume24h:  vol24h,
			Price:      price,
		})
	}

	return results, nil
}

func (c *Client) GetTopGainer(_ context.Context, _ exchange.TopGainerRequest) ([]exchange.TopGainerResult, error) {
	return nil, nil
}
