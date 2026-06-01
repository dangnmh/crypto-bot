package bingx

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
)

// GetServerTime returns the BingX server timestamp in milliseconds.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	body, err := c.GetCtx(ctx, pathServerTime, nil)
	if err != nil {
		return 0, err
	}

	type bingxTimeData struct {
		ServerTime int64 `json:"serverTime"`
	}

	data, err := ParseResponse[bingxTimeData](body, "server_time")
	if err != nil {
		return 0, err
	}

	return data.ServerTime, nil
}

// GetContractDetails returns specifications for all active Swap/Futures contracts.
func (c *Client) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	body, err := c.GetCtx(ctx, pathContracts, nil)
	if err != nil {
		return nil, err
	}

	type bingxContract struct {
		Symbol            string  `json:"symbol"`
		QuantityPrecision int     `json:"quantity_precision"`
		PricePrecision    int     `json:"price_precision"`
		MakerFeeRate      float64 `json:"maker_fee_rate"`
		TakerFeeRate      float64 `json:"taker_fee_rate"`
		TradeMinQuantity  float64 `json:"trade_min_quantity"`
		TradeMinUsdt      float64 `json:"trade_min_usdt"`
		Currency          string  `json:"currency"`
		Asset             string  `json:"asset"`
		Status            int     `json:"status"`
	}

	instruments, err := ParseResponse[[]bingxContract](body, "contract_details")
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
			PositionOpenType: 1, // Isolated & Cross supported
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

func (c *Client) getBingXVolumes24h(ctx context.Context, symbol string) (vols map[string]float64, amts map[string]float64, lasts map[string]float64, err error) {
	params := make(map[string]string)
	if symbol != "" {
		params[paramSymbol] = symbol
	}

	tickerBody, err := c.GetCtx(ctx, pathTickers, params)
	if err != nil {
		return nil, nil, nil, err
	}

	type bingxTicker struct {
		Symbol      string `json:"symbol"`
		LastPrice   string `json:"lastPrice"`
		Volume      string `json:"volume"`
		QuoteVolume string `json:"quoteVolume"`
	}

	var rawTickers []bingxTicker
	var singleTicker bingxTicker
	if err := json.Unmarshal(tickerBody, &singleTicker); err == nil && singleTicker.Symbol != "" {
		rawTickers = []bingxTicker{singleTicker}
	} else {
		tickersParsed, err := ParseResponse[[]bingxTicker](tickerBody, "tickers")
		if err != nil {
			return nil, nil, nil, err
		}
		rawTickers = tickersParsed
	}

	vols = make(map[string]float64)
	amts = make(map[string]float64)
	lasts = make(map[string]float64)
	for i := range rawTickers {
		t := &rawTickers[i]
		vols[t.Symbol] = decmath.ParseFloat(t.Volume)
		amts[t.Symbol] = decmath.ParseFloat(t.QuoteVolume)
		lasts[t.Symbol] = decmath.ParseFloat(t.LastPrice)
	}

	return vols, amts, lasts, nil
}

func (c *Client) getBingXFundingRates(ctx context.Context, symbol string) ([]exchange.FundingRateResult, error) {
	params := make(map[string]string)
	if symbol != "" {
		params[paramSymbol] = symbol
	}

	indexBody, err := c.GetCtx(ctx, pathFundingRate, params)
	if err != nil {
		return nil, err
	}

	_, amounts, _, _ := c.getBingXVolumes24h(ctx, symbol)

	type bingxPremiumIndex struct {
		Symbol          string `json:"symbol"`
		LastFundingRate string `json:"lastFundingRate"`
		NextFundingTime int64  `json:"nextFundingTime"`
	}

	var rawIndexes []bingxPremiumIndex
	var singleIndex bingxPremiumIndex
	if err := json.Unmarshal(indexBody, &singleIndex); err == nil && singleIndex.Symbol != "" {
		rawIndexes = []bingxPremiumIndex{singleIndex}
	} else {
		indexesParsed, err := ParseResponse[[]bingxPremiumIndex](indexBody, "premium_index")
		if err != nil {
			return nil, err
		}
		rawIndexes = indexesParsed
	}

	rates := make([]exchange.FundingRateResult, 0, len(rawIndexes))
	for i := range rawIndexes {
		idx := &rawIndexes[i]
		var vol float64
		if amounts != nil {
			vol = amounts[idx.Symbol]
		}
		rates = append(rates, exchange.FundingRateResult{
			Symbol:     idx.Symbol,
			Rate:       decmath.ParseFloat(idx.LastFundingRate),
			SettleTime: idx.NextFundingTime,
			Volume24h:  vol,
		})
	}

	return rates, nil
}

