package kucoin

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"crypto-bot/internal/infrastructure/exchange"
)

// GetServerTime returns the KuCoin server timestamp in milliseconds.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	body, err := c.GetCtx(ctx, pathServerTime, nil)
	if err != nil {
		return 0, err
	}

	var numVal int64
	if err := json.Unmarshal(body, &numVal); err == nil {
		return numVal, nil
	}

	data, err := ParseResponse[int64](body, "server_time")
	if err != nil {
		return 0, err
	}

	return data, nil
}

// GetContractDetails returns specifications for all active Futures contracts.
func (c *Client) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	body, err := c.GetCtx(ctx, pathContracts, nil)
	if err != nil {
		return nil, err
	}

	type kucoinContract struct {
		Symbol         string      `json:"symbol"`
		BaseCurrency   string      `json:"baseCurrency"`
		QuoteCurrency  string      `json:"quoteCurrency"`
		SettleCurrency string      `json:"settleCurrency"`
		LotSize        interface{} `json:"lotSize"`
		TickSize       interface{} `json:"tickSize"`
		Multiplier     interface{} `json:"multiplier"`
		Status         string      `json:"status"`
	}

	instruments, err := ParseResponse[[]kucoinContract](body, "contract_details")
	if err != nil {
		return nil, err
	}

	details := make([]exchange.ContractDetail, 0, len(instruments))
	for i := range instruments {
		inst := &instruments[i]

		stateVal := 0
		if inst.Status == "Open" {
			stateVal = 1
		}

		lotSize := parseFloat(inst.LotSize)
		tickSize := parseFloat(inst.TickSize)

		priceScale := getDecimals(fmt.Sprintf("%g", tickSize))
		if priceScale <= 0 {
			priceScale = 2
		}

		details = append(details, exchange.ContractDetail{
			Symbol:           inst.Symbol,
			DisplayName:      inst.Symbol,
			DisplayNameEn:    inst.Symbol,
			PositionOpenType: 1, // Isolated/Cross both supported
			BaseCoin:         inst.BaseCurrency,
			QuoteCoin:        inst.QuoteCurrency,
			SettleCoin:       inst.SettleCurrency,
			ContractSize:     lotSize,
			MinLeverage:      1,
			MaxLeverage:      100,
			PriceScale:       priceScale,
			VolScale:         0, // Defaults to 0 (unit contracts)
			PriceUnit:        tickSize,
			MinVol:           1,
			State:            stateVal,
		})
	}

	return details, nil
}

