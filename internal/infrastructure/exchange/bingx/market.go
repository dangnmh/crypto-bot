package bingx

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"

	"crypto-bot/internal/infrastructure/exchange"
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
		Symbol      string      `json:"symbol"`
		LastPrice   interface{} `json:"lastPrice"`
		BidPrice    interface{} `json:"bidPrice"`
		AskPrice    interface{} `json:"askPrice"`
		Volume      interface{} `json:"volume"`
		QuoteVolume interface{} `json:"quoteVolume"`
		Time        interface{} `json:"time"`
	}

	var rawTickers []bingxTicker
	// If a single symbol is queried, the exchange might return an object or array.
	// Let's support both.
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
		Symbol          string      `json:"symbol"`
		MarkPrice       interface{} `json:"markPrice"`
		LastFundingRate interface{} `json:"lastFundingRate"`
		NextFundingTime interface{} `json:"nextFundingTime"`
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

		last := parseFloat(t.LastPrice)
		bid := parseFloat(t.BidPrice)
		ask := parseFloat(t.AskPrice)
		vol := parseFloat(t.Volume)
		amt := parseFloat(t.QuoteVolume)
		ts := parseInt64(t.Time)

		var mark float64 = last
		var fr float64

		if idx, ok := indexMap[t.Symbol]; ok {
			mark = parseFloat(idx.MarkPrice)
			fr = parseFloat(idx.LastFundingRate)
		}

		exchangeTickers = append(exchangeTickers, exchange.Ticker{
			Symbol:      t.Symbol,
			LastPrice:   last,
			Bid1:        bid,
			Ask1: ask,
			Volume24:    vol,
			Amount24:    amt,
			FairPrice:   mark,
			FundingRate: fr,
			Timestamp:   ts,
		})
	}

	return exchangeTickers, nil
}

// GetFundingRate returns current funding rate details for a specific symbol.
func (c *Client) GetFundingRate(ctx context.Context, symbol string) (*exchange.FundingRateDetail, error) {
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required for GetFundingRate")
	}

	params := map[string]string{
		paramSymbol: symbol,
	}

	body, err := c.GetCtx(ctx, pathFundingRate, params)
	if err != nil {
		return nil, err
	}

	type bingxPremiumIndex struct {
		Symbol          string      `json:"symbol"`
		LastFundingRate interface{} `json:"lastFundingRate"`
		NextFundingTime interface{} `json:"nextFundingTime"`
	}

	var rawIndexes []bingxPremiumIndex
	var singleIndex bingxPremiumIndex
	if err := json.Unmarshal(body, &singleIndex); err == nil && singleIndex.Symbol != "" {
		rawIndexes = []bingxPremiumIndex{singleIndex}
	} else {
		indexesParsed, err := ParseResponse[[]bingxPremiumIndex](body, "funding_rate")
		if err != nil {
			return nil, err
		}
		rawIndexes = indexesParsed
	}

	if len(rawIndexes) == 0 {
		return nil, fmt.Errorf("empty premium index for %s", symbol)
	}

	idx := &rawIndexes[0]
	fr := parseFloat(idx.LastFundingRate)
	nextSettle := parseInt64(idx.NextFundingTime)

	return &exchange.FundingRateDetail{
		Symbol:         idx.Symbol,
		FundingRate:    fr,
		NextSettleTime: nextSettle,
		Timestamp:      nextSettle - 8*3600*1000,
	}, nil
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

	var rawData []json.RawMessage
	parsedData, err := ParseResponse[[]json.RawMessage](body, "klines")
	if err != nil {
		return nil, err
	}
	rawData = parsedData

	type bingxKlineObj struct {
		OpenTime  interface{} `json:"openTime"`
		Open      interface{} `json:"open"`
		High      interface{} `json:"high"`
		Low       interface{} `json:"low"`
		Close     interface{} `json:"close"`
		Volume    interface{} `json:"volume"`
		Time      interface{} `json:"time"`
		O         interface{} `json:"o"`
		H         interface{} `json:"h"`
		L         interface{} `json:"l"`
		C         interface{} `json:"c"`
		V         interface{} `json:"v"`
		T         interface{} `json:"t"`
	}

	klines := make([]exchange.Kline, 0, len(rawData))
	for _, row := range rawData {
		var listRow []interface{}
		if err := json.Unmarshal(row, &listRow); err == nil && len(listRow) >= 6 {
			// Array format: [openTime, open, high, low, close, volume, ...]
			ts := parseInt64(listRow[0])
			o := parseFloat(listRow[1])
			h := parseFloat(listRow[2])
			l := parseFloat(listRow[3])
			cVal := parseFloat(listRow[4])
			v := parseFloat(listRow[5])

			klines = append(klines, exchange.Kline{
				Timestamp: ts,
				Open:      o,
				High:      h,
				Low:       l,
				Close:     cVal,
				Volume:    v,
			})
			continue
		}

		var objRow bingxKlineObj
		if err := json.Unmarshal(row, &objRow); err == nil {
			var ts int64
			if objRow.Time != nil {
				ts = parseInt64(objRow.Time)
			} else if objRow.OpenTime != nil {
				ts = parseInt64(objRow.OpenTime)
			} else {
				ts = parseInt64(objRow.T)
			}

			var o, h, l, cVal, v float64
			if objRow.Open != nil {
				o = parseFloat(objRow.Open)
			} else {
				o = parseFloat(objRow.O)
			}
			if objRow.High != nil {
				h = parseFloat(objRow.High)
			} else {
				h = parseFloat(objRow.H)
			}
			if objRow.Low != nil {
				l = parseFloat(objRow.Low)
			} else {
				l = parseFloat(objRow.L)
			}
			if objRow.Close != nil {
				cVal = parseFloat(objRow.Close)
			} else {
				cVal = parseFloat(objRow.C)
			}
			if objRow.Volume != nil {
				v = parseFloat(objRow.Volume)
			} else {
				v = parseFloat(objRow.V)
			}

			klines = append(klines, exchange.Kline{
				Timestamp: ts,
				Open:      o,
				High:      h,
				Low:       l,
				Close:     cVal,
				Volume:    v,
			})
		}
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
		Asks [][]interface{} `json:"asks"`
		Bids [][]interface{} `json:"bids"`
		Ts   interface{}     `json:"ts"`
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
		p := parseFloat(level[0])
		v := parseFloat(level[1])
		ob.Asks = append(ob.Asks, exchange.OrderBookEntry{Price: p, Volume: v})
	}

	for _, level := range book.Bids {
		if len(level) < 2 {
			continue
		}
		p := parseFloat(level[0])
		v := parseFloat(level[1])
		ob.Bids = append(ob.Bids, exchange.OrderBookEntry{Price: p, Volume: v})
	}

	return ob, nil
}

// GetDepthCommits is not supported on BingX REST.
func (c *Client) GetDepthCommits(ctx context.Context, symbol string, limit int) ([]exchange.DepthCommit, error) {
	return nil, fmt.Errorf("GetDepthCommits not supported on BingX REST")
}

func parseFloat(v interface{}) float64 {
	if v == nil {
		return 0
	}
	switch val := v.(type) {
	case float64:
		return val
	case string:
		f, _ := strconv.ParseFloat(val, 64)
		return f
	case int64:
		return float64(val)
	case int:
		return float64(val)
	}
	return 0
}

func parseInt64(v interface{}) int64 {
	if v == nil {
		return 0
	}
	switch val := v.(type) {
	case float64:
		return int64(val)
	case int64:
		return val
	case int:
		return int64(val)
	case string:
		i, _ := strconv.ParseInt(val, 10, 64)
		return i
	}
	return 0
}
