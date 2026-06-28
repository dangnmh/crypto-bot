package bitunix

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type bitunixTicker struct {
	Symbol    string `json:"symbol"`
	MarkPrice string `json:"markPrice"`
	LastPrice string `json:"lastPrice"`
	QuoteVol  string `json:"quoteVol"`
	BaseVol   string `json:"baseVol"`
	Last      string `json:"last"`
}

type bitunixTickersResponse struct {
	Code int             `json:"code"`
	Data []bitunixTicker `json:"data"`
	Msg  string          `json:"msg"`
}

type bitunixFundingRate struct {
	Symbol          string `json:"symbol"`
	FundingRate     string `json:"fundingRate"`
	NextFundingTime string `json:"nextFundingTime"`
	MarkPrice       string `json:"markPrice"`
}

type bitunixFundingRateResponse struct {
	Code int                  `json:"code"`
	Data []bitunixFundingRate `json:"data"`
	Msg  string               `json:"msg"`
}

type bitunixTradingPair struct {
	Symbol          string `json:"symbol"`
	Base            string `json:"base"`
	Quote           string `json:"quote"`
	MinTradeVolume  string `json:"minTradeVolume"`
	BasePrecision   int    `json:"basePrecision"`
	QuotePrecision  int    `json:"quotePrecision"`
	MaxLeverage     int    `json:"maxLeverage"`
	MinLeverage     int    `json:"minLeverage"`
	DefaultLeverage int    `json:"defaultLeverage"`
	SymbolStatus    string `json:"symbolStatus"`
}

type bitunixTradingPairsResponse struct {
	Code int                  `json:"code"`
	Data []bitunixTradingPair `json:"data"`
	Msg  string               `json:"msg"`
}

func (c *Client) fetchTickersData(ctx context.Context) (map[string]float64, map[string]float64, error) {
	tickersBody, err := c.request(ctx, http.MethodGet, "/api/v1/futures/market/tickers", nil, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("bitunix fetch tickers: %w", err)
	}

	var tickersResp bitunixTickersResponse
	if err := xjson.Unmarshal(tickersBody, &tickersResp); err != nil {
		return nil, nil, fmt.Errorf("bitunix unmarshal tickers: %w", err)
	}

	volMap := make(map[string]float64)
	priceMap := make(map[string]float64)
	for i := range tickersResp.Data {
		item := &tickersResp.Data[i]
		vol, _ := strconv.ParseFloat(item.QuoteVol, 64)
		price, _ := strconv.ParseFloat(item.MarkPrice, 64)
		if price == 0 {
			price, _ = strconv.ParseFloat(item.LastPrice, 64)
		}
		if price == 0 {
			price, _ = strconv.ParseFloat(item.Last, 64)
		}
		volMap[item.Symbol] = vol
		priceMap[item.Symbol] = price
	}

	return volMap, priceMap, nil
}

// GetTickers returns 24hr ticker price change statistics.
func (c *Client) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	tickersBody, err := c.request(ctx, http.MethodGet, "/api/v1/futures/market/tickers", nil, nil)
	if err != nil {
		return nil, err
	}

	var tickersResp bitunixTickersResponse
	if err := xjson.Unmarshal(tickersBody, &tickersResp); err != nil {
		return nil, fmt.Errorf("bitunix unmarshal tickers: %w", err)
	}

	var tickers []exchange.Ticker
	for i := range tickersResp.Data {
		item := &tickersResp.Data[i]
		if symbol != "" && !strings.EqualFold(item.Symbol, symbol) {
			continue
		}

		price, _ := strconv.ParseFloat(item.LastPrice, 64)
		if price == 0 {
			price, _ = strconv.ParseFloat(item.MarkPrice, 64)
		}
		if price == 0 {
			price, _ = strconv.ParseFloat(item.Last, 64)
		}
		baseVol, _ := strconv.ParseFloat(item.BaseVol, 64)
		quoteVol, _ := strconv.ParseFloat(item.QuoteVol, 64)

		tickers = append(tickers, exchange.Ticker{
			Symbol:       item.Symbol,
			LastPrice:    price,
			Bid1:         price,
			Ask1:         price,
			Volume24:     baseVol,
			AmountUSDT24: quoteVol,
			Timestamp:    time.Now().UnixMilli(),
		})
	}

	return tickers, nil
}

