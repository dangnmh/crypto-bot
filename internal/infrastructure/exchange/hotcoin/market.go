package hotcoin

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

const (
	successMsg = "success"
)

type hotcoinContract struct {
	TickerID                 string       `json:"tickerId"`
	LastPrice                xjson.Number `json:"lastPrice"`
	NextFundingRate          xjson.Number `json:"nextFundingRate"`
	NextFundingRateTimestamp int64        `json:"nextFundingRateTimestamp"`
	TargetVolume             xjson.Number `json:"targetVolume"`
	Bid                      xjson.Number `json:"bid"`
	Ask                      xjson.Number `json:"ask"`
	BaseVolume               xjson.Number `json:"baseVolume"`
	FundingRate              xjson.Number `json:"fundingRate"`
}

type hotcoinResponse struct {
	Code int               `json:"code"`
	Data []hotcoinContract `json:"data"`
	Msg  string            `json:"msg"`
}

type hotcoinPublicContract struct {
	Code          string       `json:"code"`
	Base          string       `json:"base"`
	Quote         string       `json:"quote"`
	IndexBase     string       `json:"indexBase"`
	MinQuoteDigit xjson.Number `json:"minQuoteDigit"`
	MinTradeDigit xjson.Number `json:"minTradeDigit"`
	MinTradeUnit  xjson.Number `json:"minTradeUnit"`
	MaxLever      xjson.Number `json:"maxLever"`
	UnitAmount    xjson.Number `json:"unitAmount"`
}

type hotcoinPublicResponse struct {
	Code int                     `json:"code"`
	Data []hotcoinPublicContract `json:"data"`
	Msg  string                  `json:"msg"`
}

// GetTickers retrieves current ticker prices for all active contracts (or filtered by symbol).
func (c *Client) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	body, err := c.request(ctx, http.MethodGet, "/api/v1/perpetual/public/contracts", nil, nil, false)
	if err != nil {
		return nil, err
	}

	var resp hotcoinResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal tickers: %w", err)
	}

	if resp.Code != 200 && resp.Msg != successMsg {
		return nil, fmt.Errorf("API error: code=%d msg=%s", resp.Code, resp.Msg)
	}

	var targetSymbol string
	if symbol != "" {
		targetSymbol = strings.ToUpper(strings.ReplaceAll(symbol, "_", ""))
	}

	nowMs := time.Now().UnixMilli()
	var tickers []exchange.Ticker
	for i := range resp.Data {
		item := &resp.Data[i]
		symUpper := strings.ToUpper(strings.ReplaceAll(item.TickerID, "_", ""))
		if targetSymbol != "" && symUpper != targetSymbol {
			continue
		}

		// Normalize symbol name with underscore if possible (e.g. BTCUSDT -> BTC_USDT)
		normSymbol := strings.ToUpper(item.TickerID)
		if !strings.Contains(normSymbol, "_") {
			// Find suffix USDT or USDC to insert underscore
			if before, ok := strings.CutSuffix(normSymbol, "USDT"); ok {
				normSymbol = before + "_USDT"
			} else if before, ok := strings.CutSuffix(normSymbol, "USDC"); ok {
				normSymbol = before + "_USDC"
			}
		}

		lastVal := xjson.ToFloat64(item.LastPrice)
		volVal := xjson.ToFloat64(item.BaseVolume)
		tickers = append(tickers, exchange.Ticker{
			Symbol:       normSymbol,
			LastPrice:    lastVal,
			Bid1:         xjson.ToFloat64(item.Bid),
			Ask1:         xjson.ToFloat64(item.Ask),
			Volume24:     volVal,
			AmountUSDT24: volVal * lastVal,
			Timestamp:    nowMs,
		})
	}

	return tickers, nil
}

// GetContractDetails fetches contract specifications, leverage tiers, and trading precisions.
func (c *Client) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	body, err := c.request(ctx, http.MethodGet, "/api/v1/perpetual/public", nil, nil, false)
	if err != nil {
		return nil, err
	}

	var resp hotcoinPublicResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal public contracts: %w", err)
	}

	var details []exchange.ContractDetail
	for i := range resp.Data {
		item := &resp.Data[i]
		baseCoin := strings.ToUpper(item.IndexBase)
		quoteCoin := strings.ToUpper(item.Quote)
		if baseCoin == "" {
			// Fallback base coin extract from code
			baseCoin = strings.ToUpper(strings.TrimSuffix(item.Code, item.Quote))
		}

		symbol := baseCoin + "_" + quoteCoin

		minVolVal := int(xjson.ToInt64(item.MinTradeUnit))
		if minVolVal == 0 {
			minVolVal = 1
		}

		priceScale := int(xjson.ToInt64(item.MinQuoteDigit))
		details = append(details, exchange.ContractDetail{
			Symbol:       symbol,
			DisplayName:  strings.ToUpper(item.Code),
			BaseCoin:     baseCoin,
			QuoteCoin:    quoteCoin,
			SettleCoin:   quoteCoin,
			ContractSize: xjson.ToFloat64(item.UnitAmount),
			PriceScale:   priceScale,
			PriceUnit:    math.Pow10(-priceScale),
			VolScale:     int(xjson.ToInt64(item.MinTradeDigit)),
			MinVol:       minVolVal,
			VolUnit:      minVolVal,
			MinLeverage:  1,
			MaxLeverage:  int(xjson.ToInt64(item.MaxLever)),
		})
	}

	return details, nil
}

