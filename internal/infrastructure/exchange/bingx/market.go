package bingx

import (
	"context"
	"fmt"
	"math"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"

	"crypto-bot/pkg/xjson"
)

// Explicit request/response structs for market data endpoints.

type bingxServerTimeRequest struct{}
type bingxContractsRequest struct{}

type bingxFundingRateRequest struct {
	Symbol string `json:"symbol,omitempty"`
}

type bingxTickersRequest struct {
	Symbol string `json:"symbol,omitempty"`
}

type bingxTimeData struct {
	ServerTime int64 `json:"serverTime"`
}

type bingxContract struct {
	Symbol            string  `json:"symbol"`
	QuantityPrecision int     `json:"quantityPrecision"`
	PricePrecision    int     `json:"pricePrecision"`
	MakerFeeRate      float64 `json:"makerFeeRate"`
	TakerFeeRate      float64 `json:"takerFeeRate"`
	TradeMinQuantity  float64 `json:"tradeMinQuantity"`
	TradeMinUsdt      float64 `json:"tradeMinUSDT"`
	Currency          string  `json:"currency"`
	Asset             string  `json:"asset"`
	Status            int     `json:"status"`
}

type rawPremiumIndex struct {
	Symbol          string `json:"symbol"`
	LastFundingRate string `json:"lastFundingRate"`
	NextFundingTime int64  `json:"nextFundingTime"`
}

type rawBingxTicker struct {
	Symbol      string `json:"symbol"`
	LastPrice   string `json:"lastPrice"`
	BidPrice    string `json:"bidPrice"`
	AskPrice    string `json:"askPrice"`
	Volume      string `json:"volume"`
	QuoteVolume string `json:"quoteVolume"`
	Time        string `json:"time"`
}

// Private raw methods invoking the BingX REST API.

func (c *Client) getRawServerTime(ctx context.Context, _ bingxServerTimeRequest) (*bingxTimeData, error) {
	body, err := c.GetCtx(ctx, pathServerTime, nil)
	if err != nil {
		return nil, err
	}

	data, err := ParseResponse[bingxTimeData](body, "server_time")
	if err != nil {
		return nil, err
	}
	return &data, nil
}

func (c *Client) getRawContractDetails(ctx context.Context, _ bingxContractsRequest) ([]bingxContract, error) {
	body, err := c.GetCtx(ctx, pathContracts, nil)
	if err != nil {
		return nil, err
	}

	instruments, err := ParseResponse[[]bingxContract](body, "contract_details")
	if err != nil {
		return nil, err
	}
	return instruments, nil
}

func (c *Client) getRawFundingRate(ctx context.Context, req bingxFundingRateRequest) ([]rawPremiumIndex, error) {
	params := make(map[string]string)
	if req.Symbol != "" {
		params[paramSymbol] = req.Symbol
	}

	indexBody, err := c.GetCtx(ctx, pathFundingRate, params)
	if err != nil {
		return nil, err
	}

	var rawIndexes []rawPremiumIndex
	if singleIndex, err := ParseResponse[rawPremiumIndex](indexBody, "premium_index"); err == nil {
		rawIndexes = []rawPremiumIndex{singleIndex}
	} else {
		indexesParsed, err := ParseResponse[[]rawPremiumIndex](indexBody, "premium_index")
		if err != nil {
			return nil, err
		}
		rawIndexes = indexesParsed
	}
	return rawIndexes, nil
}

func (c *Client) getRawTickers(ctx context.Context, req bingxTickersRequest) ([]rawBingxTicker, error) {
	params := make(map[string]string)
	if req.Symbol != "" {
		params[paramSymbol] = req.Symbol
	}

	tickerBody, err := c.GetCtx(ctx, pathTickers, params)
	if err != nil {
		return nil, err
	}

	var rawTickers []rawBingxTicker
	var singleTicker rawBingxTicker
	if err := xjson.Unmarshal(tickerBody, &singleTicker); err == nil && singleTicker.Symbol != "" {
		rawTickers = []rawBingxTicker{singleTicker}
	} else {
		tickersParsed, err := ParseResponse[[]rawBingxTicker](tickerBody, "tickers")
		if err != nil {
			return nil, err
		}
		rawTickers = tickersParsed
	}
	return rawTickers, nil
}

