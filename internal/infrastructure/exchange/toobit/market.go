package toobit

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
)

type toobitTicker struct {
	T   int64  `json:"t"`   // time
	A   string `json:"a"`   // highest selling price
	B   string `json:"b"`   // highest bid
	S   string `json:"s"`   // symbol
	C   string `json:"c"`   // latest transaction price
	O   string `json:"o"`   // open price
	H   string `json:"h"`   // high price
	L   string `json:"l"`   // low price
	V   string `json:"v"`   // Base asset volume
	Qv  string `json:"qv"`  // total trade volume (in quote asset)
	Pc  string `json:"pc"`  // priceChange
	Pcp string `json:"pcp"` // priceChangePercent
}

type toobitFundingRate struct {
	Symbol           string      `json:"symbol"`
	Rate             string      `json:"rate"`
	Period           string      `json:"period"`
	NextFundingTime  json.Number `json:"nextFundingTime"`
	Interest         string      `json:"interest"`
	FundingRateCap   string      `json:"fundingRateCap"`
	FundingRateFloor string      `json:"fundingRateFloor"`
}

type serverTimeResponse struct {
	ServerTime int64 `json:"serverTime"`
}

// GetServerTime returns the server millisecond timestamp.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	body, err := c.request(ctx, http.MethodGet, "/api/v1/time", nil, false)
	if err != nil {
		return 0, err
	}
	var resp serverTimeResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, fmt.Errorf("unmarshal server time: %w", err)
	}
	return resp.ServerTime, nil
}

type toobitExchangeInfo struct {
	Contracts []toobitContract `json:"contracts"`
	Symbols   []toobitContract `json:"symbols"` // fallback
}

type toobitContract struct {
	Symbol             string         `json:"symbol"`
	BaseAsset          string         `json:"baseAsset"`
	QuoteAsset         string         `json:"quoteAsset"`
	MarginAsset        string         `json:"marginAsset"`
	ContractMultiplier string         `json:"contractMultiplier"`
	Filters            []toobitFilter `json:"filters"`
}

type toobitFilter struct {
	FilterType string `json:"filterType"`
	TickSize   string `json:"tickSize,omitempty"`
	StepSize   string `json:"stepSize,omitempty"`
	MinQty     string `json:"minQty,omitempty"`
	MinPrice   string `json:"minPrice,omitempty"`
}

// GetContractDetails returns contracts specs.
func (c *Client) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	body, err := c.request(ctx, http.MethodGet, "/api/v1/exchangeInfo", nil, false)
	if err != nil {
		return nil, err
	}
	var resp toobitExchangeInfo
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal exchange info: %w", err)
	}

	contracts := resp.Contracts
	if len(contracts) == 0 {
		contracts = resp.Symbols
	}

	details := make([]exchange.ContractDetail, 0, len(contracts))
	for i := range contracts {
		raw := &contracts[i]

		priceUnit := 0.0
		minVol := 0.0
		stepSize := 0.0
		tickSizeStr := ""
		stepSizeStr := ""

		for _, f := range raw.Filters {
			switch f.FilterType {
			case "PRICE_FILTER":
				priceUnit = decmath.ParseFloat(f.TickSize)
				tickSizeStr = f.TickSize
			case "LOT_SIZE":
				minVol = decmath.ParseFloat(f.MinQty)
				stepSize = decmath.ParseFloat(f.StepSize)
				stepSizeStr = f.StepSize
			}
		}

		priceScale := decmath.DecimalPlaces(tickSizeStr)
		volScale := decmath.DecimalPlaces(stepSizeStr)

		multiplier := 1.0
		if raw.ContractMultiplier != "" {
			multiplier = decmath.ParseFloat(raw.ContractMultiplier)
		}

		displayName := raw.Symbol
		displayName = strings.ReplaceAll(displayName, "-SWAP", "")
		displayName = strings.ReplaceAll(displayName, "-", "")
		displayName = strings.ReplaceAll(displayName, "_", "")

		details = append(details, exchange.ContractDetail{
			Symbol:        raw.Symbol,
			DisplayName:   displayName,
			DisplayNameEn: displayName,
			BaseCoin:      raw.BaseAsset,
			QuoteCoin:     raw.QuoteAsset,
			SettleCoin:    raw.MarginAsset,
			ContractSize:  multiplier,
			MinLeverage:   1,
			MaxLeverage:   100,
			PriceUnit:     priceUnit,
			MinVol:        int(minVol),
			VolUnit:       int(stepSize),
			PriceScale:    priceScale,
			VolScale:      volScale,
			State:         1,
		})
	}

	return details, nil
}

