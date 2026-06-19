package bitget

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strconv"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
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

type bitgetKlinesRequest struct {
	Symbol      string `json:"symbol"`
	ProductType string `json:"productType"`
	Granularity string `json:"granularity"`
	Limit       string `json:"limit,omitempty"`
	StartTime   string `json:"startTime,omitempty"`
	EndTime     string `json:"endTime,omitempty"`
}

type bitgetDepthRequest struct {
	Symbol      string `json:"symbol"`
	ProductType string `json:"productType"`
	Limit       string `json:"limit,omitempty"`
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

type bitgetDepthLevel []float64

type bitgetDepth struct {
	Asks []bitgetDepthLevel `json:"asks"`
	Bids []bitgetDepthLevel `json:"bids"`
	Ts   string             `json:"ts"`
}

// Private raw methods invoking the Bitget REST API.

func (c *Client) getRawServerTime(ctx context.Context, _ bitgetServerTimeRequest) (json.RawMessage, error) {
	body, err := c.GetCtx(ctx, pathServerTime, nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Code string          `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse server time response: %w", err)
	}
	if resp.Code != "00000" {
		return nil, toAPIError(0, resp.Msg, "server_time")
	}
	return resp.Data, nil
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

func (c *Client) getRawKlines(ctx context.Context, req bitgetKlinesRequest) ([][]string, error) {
	params := map[string]string{
		paramSymbol:      req.Symbol,
		paramProductType: req.ProductType,
		"granularity":    req.Granularity,
		paramLimit:       req.Limit,
	}

	if req.StartTime != "" {
		params["startTime"] = req.StartTime
	}
	if req.EndTime != "" {
		params["endTime"] = req.EndTime
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
	return resp.Data, nil
}

func (c *Client) getRawDepthSnapshot(ctx context.Context, req bitgetDepthRequest) (*bitgetDepth, error) {
	params := map[string]string{
		paramSymbol:      req.Symbol,
		paramProductType: req.ProductType,
		paramLimit:       req.Limit,
	}

	body, err := c.GetCtx(ctx, pathDepth, params)
	if err != nil {
		return nil, err
	}

	book, err := ParseResponse[bitgetDepth](body, "depth_snapshot")
	if err != nil {
		return nil, err
	}
	return &book, nil
}

// Public mapper methods implementing the exchange.MarketDataProvider interface.

// GetServerTime returns the Bitget server timestamp in milliseconds.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	data, err := c.getRawServerTime(ctx, bitgetServerTimeRequest{})
	if err != nil {
		return 0, err
	}

	var strVal string
	if err := json.Unmarshal(data, &strVal); err == nil {
		val, err := strconv.ParseInt(strVal, 10, 64)
		if err == nil {
			return val, nil
		}
	}

	var numVal int64
	if err := json.Unmarshal(data, &numVal); err == nil {
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

		details = append(details, exchange.ContractDetail{
			Symbol:           inst.Symbol,
			DisplayName:      inst.Symbol,
			DisplayNameEn:    inst.Symbol,
			PositionOpenType: 1, // Isolated/Cross.
			BaseCoin:         inst.BaseCoin,
			QuoteCoin:        inst.QuoteCoin,
			SettleCoin:       inst.SettleCoin,
			ContractSize:     1.0, // Defaults to 1 for generic USDT margin linear futures.
			MinLeverage:      1,
			MaxLeverage:      100, // Safe default since max leverage tier query is distinct.
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

// GetKlines returns candlestick data for a symbol.
func (c *Client) GetKlines(ctx context.Context, symbol, interval string, start, end int64) ([]exchange.Kline, error) {
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required for GetKlines")
	}

	gran := "1m"
	if interval == "Min1" || interval == "1m" {
		gran = "1m"
	}

	req := bitgetKlinesRequest{
		Symbol:      symbol,
		ProductType: productTypeUsdtFutures,
		Granularity: gran,
		Limit:       "100",
	}

	if start > 0 {
		req.StartTime = fmt.Sprintf("%d", start)
	}
	if end > 0 {
		req.EndTime = fmt.Sprintf("%d", end)
	}

	rawData, err := c.getRawKlines(ctx, req)
	if err != nil {
		return nil, err
	}

	klines := make([]exchange.Kline, 0, len(rawData))
	for _, row := range slices.Backward(rawData) { // Bitget returns newest first, reverse to ascending.
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

	book, err := c.getRawDepthSnapshot(ctx, bitgetDepthRequest{
		Symbol:      symbol,
		ProductType: productTypeUsdtFutures,
		Limit:       limitStr,
	})
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