// GetContractDetails returns contracts specs.
func (c *Client) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	body, err := c.request(ctx, http.MethodGet, "/api/v1/futures/market/trading_pairs", nil, nil)
	if err != nil {
		return nil, err
	}

	var resp bitunixTradingPairsResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal trading pairs: %w", err)
	}

	details := make([]exchange.ContractDetail, 0, len(resp.Data))
	for i := range resp.Data {
		item := &resp.Data[i]
		if !strings.EqualFold(item.SymbolStatus, "OPEN") {
			continue
		}

		priceUnit := math.Pow(10, -float64(item.QuotePrecision))
		minVol := 1
		volUnit := 1
		stepSize := math.Pow(10, -float64(item.BasePrecision))
		if stepSize >= 1 {
			rawMin, _ := strconv.ParseFloat(item.MinTradeVolume, 64)
			minVol = int(rawMin)
			volUnit = int(stepSize)
		}

		displayName := item.Symbol
		displayName = strings.ReplaceAll(displayName, "-SWAP", "")
		displayName = strings.ReplaceAll(displayName, "-", "")
		displayName = strings.ReplaceAll(displayName, "_", "")

		details = append(details, exchange.ContractDetail{
			Symbol:        item.Symbol,
			DisplayName:   displayName,
			DisplayNameEn: displayName,
			BaseCoin:      item.Base,
			QuoteCoin:     item.Quote,
			SettleCoin:    item.Quote,
			ContractSize:  1.0,
			MinLeverage:   item.MinLeverage,
			MaxLeverage:   item.MaxLeverage,
			PriceUnit:     priceUnit,
			MinVol:        minVol,
			VolUnit:       volUnit,
			PriceScale:    item.QuotePrecision,
			VolScale:      item.BasePrecision,
			State:         1,
		})
	}

	return details, nil
}

// GetFundingRates returns funding rates for specific symbols.
func (c *Client) GetFundingRates(ctx context.Context, symbols []string) ([]exchange.FundingRateResult, error) {
	ratesBody, err := c.request(ctx, http.MethodGet, "/api/v1/futures/market/funding_rate/batch", nil, nil)
	if err != nil {
		return nil, fmt.Errorf("bitunix fetch funding rates batch: %w", err)
	}

	var ratesResp bitunixFundingRateResponse
	if err := xjson.Unmarshal(ratesBody, &ratesResp); err != nil {
		return nil, fmt.Errorf("bitunix unmarshal funding rates batch: %w", err)
	}

	targetSymbols := make(map[string]bool)
	for _, sym := range symbols {
		targetSymbols[strings.ToUpper(sym)] = true
	}

	var results []exchange.FundingRateResult
	for i := range ratesResp.Data {
		item := &ratesResp.Data[i]
		stdSym := strings.ToUpper(item.Symbol)

		if len(targetSymbols) > 0 && !targetSymbols[stdSym] {
			continue
		}

		rate, _ := strconv.ParseFloat(item.FundingRate, 64)
		rate /= 100.0 // Convert percentage to absolute decimal (0.01% -> 0.0001)

		nextSettleStr := item.NextFundingTime
		nextSettle, _ := strconv.ParseInt(nextSettleStr, 10, 64)

		results = append(results, exchange.FundingRateResult{
			Symbol:     item.Symbol,
			Rate:       rate,
			SettleTime: nextSettle,
		})
	}

	return results, nil
}

// GetPotentialFundingSymbols fetches and returns active perpetual contracts with funding rates and 24h volumes.
func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	volMap, priceMap, err := c.fetchTickersData(ctx)
	if err != nil {
		return nil, err
	}

	ratesBody, err := c.request(ctx, http.MethodGet, "/api/v1/futures/market/funding_rate/batch", nil, nil)
	if err != nil {
		return nil, fmt.Errorf("bitunix fetch funding rates batch: %w", err)
	}

	var ratesResp bitunixFundingRateResponse
	if err := xjson.Unmarshal(ratesBody, &ratesResp); err != nil {
		return nil, fmt.Errorf("bitunix unmarshal funding rates batch: %w", err)
	}

	whitelistMap := make(map[string]bool)
	for _, sym := range whitelist {
		whitelistMap[sym] = true
	}

	blacklistMap := make(map[string]bool)
	for _, sym := range blacklist {
		blacklistMap[sym] = true
	}

	var results []exchange.PotentialFundingResult
	for i := range ratesResp.Data {
		item := &ratesResp.Data[i]

		if blacklistMap[item.Symbol] {
			continue
		}
		if len(whitelistMap) > 0 && !whitelistMap[item.Symbol] {
			continue
		}

		vol := volMap[item.Symbol]
		if vol < minVol24h {
			continue
		}
		if maxVol24h > 0 && vol > maxVol24h {
			continue
		}

		rate, _ := strconv.ParseFloat(item.FundingRate, 64)
		rate /= 100.0

		price := priceMap[item.Symbol]
		if price == 0 {
			price, _ = strconv.ParseFloat(item.MarkPrice, 64)
		}

		nextSettleStr := item.NextFundingTime
		nextSettle, _ := strconv.ParseInt(nextSettleStr, 10, 64)

		results = append(results, exchange.PotentialFundingResult{
			Symbol:     item.Symbol,
			Rate:       rate,
			SettleTime: nextSettle,
			Volume24h:  vol,
			Price:      price,
		})
	}

	return results, nil
}
