package bitget

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strconv"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"

	"crypto-bot/pkg/xjson"
)

// Explicit request/response structs for market data endpoints.

type bitgetServerTimeRequest struct{}

type bitgetContractsRequest struct {
	ProductType string `json:"productType"`
}

type bitgetFundingRateRequest struct {
	ProductType string `json:"productType"`
	Symbol      string `json:"symbol,omitempty"`
}

type bitgetTickersRequest struct {
	ProductType string `json:"productType"`
	Symbol      string `json:"symbol,omitempty"`
}

type bitgetInstrument struct {
	Symbol       string `json:"symbol"`
	BaseCoin     string `json:"baseCoin"`
	QuoteCoin    string `json:"quoteCoin"`
	SettleCoin   string `json:"settleCoin"`
	SymbolStatus string `json:"symbolStatus"`
	PricePlace   string `json:"pricePlace"`
	VolumePlace  string `json:"volumePlace"`
	MinTradeNum  string `json:"minTradeNum"`
	PriceEndStep string `json:"priceEndStep"`
	MinLever     string `json:"minLever"`
	MaxLever     string `json:"maxLever"`
}

type rawTicker struct {
	Symbol      string `json:"symbol"`
	LastPr      string `json:"lastPr"`
	BidPr       string `json:"bidPr"`
	AskPr       string `json:"askPr"`
	BaseVolume  string `json:"baseVolume"`
	QuoteVolume string `json:"quoteVolume"`
	Ts          string `json:"ts"`
	FundingRate string `json:"fundingRate"`
}

type rawBitgetFunding struct {
	Symbol      string `json:"symbol"`
	FundingRate string `json:"fundingRate"`
	NextUpdate  string `json:"nextUpdate"`
}

// Private raw methods invoking the Bitget REST API.

func (c *Client) getRawServerTime(ctx context.Context, _ bitgetServerTimeRequest) (json.RawMessage, error) {
	body, err := c.GetCtx(ctx, pathServerTime, nil)
	if err != nil {
		return nil, err
	}

	return ParseResponse[json.RawMessage](body, "server_time")
}

func (c *Client) getRawContractDetails(ctx context.Context, req bitgetContractsRequest) ([]bitgetInstrument, error) {
	params := map[string]string{
		paramProductType: req.ProductType,
	}

	body, err := c.GetCtx(ctx, pathContracts, params)
	if err != nil {
		return nil, err
	}

	return ParseResponse[[]bitgetInstrument](body, "contract_details")
}

func (c *Client) getRawFundingRates(ctx context.Context, req bitgetFundingRateRequest) ([]rawBitgetFunding, error) {
	params := map[string]string{
		paramProductType: req.ProductType,
	}
	if req.Symbol != "" {
		params[paramSymbol] = req.Symbol
	}

	body, err := c.GetCtx(ctx, pathFundingRate, params)
	if err != nil {
		return nil, err
	}

	return ParseResponse[[]rawBitgetFunding](body, "funding_rate")
}

func (c *Client) getRawTickers(ctx context.Context, req bitgetTickersRequest) ([]rawTicker, error) {
	params := map[string]string{
		paramProductType: req.ProductType,
	}
	if req.Symbol != "" {
		params[paramSymbol] = req.Symbol
	}

	body, err := c.GetCtx(ctx, pathTickers, params)
	if err != nil {
		return nil, err
	}

	return ParseResponse[[]rawTicker](body, "tickers")
}

// Public mapper methods implementing the exchange.MarketDataProvider interface.

// GetServerTime returns the Bitget server timestamp in milliseconds.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	data, err := c.getRawServerTime(ctx, bitgetServerTimeRequest{})
	if err != nil {
		return 0, err
	}

	// Try parsing as object {"serverTime": "..."}
	var timeObj struct {
		ServerTime xjson.Number `json:"serverTime"`
	}
	if err := xjson.Unmarshal(data, &timeObj); err == nil && timeObj.ServerTime != "" {
		if val, err := timeObj.ServerTime.Int64(); err == nil {
			return val, nil
		}
	}

	// Fallback: parse as direct string value
	var strVal string
	if err := xjson.Unmarshal(data, &strVal); err == nil {
		if val, err := strconv.ParseInt(strVal, 10, 64); err == nil {
			return val, nil
		}
	}

	// Fallback: parse as direct numeric value
	var numVal int64
	if err := xjson.Unmarshal(data, &numVal); err == nil {
		return numVal, nil
	}

	return 0, fmt.Errorf("unknown server time format: %s", string(data))
}

