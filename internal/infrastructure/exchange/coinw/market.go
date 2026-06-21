package coinw

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"crypto-bot/internal/infrastructure/exchange"
)

type coinwTicker struct {
	Name        string      `json:"name"`
	LastPrice   json.Number `json:"last_price"`
	TotalVolume json.Number `json:"total_volume"`
	Ts          int64       `json:"ts"`
}

type coinwTickerResponse struct {
	Code int           `json:"code"`
	Msg  string        `json:"msg"`
	Data []coinwTicker `json:"data"`
}

type coinwInstrument struct {
	Base           string      `json:"base"`
	Quote          string      `json:"quote"`
	Name           string      `json:"name"`
	SettledAt      int64       `json:"settledAt"`
	SettlementRate json.Number `json:"settlementRate"`
	Status         string      `json:"status"`
}

type coinwInstrumentResponse struct {
	Code int               `json:"code"`
	Msg  string            `json:"msg"`
	Data []coinwInstrument `json:"data"`
}

func (c *Client) request(ctx context.Context, method, path string, query map[string]string) ([]byte, error) {
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

	req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP %s %s: %w", method, path, err)
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

// GetTickers returns 24hr ticker price change statistics.
func (c *Client) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	body, err := c.request(ctx, http.MethodGet, "/v1/perpumPublic/tickers", nil)
	if err != nil {
		return nil, err
	}

	var resp coinwTickerResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal coinw response: %w", err)
	}

	if resp.Code != 0 {
		return nil, fmt.Errorf("coinw API error: %d - %s", resp.Code, resp.Msg)
	}

	var targetSymbol string
	if symbol != "" {
		targetSymbol = toStandardSymbol(symbol)
	}

	tickers := make([]exchange.Ticker, 0, len(resp.Data))
	for i := range resp.Data {
		item := &resp.Data[i]
		stdSym := toStandardSymbol(item.Name)

		if targetSymbol != "" && stdSym != targetSymbol {
			continue
		}

		last, _ := item.LastPrice.Float64()
		vol, _ := item.TotalVolume.Float64()

		tickers = append(tickers, exchange.Ticker{
			Symbol:       stdSym,
			LastPrice:    last,
			Bid1:         last,
			Ask1:         last,
			Volume24:     vol,
			AmountUSDT24: vol * last,
			Timestamp:    item.Ts,
		})
	}

	return tickers, nil
}

// GetFundingRates returns funding rates for specific standard symbols.
func (c *Client) GetFundingRates(ctx context.Context, symbols []string) ([]exchange.FundingRateResult, error) {
	if len(symbols) == 0 {
		return nil, nil
	}

	body, err := c.request(ctx, http.MethodGet, "/v1/perpum/instruments", nil)
	if err != nil {
		return nil, err
	}

	var resp coinwInstrumentResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal coinw instruments response: %w", err)
	}

	if resp.Code != 0 {
		return nil, fmt.Errorf("coinw API error: %d - %s", resp.Code, resp.Msg)
	}

	rateMap := make(map[string]*coinwInstrument)
	for i := range resp.Data {
		item := &resp.Data[i]
		if !strings.EqualFold(item.Status, "online") {
			continue
		}
		var stdSym string
		if item.Base != "" && item.Quote != "" {
			stdSym = toStandardSymbol(item.Base + item.Quote)
		} else {
			stdSym = toStandardSymbol(item.Name)
		}
		rateMap[stdSym] = item
	}

	results := make([]exchange.FundingRateResult, 0, len(symbols))
	for _, sym := range symbols {
		stdSym := toStandardSymbol(sym)
		item, exists := rateMap[stdSym]
		if !exists {
			continue
		}

		rate, _ := item.SettlementRate.Float64()

		results = append(results, exchange.FundingRateResult{
			Symbol:     sym,
			Rate:       rate,
			SettleTime: item.SettledAt,
		})
	}

	return results, nil
}

func toStandardSymbol(s string) string {
	upper := strings.ToUpper(s)
	upper = strings.ReplaceAll(upper, "-PERP", "")
	upper = strings.ReplaceAll(upper, "-SWAP", "")
	upper = strings.ReplaceAll(upper, "-", "")
	upper = strings.ReplaceAll(upper, "_", "")
	return upper
}
