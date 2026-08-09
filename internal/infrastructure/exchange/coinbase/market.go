package coinbase

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type coinbaseQuote struct {
	TradePrice       xjson.Number `json:"trade_price"`
	PredictedFunding xjson.Number `json:"predicted_funding"`
	Timestamp        string       `json:"timestamp"`
}

type coinbaseInstrument struct {
	Symbol       string        `json:"symbol"`
	Type         string        `json:"type"`
	Notional24h  xjson.Number  `json:"notional_24hr"`
	TradingState string        `json:"trading_state"`
	Quote        coinbaseQuote `json:"quote"`
}

func (c *Client) request(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, http.NoBody)
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
	if before, ok := strings.CutSuffix(s, "-PERP"); ok {
		s = before + "USDC"
	}
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "_", "")
	return s
}

func getNextHourlyInterval() int64 {
	now := time.Now().UTC()
	return now.Truncate(time.Hour).Add(time.Hour).UnixMilli()
}

// GetPotentialFundingSymbols satisfies the ScannerClient interface.
func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist, blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	body, err := c.request(ctx, "/api/v1/instruments")
	if err != nil {
		return nil, fmt.Errorf("coinbase get instruments: %w", err)
	}

	var instruments []coinbaseInstrument
	if err := json.Unmarshal(body, &instruments); err != nil {
		return nil, fmt.Errorf("unmarshal coinbase instruments: %w", err)
	}

	results := c.filterInstruments(instruments, minVol24h, maxVol24h, whitelist, blacklist)
	return results, nil
}

func (c *Client) filterInstruments(
	instruments []coinbaseInstrument,
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
	for _, inst := range instruments {
		if inst.Type != "PERP" {
			continue
		}
		if inst.TradingState != "TRADING" {
			continue
		}

		stdSym := toStandardSymbol(inst.Symbol)
		if blacklistMap[stdSym] {
			continue
		}
		if len(whitelistMap) > 0 && !whitelistMap[stdSym] {
			continue
		}

		vol24h, _ := inst.Notional24h.Float64()
		if minVol24h > 0 && vol24h < minVol24h {
			continue
		}
		if maxVol24h > 0 && vol24h > maxVol24h {
			continue
		}

		price, _ := inst.Quote.TradePrice.Float64()
		rate, _ := inst.Quote.PredictedFunding.Float64()

		results = append(results, exchange.PotentialFundingResult{
			Symbol:     stdSym,
			Rate:       rate,
			SettleTime: getNextHourlyInterval(),
			Volume24h:  vol24h,
			Price:      price,
		})
	}
	return results
}

func (c *Client) GetTopGainer(_ context.Context, _ exchange.TopGainerRequest) ([]exchange.TopGainerResult, error) {
	return nil, nil
}