// Public mapper methods implementing the exchange.MarketDataProvider interface.

// GetServerTime returns the BingX server timestamp in milliseconds.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	data, err := c.getRawServerTime(ctx, bingxServerTimeRequest{})
	if err != nil {
		return 0, err
	}
	return data.ServerTime, nil
}

// GetContractDetails returns specifications for all active Swap/Futures contracts.
func (c *Client) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	instruments, err := c.getRawContractDetails(ctx, bingxContractsRequest{})
	if err != nil {
		return nil, err
	}

	details := make([]exchange.ContractDetail, 0, len(instruments))
	for i := range instruments {
		inst := &instruments[i]

		stateVal := 0
		if inst.Status == 1 {
			stateVal = 1
		}

		priceUnit := math.Pow10(-inst.PricePrecision)

		details = append(details, exchange.ContractDetail{
			Symbol:           inst.Symbol,
			DisplayName:      inst.Symbol,
			DisplayNameEn:    inst.Symbol,
			PositionOpenType: 1, // Isolated & Cross supported.
			BaseCoin:         inst.Asset,
			QuoteCoin:        inst.Currency,
			SettleCoin:       inst.Currency,
			ContractSize:     1.0,
			MinLeverage:      1,
			MaxLeverage:      100,
			PriceScale:       inst.PricePrecision,
			VolScale:         inst.QuantityPrecision,
			PriceUnit:        priceUnit,
			MinVol:           int(inst.TradeMinQuantity),
			State:            stateVal,
		})
	}

	return details, nil
}

// GetTickers returns ticker data combined with premium index (funding rate and mark price).
func (c *Client) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	rawTickers, err := c.getRawTickers(ctx, bingxTickersRequest{Symbol: symbol})
	if err != nil {
		return nil, err
	}

	exchangeTickers := make([]exchange.Ticker, 0, len(rawTickers))
	for i := range rawTickers {
		t := &rawTickers[i]

		last := decmath.ParseFloat(t.LastPrice)
		bid := decmath.ParseFloat(t.BidPrice)
		ask := decmath.ParseFloat(t.AskPrice)
		vol := decmath.ParseFloat(t.Volume)
		amt := decmath.ParseFloat(t.QuoteVolume)
		ts := decmath.ParseInt64(t.Time)

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

// GetFundingRates returns current funding rate details for the specified symbols.
func (c *Client) GetFundingRates(ctx context.Context, symbols []string) ([]exchange.FundingRateResult, error) {
	if len(symbols) == 0 {
		return nil, nil
	}
	rates := make([]exchange.FundingRateResult, 0, len(symbols))
	if len(symbols) > 1 {
		rawList, err := c.getRawFundingRate(ctx, bingxFundingRateRequest{})
		if err != nil {
			return nil, err
		}
		rawMap := make(map[string]rawPremiumIndex)
		for _, raw := range rawList {
			rawMap[raw.Symbol] = raw
		}
		for _, sym := range symbols {
			raw, ok := rawMap[sym]
			if !ok {
				return nil, fmt.Errorf("bingx funding rate not found for symbol: %s", sym)
			}
			rates = append(rates, exchange.FundingRateResult{
				Symbol:     raw.Symbol,
				Rate:       decmath.ParseFloat(raw.LastFundingRate),
				SettleTime: raw.NextFundingTime,
			})
		}
		return rates, nil
	}

	// Single symbol path
	sym := symbols[0]
	rawList, err := c.getRawFundingRate(ctx, bingxFundingRateRequest{Symbol: sym})
	if err != nil {
		return nil, err
	}
	if len(rawList) == 0 {
		return nil, fmt.Errorf("bingx funding rate not found for symbol: %s", sym)
	}
	raw := &rawList[0]
	rates = append(rates, exchange.FundingRateResult{
		Symbol:     raw.Symbol,
		Rate:       decmath.ParseFloat(raw.LastFundingRate),
		SettleTime: raw.NextFundingTime,
	})
	return rates, nil
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

func (c *Client) GetTopGainer(_ context.Context, _ exchange.TopGainerRequest) ([]exchange.TopGainerResult, error) {
	return nil, nil
}
