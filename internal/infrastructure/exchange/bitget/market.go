package bitget

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
)

// GetServerTime returns the Bitget server timestamp in milliseconds.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	body, err := c.GetCtx(ctx, pathServerTime, nil)
	if err != nil {
		return 0, err
	}

	var resp struct {
		Code string          `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, fmt.Errorf("parse server time response: %w", err)
	}
	if resp.Code != "00000" {
		return 0, toAPIError(0, resp.Msg, "server_time")
	}

	var strVal string
	if err := json.Unmarshal(resp.Data, &strVal); err == nil {
		val, err := strconv.ParseInt(strVal, 10, 64)
		if err == nil {
			return val, nil
		}
	}

	var numVal int64
	if err := json.Unmarshal(resp.Data, &numVal); err == nil {
		return numVal, nil
	}

	return 0, fmt.Errorf("unknown server time format: %s", string(resp.Data))
}

// GetContractDetails returns specifications for all USDT-FUTURES contracts.
func (c *Client) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	params := map[string]string{
		paramProductType: productTypeUsdtFutures,
	}

	body, err := c.GetCtx(ctx, pathContracts, params)
	if err != nil {
		return nil, err
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
	}

	instruments, err := ParseResponse[[]bitgetInstrument](body, "contract_details")
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

		details = append(details, exchange.ContractDetail{
			Symbol:           inst.Symbol,
			DisplayName:      inst.Symbol,
			DisplayNameEn:    inst.Symbol,
			PositionOpenType: 1, // Isolated/Cross
			BaseCoin:         inst.BaseCoin,
			QuoteCoin:        inst.QuoteCoin,
			SettleCoin:       inst.SettleCoin,
			ContractSize:     1.0, // Defaults to 1 for generic USDT margin linear futures
			MinLeverage:      1,
			MaxLeverage:      100, // Safe default since max leverage tier query is distinct
			PriceScale:       priceScale,
			VolScale:         volScale,
			PriceUnit:        priceUnit,
			MinVol:           int(minVol),
			State:            stateVal,
		})
	}

	return details, nil
}

// GetTickers returns ticker data for all SWAP contracts or a specific instrument.
func (c *Client) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	params := map[string]string{
		paramProductType: productTypeUsdtFutures,
	}
	if symbol != "" {
		params[paramSymbol] = symbol
	}

	body, err := c.GetCtx(ctx, pathTickers, params)
	if err != nil {
		return nil, err
	}

	type bitgetTicker struct {
		Symbol      string `json:"symbol"`
		LastPr      string `json:"lastPr"`
		BidPr       string `json:"bidPr"`
		AskPr       string `json:"askPr"`
		BaseVolume  string `json:"baseVolume"`
		QuoteVolume string `json:"quoteVolume"`
		Ts          string `json:"ts"`
		FundingRate string `json:"fundingRate"`
	}

	tickers, err := ParseResponse[[]bitgetTicker](body, "tickers")
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
		fr, _ := strconv.ParseFloat(t.FundingRate, 64)

		exchangeTickers = append(exchangeTickers, exchange.Ticker{
			Symbol:      t.Symbol,
			LastPrice:   last,
			Bid1:        bid,
			Ask1:        ask,
			Volume24:    vol,
			Amount24:    amt,
			FairPrice:   last,
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
		paramSymbol:      symbol,
		paramProductType: productTypeUsdtFutures,
	}

	body, err := c.GetCtx(ctx, pathFundingRate, params)
	if err != nil {
		return nil, err
	}

	type bitgetFundingRate struct {
		Symbol      string `json:"symbol"`
		FundingRate string `json:"fundingRate"`
		NextUpdate  string `json:"nextUpdate"`
	}

	data, err := ParseResponse[[]bitgetFundingRate](body, "funding_rate")
	if err != nil {
		return nil, err
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("empty funding rate data for %s", symbol)
	}

	frItem := &data[0]
	fr, _ := strconv.ParseFloat(frItem.FundingRate, 64)
	nextSettle, _ := strconv.ParseInt(frItem.NextUpdate, 10, 64)

	return &exchange.FundingRateDetail{
		Symbol:         frItem.Symbol,
		FundingRate:    fr,
		NextSettleTime: nextSettle,
		Timestamp:      nextSettle - 8*3600*1000, // Conceptually cycle is 8 hours
	}, nil
}

// GetKlines returns candlestick data for a symbol.
func (c *Client) GetKlines(ctx context.Context, symbol, interval string, start, end int64) ([]exchange.Kline, error) {
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required for GetKlines")
	}

	gran := "1m"
	if interval == "Min1" || interval == "1m" {
		gran = "1m"
	}

	params := map[string]string{
		paramSymbol:      symbol,
		paramProductType: productTypeUsdtFutures,
		"granularity":    gran,
		paramLimit:       "100",
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

	var resp APIResponse[[][]string]
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse klines response: %w", err)
	}
	if resp.Code != "00000" {
		codeVal := 0
		_, _ = fmt.Sscanf(resp.Code, "%d", &codeVal)
		return nil, toAPIError(codeVal, resp.Msg, "klines")
	}

	klines := make([]exchange.Kline, 0, len(resp.Data))
	for i := len(resp.Data) - 1; i >= 0; i-- { // Bitget returns newest first, reverse to ascending
		row := resp.Data[i]
		if len(row) < 6 {
			continue
		}

		ts, _ := strconv.ParseInt(row[0], 10, 64)
		o, _ := strconv.ParseFloat(row[1], 64)
		h, _ := strconv.ParseFloat(row[2], 64)
		l, _ := strconv.ParseFloat(row[3], 64)
		cVal, _ := strconv.ParseFloat(row[4], 64)
		v, _ := strconv.ParseFloat(row[5], 64)
		a, _ := strconv.ParseFloat(row[6], 64)

		klines = append(klines, exchange.Kline{
			Timestamp: ts,
			Open:      o,
			High:      h,
			Low:       l,
			Close:     cVal,
			Volume:    v,
			Amount:    a,
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
		paramSymbol:      symbol,
		paramProductType: productTypeUsdtFutures,
		paramLimit:       limitStr,
	}

	body, err := c.GetCtx(ctx, pathDepth, params)
	if err != nil {
		return nil, err
	}

	type bitgetDepthLevel []float64
	type bitgetDepth struct {
		Asks []bitgetDepthLevel `json:"asks"`
		Bids []bitgetDepthLevel `json:"bids"`
		Ts   string             `json:"ts"`
	}

	book, err := ParseResponse[bitgetDepth](body, "depth_snapshot")
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
		p := level[0]
		v := level[1]
		ob.Asks = append(ob.Asks, exchange.OrderBookEntry{Price: p, Volume: v})
	}

	for _, level := range book.Bids {
		if len(level) < 2 {
			continue
		}
		p := level[0]
		v := level[1]
		ob.Bids = append(ob.Bids, exchange.OrderBookEntry{Price: p, Volume: v})
	}

	return ob, nil
}

// GetDepthCommits is not supported on Bitget REST.
func (c *Client) GetDepthCommits(ctx context.Context, symbol string, limit int) ([]exchange.DepthCommit, error) {
	return nil, fmt.Errorf("GetDepthCommits not supported on Bitget REST")
}
