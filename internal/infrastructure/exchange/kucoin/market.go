package kucoin

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
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
		Symbol         string  `json:"symbol"`
		BaseCurrency   string  `json:"baseCurrency"`
		QuoteCurrency  string  `json:"quoteCurrency"`
		SettleCurrency string  `json:"settleCurrency"`
		LotSize        int64   `json:"lotSize"`
		TickSize       float64 `json:"tickSize"`
		Multiplier     float64 `json:"multiplier"`
		Status         string  `json:"status"`
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

		lotSize := float64(inst.LotSize)
		tickSize := inst.TickSize

		priceScale := decmath.DecimalPlaces(fmt.Sprintf("%g", tickSize))
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
			Symbol       string `json:"symbol"`
			BestBidPrice string `json:"bestBidPrice"`
			BestAskPrice string `json:"bestAskPrice"`
			Price        string `json:"price"`
			Size         string `json:"size"`
			Ts           string `json:"ts"`
		}

		raw, err := ParseResponse[kucoinSingleTicker](body, "ticker_single")
		if err != nil {
			return nil, err
		}

		last := decmath.ParseFloat(raw.Price)
		bid := decmath.ParseFloat(raw.BestBidPrice)
		ask := decmath.ParseFloat(raw.BestAskPrice)
		ts := decmath.ParseInt64(raw.Ts)

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
		Symbol       string `json:"symbol"`
		BestBidPrice string `json:"bestBidPrice"`
		BestAskPrice string `json:"bestAskPrice"`
		LastPrice    string `json:"lastPrice"`
		Price        string `json:"price"`
		Volume       string `json:"volume"`
		Vol          string `json:"vol"`
		Ts           int64  `json:"ts"`
	}

	tickers, err := ParseResponse[[]kucoinTicker](body, "tickers")
	if err != nil {
		return nil, err
	}

	// Fetch active contracts in bulk to populate funding rates & next settle times without separate API calls
	cMap := make(map[string]float64)
	cTimeMap := make(map[string]int64)
	cVolMap := make(map[string]float64)
	cAmtMap := make(map[string]float64)
	cBody, err := c.GetCtx(ctx, pathContracts, nil)
	if err == nil {
		type kucoinContractFunding struct {
			Symbol                  string  `json:"symbol"`
			FundingFeeRate          float64 `json:"fundingFeeRate"`
			NextFundingRateDateTime int64   `json:"nextFundingRateDateTime"`
			TurnoverOf24h           float64 `json:"turnoverOf24h"`
			VolumeOf24h             float64 `json:"volumeOf24h"`
		}
		cList, err := ParseResponse[[]kucoinContractFunding](cBody, "contracts_active")
		if err == nil {
			for i := range cList {
				fr := cList[i].FundingFeeRate
				ns := cList[i].NextFundingRateDateTime
				cMap[cList[i].Symbol] = fr
				cTimeMap[cList[i].Symbol] = ns
				cVolMap[cList[i].Symbol] = cList[i].VolumeOf24h
				cAmtMap[cList[i].Symbol] = cList[i].TurnoverOf24h
			}
		}
	}

	exchangeTickers := make([]exchange.Ticker, 0, len(tickers))
	for i := range tickers {
		t := &tickers[i]

		last := decmath.ParseFloat(t.LastPrice)
		if last == 0 {
			last = decmath.ParseFloat(t.Price)
		}

		bid := decmath.ParseFloat(t.BestBidPrice)
		ask := decmath.ParseFloat(t.BestAskPrice)
		ts := t.Ts

		vol := cVolMap[t.Symbol]
		amt := cAmtMap[t.Symbol]
		if vol == 0 {
			vol = decmath.ParseFloat(t.Volume)
			if vol == 0 {
				vol = decmath.ParseFloat(t.Vol)
			}
			amt = vol * last
		}

		fr := cMap[t.Symbol]
		ns := cTimeMap[t.Symbol]

		exchangeTickers = append(exchangeTickers, exchange.Ticker{
			Symbol:         t.Symbol,
			LastPrice:      last,
			Bid1:           bid,
			Ask1:           ask,
			Volume24:       vol,
			Amount24:       amt,
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

	path := fmt.Sprintf("/api/v1/funding-rate/%s/current", symbol)
	body, err := c.GetCtx(ctx, path, nil)
	if err != nil {
		return nil, err
	}

	type kucoinFundingRate struct {
		Symbol      string  `json:"symbol"`
		Value       float64 `json:"value"`
		FundingTime int64   `json:"fundingTime"`
	}

	raw, err := ParseResponse[kucoinFundingRate](body, "funding_rate")
	if err != nil {
		return nil, err
	}

	fr := (raw.Value)
	nextSettle := raw.FundingTime

	return &exchange.FundingRateDetail{
		Symbol:         symbol,
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

	var rawRows [][]float64
	parsedRows, err := ParseResponse[[][]float64](body, "klines")
	if err != nil {
		return nil, err
	}
	rawRows = parsedRows

	klines := make([]exchange.Kline, 0, len(rawRows))
	// KuCoin returns newest first. Let's reverse to ascending.
	for _, row := range slices.Backward(rawRows) {
		if len(row) < 6 {
			continue
		}

		ts := int64(row[0])
		o := (row[1])
		h := (row[2])
		l := (row[3])
		cVal := (row[4])
		v := (row[5])

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
		Asks [][]float64 `json:"asks"`
		Bids [][]float64 `json:"bids"`
		Ts   int64       `json:"ts"`
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
		p := (level[0])
		v := (level[1])
		ob.Asks = append(ob.Asks, exchange.OrderBookEntry{Price: p, Volume: v})
	}

	for _, level := range book.Bids {
		if len(level) < 2 {
			continue
		}
		p := (level[0])
		v := (level[1])
		ob.Bids = append(ob.Bids, exchange.OrderBookEntry{Price: p, Volume: v})
	}

	return ob, nil
}

// GetDepthCommits is not supported on KuCoin REST.
func (c *Client) GetDepthCommits(ctx context.Context, symbol string, limit int) ([]exchange.DepthCommit, error) {
	return nil, fmt.Errorf("GetDepthCommits not supported on KuCoin REST")
}