// GetContractDetails returns specifications for all USDT-FUTURES contracts.
func (c *Client) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	instruments, err := c.getRawContractDetails(ctx, bitgetContractsRequest{
		ProductType: productTypeUsdtFutures,
	})
	if err != nil {
		return nil, err
	}

	details := make([]exchange.ContractDetail, 0, len(instruments))
	for i := range instruments {
		inst := &instruments[i]

		priceScale, _ := strconv.Atoi(inst.PricePlace)
		volScale, _ := strconv.Atoi(inst.VolumePlace)
		priceUnit, _ := strconv.ParseFloat(inst.PriceEndStep, 64)
		minVol, _ := strconv.ParseFloat(inst.MinTradeNum, 64)

		stateVal := 0
		if inst.SymbolStatus == "online" {
			stateVal = 1
		}

		if priceScale <= 0 && inst.PriceEndStep != "" {
			priceScale = decmath.DecimalPlaces(inst.PriceEndStep)
		}

		if priceUnit >= 1.0 && priceScale > 0 {
			priceUnit *= math.Pow10(-priceScale)
		}

		minLeverage := 1
		if inst.MinLever != "" {
			if l, err := strconv.Atoi(inst.MinLever); err == nil {
				minLeverage = l
			}
		}
		maxLeverage := 100
		if inst.MaxLever != "" {
			if l, err := strconv.Atoi(inst.MaxLever); err == nil {
				maxLeverage = l
			}
		}

		details = append(details, exchange.ContractDetail{
			Symbol:           inst.Symbol,
			DisplayName:      inst.Symbol,
			DisplayNameEn:    inst.Symbol,
			PositionOpenType: 1, // Isolated/Cross.
			BaseCoin:         inst.BaseCoin,
			QuoteCoin:        inst.QuoteCoin,
			SettleCoin:       inst.SettleCoin,
			ContractSize:     1.0, // Defaults to 1 for generic USDT margin linear futures.
			MinLeverage:      minLeverage,
			MaxLeverage:      maxLeverage,
			PriceScale:       priceScale,
			VolScale:         volScale,
			PriceUnit:        priceUnit,
			MinVol:           int(minVol),
			State:            stateVal,
		})
	}

	return details, nil
}

// GetFundingRates returns current funding rate details for the specified symbols.
func (c *Client) GetFundingRates(ctx context.Context, symbols []string) ([]exchange.FundingRateResult, error) {
	if len(symbols) == 0 {
		return nil, nil
	}

	rawRates, err := c.getRawFundingRates(ctx, bitgetFundingRateRequest{
		ProductType: productTypeUsdtFutures,
	})
	if err != nil {
		return nil, err
	}

	rateMap := make(map[string]*rawBitgetFunding)
	for i := range rawRates {
		rateMap[rawRates[i].Symbol] = &rawRates[i]
	}

	rates := make([]exchange.FundingRateResult, 0, len(symbols))
	for _, sym := range symbols {
		raw, ok := rateMap[sym]
		if !ok {
			c.logger.WarnContext(ctx, "Bitget funding rate not found for symbol", slog.String("symbol", sym))
			continue
		}

		fr, _ := strconv.ParseFloat(raw.FundingRate, 64)
		nextUpdateVal, _ := strconv.ParseInt(raw.NextUpdate, 10, 64)

		rates = append(rates, exchange.FundingRateResult{
			Symbol:     sym,
			Rate:       fr,
			SettleTime: nextUpdateVal,
		})
	}

	return rates, nil
}

// GetTickers returns ticker data for all SWAP contracts or a specific instrument.
func (c *Client) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	tickers, err := c.getRawTickers(ctx, bitgetTickersRequest{
		ProductType: productTypeUsdtFutures,
		Symbol:      symbol,
	})
	if err != nil {
		return nil, err
	}

	exchangeTickers := make([]exchange.Ticker, 0, len(tickers))
	for i := range tickers {
		t := &tickers[i]
		last, _ := strconv.ParseFloat(t.LastPr, 64)
		bid, _ := strconv.ParseFloat(t.BidPr, 64)
		ask, _ := strconv.ParseFloat(t.AskPr, 64)
		vol, _ := strconv.ParseFloat(t.BaseVolume, 64)
		amt, _ := strconv.ParseFloat(t.QuoteVolume, 64)
		ts, _ := strconv.ParseInt(t.Ts, 10, 64)

		exchangeTickers = append(exchangeTickers, exchange.Ticker{
			Symbol:       t.Symbol,
			LastPrice:    last,
			Bid1:         bid,
			Ask1:         ask,
			Volume24:     vol,
			AmountUSDT24: amt,
			Timestamp:    ts,
		})
	}

	return exchangeTickers, nil
}

func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	tickers, err := c.GetTickers(ctx, "")
	if err != nil {
		return nil, err
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
		if blacklistMap[t.Symbol] {
			continue
		}
		if len(whitelistMap) > 0 && !whitelistMap[t.Symbol] {
			continue
		}
		if t.AmountUSDT24 < minVol24h {
			continue
		}
		if maxVol24h > 0 && t.AmountUSDT24 > maxVol24h {
			continue
		}

		filteredSymbols = append(filteredSymbols, t.Symbol)
		volMap[t.Symbol] = t.AmountUSDT24
		priceMap[t.Symbol] = t.LastPrice
	}

	if len(filteredSymbols) == 0 {
		return nil, nil
	}

	rates, err := c.GetFundingRates(ctx, filteredSymbols)
	if err != nil {
		return nil, err
	}

	var results []exchange.PotentialFundingResult
	for _, r := range rates {
		results = append(results, exchange.PotentialFundingResult{
			Symbol:     r.Symbol,
			Rate:       r.Rate,
			SettleTime: r.SettleTime,
			Volume24h:  volMap[r.Symbol],
			Price:      priceMap[r.Symbol],
		})
	}

	return results, nil
}
