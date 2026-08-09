package weex

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type weexTicker struct {
	Symbol             string       `json:"symbol"`
	PriceChange        string       `json:"priceChange"`
	PriceChangePercent string       `json:"priceChangePercent"`
	LastPrice          string       `json:"lastPrice"`
	OpenPrice          string       `json:"openPrice"`
	HighPrice          string       `json:"highPrice"`
	LowPrice           string       `json:"lowPrice"`
	Volume             string       `json:"volume"`
	QuoteVolume        string       `json:"quoteVolume"`
	MarkPrice          string       `json:"markPrice"`
	IndexPrice         string       `json:"indexPrice"`
	OpenTime           xjson.Number `json:"openTime"`
	CloseTime          xjson.Number `json:"closeTime"`
}

type weexPremiumIndex struct {
	Symbol              string       `json:"symbol"`
	MarkPrice           string       `json:"markPrice"`
	IndexPrice          string       `json:"indexPrice"`
	LastFundingRate     string       `json:"lastFundingRate"`
	ForecastFundingRate string       `json:"forecastFundingRate"`
	InterestRate        string       `json:"interestRate"`
	NextFundingTime     xjson.Number `json:"nextFundingTime"`
	Time                xjson.Number `json:"time"`
	CollectCycle        xjson.Number `json:"collectCycle"`
}

type weexSymbol struct {
	Symbol            string       `json:"symbol"`
	BaseAsset         string       `json:"baseAsset"`
	QuoteAsset        string       `json:"quoteAsset"`
	MarginAsset       string       `json:"marginAsset"`
	PricePrecision    int          `json:"pricePrecision"`
	QuantityPrecision int          `json:"quantityPrecision"`
	MinOrderSize      xjson.Number `json:"minOrderSize"`
	MaxLeverage       int          `json:"maxLeverage"`
}

type weexExchangeInfo struct {
	Symbols []weexSymbol `json:"symbols"`
}

// GetContractDetails returns the contract specifications for all symbols.
func (c *Client) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	body, err := c.request(ctx, http.MethodGet, "/capi/v3/market/exchangeInfo", nil, nil, false)
	if err != nil {
		return nil, err
	}

	resp, err := parseResponse[weexExchangeInfo](body)
	if err != nil {
		return nil, err
	}

	details := make([]exchange.ContractDetail, 0, len(resp.Symbols))
	for i := range resp.Symbols {
		s := &resp.Symbols[i]

		displayName := s.Symbol

		minVol, _ := s.MinOrderSize.Float64()
		priceUnit := math.Pow10(-s.PricePrecision)
		volUnit := int(minVol)
		if volUnit == 0 {
			volUnit = 1
		}

		details = append(details, exchange.ContractDetail{
			Symbol:        strings.ToUpper(s.Symbol),
			DisplayName:   displayName,
			DisplayNameEn: displayName,
			BaseCoin:      strings.ToUpper(s.BaseAsset),
			QuoteCoin:     strings.ToUpper(s.QuoteAsset),
			SettleCoin:    strings.ToUpper(s.MarginAsset),
			ContractSize:  1.0,
			MinLeverage:   1,
			MaxLeverage:   s.MaxLeverage,
			PriceScale:    s.PricePrecision,
			VolScale:      s.QuantityPrecision,
			PriceUnit:     priceUnit,
			VolUnit:       volUnit,
			MinVol:        int(minVol),
			State:         1,
		})
	}

	return details, nil
}