// GetTickers returns ticker data combined with premium index (funding rate and mark price).
func (c *Client) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	params := make(map[string]string)
	if symbol != "" {
		params[paramSymbol] = symbol
	}

	// 1. Fetch 24h ticker info
	tickerBody, err := c.GetCtx(ctx, pathTickers, params)
	if err != nil {
		return nil, err
	}

	type bingxTicker struct {
		Symbol      string `json:"symbol"`
		LastPrice   string `json:"lastPrice"`
		BidPrice    string `json:"bidPrice"`
		AskPrice    string `json:"askPrice"`
		Volume      string `json:"volume"`
		QuoteVolume string `json:"quoteVolume"`
		Time        string `json:"time"`
	}

	var rawTickers []bingxTicker
	var singleTicker bingxTicker
	if err := json.Unmarshal(tickerBody, &singleTicker); err == nil && singleTicker.Symbol != "" {
		rawTickers = []bingxTicker{singleTicker}
	} else {
		tickersParsed, err := ParseResponse[[]bingxTicker](tickerBody, "tickers")
		if err != nil {
			return nil, err
		}
		rawTickers = tickersParsed
	}

	// 2. Fetch Premium Index (funding rates & mark prices)
	indexBody, err := c.GetCtx(ctx, pathFundingRate, params)
	if err != nil {
		return nil, err
	}

	type bingxPremiumIndex struct {
		Symbol          string `json:"symbol"`
		MarkPrice       string `json:"markPrice"`
		LastFundingRate string `json:"lastFundingRate"`
		NextFundingTime int64  `json:"nextFundingTime"`
	}

	var rawIndexes []bingxPremiumIndex
	var singleIndex bingxPremiumIndex
	if err := json.Unmarshal(indexBody, &singleIndex); err == nil && singleIndex.Symbol != "" {
		rawIndexes = []bingxPremiumIndex{singleIndex}
	} else {
		indexesParsed, err := ParseResponse[[]bingxPremiumIndex](indexBody, "premium_index")
		if err != nil {
			return nil, err
		}
		rawIndexes = indexesParsed
	}

	indexMap := make(map[string]*bingxPremiumIndex)
	for i := range rawIndexes {
		idx := &rawIndexes[i]
		indexMap[idx.Symbol] = idx
	}

	// 3. Merge ticker and premium index
	exchangeTickers := make([]exchange.Ticker, 0, len(rawTickers))
	for i := range rawTickers {
		t := &rawTickers[i]

		last := decmath.ParseFloat(t.LastPrice)
		bid := decmath.ParseFloat(t.BidPrice)
		ask := decmath.ParseFloat(t.AskPrice)
		vol := decmath.ParseFloat(t.Volume)
		amt := decmath.ParseFloat(t.QuoteVolume)
		ts := decmath.ParseInt64(t.Time)

		var fr float64
		var nextSettle int64

		if idx, ok := indexMap[t.Symbol]; ok {
			fr = decmath.ParseFloat(idx.LastFundingRate)
			nextSettle = idx.NextFundingTime
		}

		exchangeTickers = append(exchangeTickers, exchange.Ticker{
			Symbol:         t.Symbol,
			LastPrice:      last,
			Bid1:           bid,
			Ask1:           ask,
			Volume24:       vol,
			Amount24:       amt,
			FundingRate:    fr,
			NextSettleTime: nextSettle,
			Timestamp:      ts,
		})
	}

	return exchangeTickers, nil
}