// GetTickers returns ticker data for all contracts.
func (c *Client) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	if symbol != "" {
		params := map[string]string{
			paramSymbol: symbol,
		}
		body, err := c.GetCtx(ctx, pathTickerSingle, params)
		if err != nil {
			return nil, err
		}

		type kucoinSingleTicker struct {
			Symbol       string      `json:"symbol"`
			BestBidPrice interface{} `json:"bestBidPrice"`
			BestAskPrice interface{} `json:"bestAskPrice"`
			Price        interface{} `json:"price"`
			Size         interface{} `json:"size"`
			Ts           interface{} `json:"ts"`
		}

		raw, err := ParseResponse[kucoinSingleTicker](body, "ticker_single")
		if err != nil {
			return nil, err
		}

		last := parseFloat(raw.Price)
		bid := parseFloat(raw.BestBidPrice)
		ask := parseFloat(raw.BestAskPrice)
		ts := parseInt64(raw.Ts)

		return []exchange.Ticker{
			{
				Symbol:    raw.Symbol,
				LastPrice: last,
				Bid1:      bid,
				Ask1:      ask,
				FairPrice: last,
				Timestamp: ts,
			},
		}, nil
	}

	body, err := c.GetCtx(ctx, pathTickers, nil)
	if err != nil {
		return nil, err
	}

	type kucoinTicker struct {
		Symbol       string      `json:"symbol"`
		BestBidPrice interface{} `json:"bestBidPrice"`
		BestAskPrice interface{} `json:"bestAskPrice"`
		LastPrice    interface{} `json:"lastPrice"`
		Price        interface{} `json:"price"`
		Volume       interface{} `json:"volume"`
		Vol          interface{} `json:"vol"`
		Ts           interface{} `json:"ts"`
	}

	tickers, err := ParseResponse[[]kucoinTicker](body, "tickers")
	if err != nil {
		return nil, err
	}

	// Fetch active contracts in bulk to populate funding rates & next settle times without separate API calls
	cMap := make(map[string]float64)
	cTimeMap := make(map[string]int64)
	cBody, err := c.GetCtx(ctx, pathContracts, nil)
	if err == nil {
		type kucoinContractFunding struct {
			Symbol              string      `json:"symbol"`
			FundingRate         interface{} `json:"fundingRate"`
			NextFundingRateTime interface{} `json:"nextFundingRateTime"`
		}
		cList, err := ParseResponse[[]kucoinContractFunding](cBody, "contracts_active")
		if err == nil {
			for i := range cList {
				fr := parseFloat(cList[i].FundingRate)
				ns := parseInt64(cList[i].NextFundingRateTime)
				cMap[cList[i].Symbol] = fr
				cTimeMap[cList[i].Symbol] = ns
			}
		}
	}

	exchangeTickers := make([]exchange.Ticker, 0, len(tickers))
	for i := range tickers {
		t := &tickers[i]

		last := parseFloat(t.LastPrice)
		if last == 0 {
			last = parseFloat(t.Price)
		}

		bid := parseFloat(t.BestBidPrice)
		ask := parseFloat(t.BestAskPrice)
		vol := parseFloat(t.Volume)
		if vol == 0 {
			vol = parseFloat(t.Vol)
		}
		ts := parseInt64(t.Ts)

		fr := cMap[t.Symbol]
		ns := cTimeMap[t.Symbol]

		exchangeTickers = append(exchangeTickers, exchange.Ticker{
			Symbol:         t.Symbol,
			LastPrice:      last,
			Bid1:           bid,
			Ask1:           ask,
			Volume24:       vol,
			Amount24:       vol * last, // Estimate amount if not directly provided
			FairPrice:      last,
			FundingRate:    fr,
			NextSettleTime: ns,
			Timestamp:      ts,
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

	type kucoinFundingRate struct {
		Symbol          string      `json:"symbol"`
		FundingRate     interface{} `json:"fundingRate"`
		NextFundingTime interface{} `json:"nextFundingTime"`
	}

	raw, err := ParseResponse[kucoinFundingRate](body, "funding_rate")
	if err != nil {
		return nil, err
	}

	fr := parseFloat(raw.FundingRate)
	nextSettle := parseInt64(raw.NextFundingTime)

	return &exchange.FundingRateDetail{
		Symbol:         raw.Symbol,
		FundingRate:    fr,
		NextSettleTime: nextSettle,
		Timestamp:      nextSettle - 8*3600*1000,
	}, nil
}

// GetKlines returns candlestick data for a symbol.
func (c *Client) GetKlines(ctx context.Context, symbol, interval string, start, end int64) ([]exchange.Kline, error) {
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required for GetKlines")
	}

	gran := "1"
	if interval == "Min1" || interval == "1m" {
		gran = "1"
	}

	params := map[string]string{
		paramSymbol:   symbol,
		"granularity": gran,
	}

	if start > 0 {
		params["from"] = fmt.Sprintf("%d", start)
	}
	if end > 0 {
		params["to"] = fmt.Sprintf("%d", end)
	}

	body, err := c.GetCtx(ctx, pathKlines, params)
	if err != nil {
		return nil, err
	}

	var rawRows [][]interface{}
	parsedRows, err := ParseResponse[[][]interface{}](body, "klines")
	if err != nil {
		return nil, err
	}
	rawRows = parsedRows

	klines := make([]exchange.Kline, 0, len(rawRows))
	// KuCoin returns newest first. Let's reverse to ascending.
	for i := len(rawRows) - 1; i >= 0; i-- {
		row := rawRows[i]
		if len(row) < 6 {
			continue
		}

		ts := parseInt64(row[0])
		o := parseFloat(row[1])
		h := parseFloat(row[2])
		l := parseFloat(row[3])
		cVal := parseFloat(row[4])
		v := parseFloat(row[5])

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

	params := map[string]string{
		paramSymbol: symbol,
	}

	body, err := c.GetCtx(ctx, pathDepth, params)
	if err != nil {
		return nil, err
	}

	type kucoinDepth struct {
		Asks [][]interface{} `json:"asks"`
		Bids [][]interface{} `json:"bids"`
		Ts   interface{}     `json:"ts"`
	}

	book, err := ParseResponse[kucoinDepth](body, "depth_snapshot")
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

// GetDepthCommits is not supported on KuCoin REST.
func (c *Client) GetDepthCommits(ctx context.Context, symbol string, limit int) ([]exchange.DepthCommit, error) {
	return nil, fmt.Errorf("GetDepthCommits not supported on KuCoin REST")
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

func getDecimals(s string) int {
	parts := strings.Split(s, ".")
	if len(parts) < 2 {
		return 0
	}
	return len(parts[1])
}