// GetTickers returns 24hr ticker price change statistics for all or a specific symbol.
func (c *Client) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	query := make(map[string]string)
	if symbol != "" {
		query["symbol"] = symbol
	}

	body, err := c.request(ctx, http.MethodGet, "/quote/v1/contract/ticker/24hr", query, false)
	if err != nil {
		return nil, err
	}

	var rawList []toobitTicker
	if err := json.Unmarshal(body, &rawList); err != nil {
		return nil, fmt.Errorf("unmarshal tickers: %w", err)
	}

	tickers := make([]exchange.Ticker, 0, len(rawList))
	for i := range rawList {
		item := &rawList[i]
		last, _ := strconv.ParseFloat(item.C, 64)
		bid, _ := strconv.ParseFloat(item.B, 64)
		ask, _ := strconv.ParseFloat(item.A, 64)
		vol, _ := strconv.ParseFloat(item.V, 64)
		amt, _ := strconv.ParseFloat(item.Qv, 64)

		tickers = append(tickers, exchange.Ticker{
			Symbol:       item.S,
			LastPrice:    last,
			Bid1:         bid,
			Ask1:         ask,
			Volume24:     vol,
			AmountUSDT24: amt,
			Timestamp:    item.T,
		})
	}

	return tickers, nil
}

// GetFundingRates returns funding rates for specific standard symbols.
func (c *Client) GetFundingRates(ctx context.Context, symbols []string) ([]exchange.FundingRateResult, error) {
	if len(symbols) == 0 {
		return nil, nil
	}

	body, err := c.request(ctx, http.MethodGet, "/api/v1/futures/fundingRate", nil, false)
	if err != nil {
		return nil, err
	}

	var rawList []toobitFundingRate
	if err := json.Unmarshal(body, &rawList); err != nil {
		return nil, fmt.Errorf("unmarshal funding rates: %w", err)
	}

	// Index rates by standardized symbol
	rateMap := make(map[string]*toobitFundingRate)
	for i := range rawList {
		item := &rawList[i]
		rateMap[item.Symbol] = item
	}

	results := make([]exchange.FundingRateResult, 0, len(symbols))
	for _, sym := range symbols {
		item, exists := rateMap[sym]
		if !exists {
			continue
		}

		ts, _ := item.NextFundingTime.Int64()

		rate, _ := strconv.ParseFloat(item.Rate, 64)
		results = append(results, exchange.FundingRateResult{
			Symbol:     sym, // return matching the requested symbol format
			Rate:       rate,
			SettleTime: ts,
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
		return nil, fmt.Errorf("toobit list tickers: %w", err)
	}

	whitelistMap := make(map[string]bool)
	for _, sym := range whitelist {
		whitelistMap[sym] = true
	}

	blacklistMap := make(map[string]bool)
	for _, sym := range blacklist {
		blacklistMap[sym] = true
	}

	var filteredSymbols []string
	volMap := make(map[string]float64)
	priceMap := make(map[string]float64)

	for _, t := range tickers {
		stdSym := t.Symbol
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
		return nil, fmt.Errorf("toobit list funding rates: %w", err)
	}

	var results []exchange.PotentialFundingResult
	for _, r := range rates {
		stdSym := r.Symbol
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