// GetFundingRates returns current funding rate details for all active symbols.
func (c *Client) GetFundingRates(ctx context.Context) ([]exchange.FundingRateResult, error) {
	return c.getBingXFundingRates(ctx, "")
}

// GetKlines returns candlestick data for a symbol. Supports both array-of-arrays and array-of-objects formats.
func (c *Client) GetKlines(ctx context.Context, symbol, interval string, start, end int64) ([]exchange.Kline, error) {
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required for GetKlines")
	}

	gran := "1m"
	if interval == "Min1" || interval == "1m" {
		gran = "1m"
	}

	params := map[string]string{
		paramSymbol: symbol,
		"interval":  gran,
		paramLimit:  "100",
	}

	if start > 0 {
		params["startTime"] = fmt.Sprintf("%d", start)
	}
	if end > 0 {
		params["endTime"] = fmt.Sprintf("%d", end)
	}

	body, err := c.GetCtx(ctx, pathKlines, params)
	if err != nil {
		return nil, err
	}

	type bingxKlineV3 struct {
		Open   string `json:"open"`
		Close  string `json:"close"`
		High   string `json:"high"`
		Low    string `json:"low"`
		Volume string `json:"volume"`
		Time   int64  `json:"time"`
	}

	klinesParsed, err := ParseResponse[[]bingxKlineV3](body, "klines")
	if err != nil {
		return nil, err
	}

	klines := make([]exchange.Kline, 0, len(klinesParsed))
	for _, row := range klinesParsed {
		o := decmath.ParseFloat(row.Open)
		cVal := decmath.ParseFloat(row.Close)
		h := decmath.ParseFloat(row.High)
		l := decmath.ParseFloat(row.Low)
		v := decmath.ParseFloat(row.Volume)
		ts := row.Time

		klines = append(klines, exchange.Kline{
			Timestamp: ts,
			Open:      o,
			High:      h,
			Low:       l,
			Close:     cVal,
			Volume:    v,
		})
	}

	return klines, nil
}

// GetDepthSnapshot returns orderbook snapshot for a symbol.
func (c *Client) GetDepthSnapshot(ctx context.Context, symbol string, limit int) (*exchange.OrderBook, error) {
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required for GetDepthSnapshot")
	}

	limitStr := "100"
	if limit > 0 {
		limitStr = strconv.Itoa(limit)
	}

	params := map[string]string{
		paramSymbol: symbol,
		paramLimit:  limitStr,
	}

	body, err := c.GetCtx(ctx, pathDepth, params)
	if err != nil {
		return nil, err
	}

	type bingxDepth struct {
		Asks [][]string `json:"asks"`
		Bids [][]string `json:"bids"`
		Ts   string     `json:"ts"`
	}

	book, err := ParseResponse[bingxDepth](body, "depth_snapshot")
	if err != nil {
		return nil, err
	}

	ob := &exchange.OrderBook{
		Symbol: symbol,
		Asks:   make([]exchange.OrderBookEntry, 0, len(book.Asks)),
		Bids:   make([]exchange.OrderBookEntry, 0, len(book.Bids)),
	}

	for _, level := range book.Asks {
		if len(level) < 2 {
			continue
		}
		p := decmath.ParseFloat(level[0])
		v := decmath.ParseFloat(level[1])
		ob.Asks = append(ob.Asks, exchange.OrderBookEntry{Price: p, Volume: v})
	}

	for _, level := range book.Bids {
		if len(level) < 2 {
			continue
		}
		p := decmath.ParseFloat(level[0])
		v := decmath.ParseFloat(level[1])
		ob.Bids = append(ob.Bids, exchange.OrderBookEntry{Price: p, Volume: v})
	}

	return ob, nil
}

// GetDepthCommits is not supported on BingX REST.
func (c *Client) GetDepthCommits(ctx context.Context, symbol string, limit int) ([]exchange.DepthCommit, error) {
	return nil, fmt.Errorf("GetDepthCommits not supported on BingX REST")
}