// GetTickers returns 24hr ticker price change statistics.
func (c *Client) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	query := make(map[string]string)
	if symbol != "" {
		query["symbol"] = strings.ToUpper(symbol)
	}

	body, err := c.request(ctx, http.MethodGet, "/capi/v3/market/ticker/24hr", query, nil, false)
	if err != nil {
		return nil, err
	}

	rawList, err := parseResponse[[]weexTicker](body)
	if err != nil {
		return nil, err
	}

	tickers := make([]exchange.Ticker, 0, len(rawList))
	for i := range rawList {
		item := &rawList[i]
		last, _ := strconv.ParseFloat(item.LastPrice, 64)
		bid, _ := strconv.ParseFloat(item.LastPrice, 64) // Weex docs don't show best bid/ask, so fallback to lastPrice
		ask, _ := strconv.ParseFloat(item.LastPrice, 64)
		vol, _ := strconv.ParseFloat(item.Volume, 64)
		amt, _ := strconv.ParseFloat(item.QuoteVolume, 64)
		ts, _ := item.CloseTime.Int64()

		tickers = append(tickers, exchange.Ticker{
			Symbol:       strings.ToUpper(item.Symbol),
			LastPrice:    last,
			Bid1:         bid,
			Ask1:         ask,
			Volume24:     vol,
			AmountUSDT24: amt,
			Timestamp:    ts,
		})
	}

	return tickers, nil
}

// GetFundingRates returns funding rates for specific standard symbols.
func (c *Client) GetFundingRates(ctx context.Context, symbols []string) ([]exchange.FundingRateResult, error) {
	if len(symbols) == 0 {
		return nil, nil
	}

	body, err := c.request(ctx, http.MethodGet, "/capi/v3/market/premiumIndex", nil, nil, false)
	if err != nil {
		return nil, err
	}

	rawList, err := parseResponse[[]weexPremiumIndex](body)
	if err != nil {
		return nil, err
	}

	// Index rates by standardized symbol name
	rateMap := make(map[string]*weexPremiumIndex)
	for i := range rawList {
		item := &rawList[i]
		stdSym := strings.ToUpper(item.Symbol)
		rateMap[stdSym] = item
	}

	results := make([]exchange.FundingRateResult, 0, len(symbols))
	for _, sym := range symbols {
		stdSym := strings.ToUpper(sym)
		item, exists := rateMap[stdSym]
		if !exists {
			continue
		}

		rate, _ := strconv.ParseFloat(item.LastFundingRate, 64)
		settleTime, _ := item.NextFundingTime.Int64()

		results = append(results, exchange.FundingRateResult{
			Symbol:     sym,
			Rate:       rate,
			SettleTime: settleTime,
		})
	}

	return results, nil
}

func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	tickers, err := c.GetTickers(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("weex list tickers: %w", err)
	}

	whitelistMap := make(map[string]bool)
	for _, sym := range whitelist {
		whitelistMap[strings.ToUpper(sym)] = true
	}

	blacklistMap := make(map[string]bool)
	for _, sym := range blacklist {
		blacklistMap[strings.ToUpper(sym)] = true
	}

	var filteredSymbols []string
	volMap := make(map[string]float64)
	priceMap := make(map[string]float64)

	for _, t := range tickers {
		stdSym := strings.ToUpper(t.Symbol)
		if blacklistMap[stdSym] {
			continue
		}
		if len(whitelistMap) > 0 && !whitelistMap[stdSym] {
			continue
		}
		if t.AmountUSDT24 < minVol24h {
			continue
		}
		if maxVol24h > 0 && t.AmountUSDT24 > maxVol24h {
			continue
		}

		filteredSymbols = append(filteredSymbols, t.Symbol)
		volMap[stdSym] = t.AmountUSDT24
		priceMap[stdSym] = t.LastPrice
	}

	if len(filteredSymbols) == 0 {
		return nil, nil
	}

	rates, err := c.GetFundingRates(ctx, filteredSymbols)
	if err != nil {
		return nil, fmt.Errorf("weex list funding rates: %w", err)
	}

	var results []exchange.PotentialFundingResult
	for _, r := range rates {
		stdSym := strings.ToUpper(r.Symbol)
		results = append(results, exchange.PotentialFundingResult{
			Symbol:     r.Symbol,
			Rate:       r.Rate,
			SettleTime: r.SettleTime,
			Volume24h:  volMap[stdSym],
			Price:      priceMap[stdSym],
		})
	}

	return results, nil
}

func (c *Client) GetTopGainer(_ context.Context, _ exchange.TopGainerRequest) ([]exchange.TopGainerResult, error) {
	return nil, nil
}