// GetFundingRates retrieves the current funding rates for the specified symbols.
func (c *Client) GetFundingRates(ctx context.Context, symbols []string) ([]exchange.FundingRateResult, error) {
	body, err := c.request(ctx, http.MethodGet, "/api/v1/perpetual/public/contracts", nil, nil, false)
	if err != nil {
		return nil, err
	}

	var resp hotcoinResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal funding rates: %w", err)
	}

	symbolMap := make(map[string]bool)
	for _, sym := range symbols {
		symbolMap[strings.ToUpper(strings.ReplaceAll(sym, "_", ""))] = true
	}

	var results []exchange.FundingRateResult
	for i := range resp.Data {
		item := &resp.Data[i]
		symUpper := strings.ToUpper(strings.ReplaceAll(item.TickerID, "_", ""))
		if len(symbolMap) > 0 && !symbolMap[symUpper] {
			continue
		}

		normSymbol := strings.ToUpper(item.TickerID)
		if !strings.Contains(normSymbol, "_") {
			if before, ok := strings.CutSuffix(normSymbol, "USDT"); ok {
				normSymbol = before + "_USDT"
			} else if before, ok := strings.CutSuffix(normSymbol, "USDC"); ok {
				normSymbol = before + "_USDC"
			}
		}

		results = append(results, exchange.FundingRateResult{
			Symbol:     normSymbol,
			Rate:       xjson.ToFloat64(item.NextFundingRate),
			SettleTime: item.NextFundingRateTimestamp,
		})
	}

	return results, nil
}

func isPotentialFunding(item *hotcoinContract, minVol24h, maxVol24h float64, whitelistMap, blacklistMap map[string]bool) bool {
	symUpper := strings.ToUpper(strings.ReplaceAll(item.TickerID, "_", ""))
	if blacklistMap[symUpper] {
		return false
	}
	if len(whitelistMap) > 0 && !whitelistMap[symUpper] {
		return false
	}
	vol := xjson.ToFloat64(item.TargetVolume)
	if vol < minVol24h {
		return false
	}
	if maxVol24h > 0 && vol > maxVol24h {
		return false
	}
	return true
}

// GetPotentialFundingSymbols fetches all perpetual contracts, their tickers, and estimated funding rates.
func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	body, err := c.request(ctx, http.MethodGet, "/api/v1/perpetual/public/contracts", nil, nil, false)
	if err != nil {
		return nil, err
	}

	var resp hotcoinResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal contracts: %w", err)
	}

	if resp.Code != 200 && resp.Msg != successMsg {
		return nil, fmt.Errorf("API error: code=%d msg=%s", resp.Code, resp.Msg)
	}

	whitelistMap := make(map[string]bool)
	for _, sym := range whitelist {
		whitelistMap[strings.ToUpper(strings.ReplaceAll(sym, "_", ""))] = true
	}
	blacklistMap := make(map[string]bool)
	for _, sym := range blacklist {
		blacklistMap[strings.ToUpper(strings.ReplaceAll(sym, "_", ""))] = true
	}

	var results []exchange.PotentialFundingResult
	for i := range resp.Data {
		item := &resp.Data[i]
		if !isPotentialFunding(item, minVol24h, maxVol24h, whitelistMap, blacklistMap) {
			continue
		}

		price := xjson.ToFloat64(item.LastPrice)
		rate := xjson.ToFloat64(item.NextFundingRate)
		vol := xjson.ToFloat64(item.TargetVolume)

		// Normalize symbol name with underscore
		normSymbol := strings.ToUpper(item.TickerID)
		if !strings.Contains(normSymbol, "_") {
			if before, ok := strings.CutSuffix(normSymbol, "USDT"); ok {
				normSymbol = before + "_USDT"
			} else if before, ok := strings.CutSuffix(normSymbol, "USDC"); ok {
				normSymbol = before + "_USDC"
			}
		}

		results = append(results, exchange.PotentialFundingResult{
			Symbol:     normSymbol,
			Rate:       rate,
			SettleTime: item.NextFundingRateTimestamp,
			Volume24h:  vol,
			Price:      price,
		})
	}

	return results, nil
}

func (c *Client) GetTopGainer(ctx context.Context, req exchange.TopGainerRequest) ([]exchange.TopGainerResult, error) {
	tickers, err := c.GetTickers(ctx, "")
	if err != nil {
		return nil, err
	}
	results := make([]exchange.TopGainerResult, 0, len(tickers))
	for _, t := range tickers {
		volUSDT := t.AmountUSDT24
		if volUSDT == 0 {
			volUSDT = t.Volume24 * t.LastPrice
		}
		spreadPct := 0.0
		if t.Bid1 > 0 && t.Ask1 > 0 {
			spreadPct = ((t.Ask1 - t.Bid1) / t.Bid1) * 100.0
		}
		results = append(results, exchange.TopGainerResult{
			Symbol:        t.Symbol,
			LastPrice:     t.LastPrice,
			Bid1:          t.Bid1,
			Ask1:          t.Ask1,
			Volume24hUSDT: volUSDT,
			Gain24hPct:    0.0,
			SpreadPct:     spreadPct,
			Timestamp:     t.Timestamp,
		})
	}
	if req.Limit > 0 && req.Limit < len(results) {
		results = results[:req.Limit]
	}
	return results, nil
}
