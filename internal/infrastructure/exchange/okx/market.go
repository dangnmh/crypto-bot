package okx

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
)

// Explicit request/response structs for market data endpoints.

type okxServerTimeResponse struct {
	Ts string `json:"ts"`
}

type okxInstrumentsRequest struct {
	InstType string `json:"instType"`
}

type okxInstrument struct {
	InstID    string `json:"instId"`
	BaseCcy   string `json:"baseCcy"`
	SettleCcy string `json:"settleCcy"`
	CtVal     string `json:"ctVal"`
	Lever     string `json:"lever"`
	TickSz    string `json:"tickSz"`
	LotSz     string `json:"lotSz"`
	MinSz     string `json:"minSz"`
	State     string `json:"state"`
}

type okxTickersRequest struct {
	InstType string `json:"instType"`
	InstID   string `json:"instId,omitempty"`
}

type okxTicker struct {
	InstID    string `json:"instId"`
	Last      string `json:"last"`
	BidPx     string `json:"bidPx"`
	AskPx     string `json:"askPx"`
	Vol24h    string `json:"vol24h"`
	VolCcy24h string `json:"volCcy24h"`
	Ts        string `json:"ts"`
}

type okxFundingRateRequest struct {
	InstID string `json:"instId"`
}

type okxFundingRate struct {
	InstID          string `json:"instId"`
	FundingRate     string `json:"fundingRate"`
	NextFundingTime string `json:"nextFundingTime"`
}

type okxKlinesRequest struct {
	InstID string `json:"instId"`
	Bar    string `json:"bar"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
	Limit  string `json:"limit,omitempty"`
}

type okxBookLevel []string

type okxDepthRequest struct {
	InstID string `json:"instId"`
	Sz     string `json:"sz,omitempty"`
}

type okxDepthResponse struct {
	Asks []okxBookLevel `json:"asks"`
	Bids []okxBookLevel `json:"bids"`
	Ts   string         `json:"ts"`
}

// Private raw methods invoking the OKX V5 REST API.

func (c *Client) getRawServerTime(ctx context.Context) (*okxServerTimeResponse, error) {
	body, err := c.RawRequest(ctx, http.MethodGet, pathServerTime, nil, nil)
	if err != nil {
		return nil, err
	}
	res, err := ParseResponseFirst[okxServerTimeResponse](body, "server_time")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *Client) getRawContractDetails(ctx context.Context, req okxInstrumentsRequest) ([]okxInstrument, error) {
	params := map[string]string{
		paramInstType: req.InstType,
	}
	body, err := c.RawRequest(ctx, http.MethodGet, pathInstruments, params, nil)
	if err != nil {
		return nil, err
	}
	return ParseResponse[okxInstrument](body, "contract_details")
}

func (c *Client) getRawTickers(ctx context.Context, req okxTickersRequest) ([]okxTicker, error) {
	params := map[string]string{
		paramInstType: req.InstType,
	}
	if req.InstID != "" {
		params[paramInstId] = req.InstID
	}
	body, err := c.GetTickersRaw(ctx, params)
	if err != nil {
		return nil, err
	}
	return ParseResponse[okxTicker](body, "tickers")
}

func (c *Client) getRawFundingRate(ctx context.Context, req okxFundingRateRequest) (*okxFundingRate, error) {
	params := map[string]string{
		paramInstId: req.InstID,
	}
	body, err := c.GetFundingRateRaw(ctx, params)
	if err != nil {
		return nil, err
	}
	frList, err := ParseResponse[okxFundingRate](body, "funding_rate")
	if err != nil {
		return nil, err
	}
	if len(frList) == 0 {
		return nil, fmt.Errorf("okx funding rate not found for symbol: %s", req.InstID)
	}
	return &frList[0], nil
}

func (c *Client) getRawKlines(ctx context.Context, req okxKlinesRequest) ([][]string, error) {
	params := map[string]string{
		paramInstId: req.InstID,
		"bar":       req.Bar,
	}
	if req.Before != "" {
		params["before"] = req.Before
	}
	if req.After != "" {
		params["after"] = req.After
	}
	if req.Limit != "" {
		params[paramLimit] = req.Limit
	}

	body, err := c.RawRequest(ctx, http.MethodGet, pathKlines, params, nil)
	if err != nil {
		return nil, err
	}

	var resp APIResponse[[]string]
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse klines response: %w", err)
	}
	if resp.Code != "0" {
		codeVal := 0
		_, _ = fmt.Sscanf(resp.Code, "%d", &codeVal)
		return nil, toAPIError(codeVal, resp.Msg, "klines")
	}
	return resp.Data, nil
}

func (c *Client) getRawDepthSnapshot(ctx context.Context, req okxDepthRequest) (*okxDepthResponse, error) {
	params := map[string]string{
		paramInstId: req.InstID,
	}
	if req.Sz != "" {
		params["sz"] = req.Sz
	}
	body, err := c.RawRequest(ctx, http.MethodGet, pathBooks, params, nil)
	if err != nil {
		return nil, err
	}
	res, err := ParseResponseFirst[okxDepthResponse](body, "depth_snapshot")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

// Public mapper methods implementing the exchange.MarketDataProvider interface.

// GetServerTime returns the OKX server timestamp in milliseconds.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	res, err := c.getRawServerTime(ctx)
	if err != nil {
		return 0, err
	}
	val, err := strconv.ParseInt(res.Ts, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse server time: %w", err)
	}
	return val, nil
}

// GetContractDetails returns specifications for all swap/futures contracts.
func (c *Client) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	instruments, err := c.getRawContractDetails(ctx, okxInstrumentsRequest{InstType: instTypeSwap})
	if err != nil {
		return nil, err
	}

	details := make([]exchange.ContractDetail, 0, len(instruments))
	for i := range instruments {
		inst := &instruments[i]
		ctVal, _ := strconv.ParseFloat(inst.CtVal, 64)
		lever, _ := strconv.Atoi(inst.Lever)
		priceUnit, _ := strconv.ParseFloat(inst.TickSz, 64)

		stateVal := 0
		if inst.State == "live" {
			stateVal = 1
		}

		priceScale := decmath.DecimalPlaces(inst.TickSz)
		volScale := decmath.DecimalPlaces(inst.LotSz)

		details = append(details, exchange.ContractDetail{
			Symbol:           inst.InstID,
			DisplayName:      inst.InstID,
			DisplayNameEn:    inst.InstID,
			PositionOpenType: 1, // Isolated by default or cross
			BaseCoin:         inst.BaseCcy,
			QuoteCoin:        inst.SettleCcy,
			SettleCoin:       inst.SettleCcy,
			ContractSize:     ctVal,
			MinLeverage:      1,
			MaxLeverage:      lever,
			PriceScale:       priceScale,
			VolScale:         volScale,
			PriceUnit:        priceUnit,
			MinVol:           1, // default
			State:            stateVal,
		})
	}
	return details, nil
}

// GetFundingRates returns the funding rate for specific contracts.
func (c *Client) GetFundingRates(ctx context.Context, symbols []string) ([]exchange.FundingRateResult, error) {
	if len(symbols) == 0 {
		return nil, nil
	}

	rates := make([]exchange.FundingRateResult, 0, len(symbols))
	for _, sym := range symbols {
		raw, err := c.getRawFundingRate(ctx, okxFundingRateRequest{InstID: sym})
		if err != nil {
			return nil, err
		}
		fr, _ := strconv.ParseFloat(raw.FundingRate, 64)
		ns, _ := strconv.ParseInt(raw.NextFundingTime, 10, 64)
		rates = append(rates, exchange.FundingRateResult{
			Symbol:     raw.InstID,
			Rate:       fr,
			SettleTime: ns,
		})
	}
	return rates, nil
}

// GetTickers returns ticker data for all SWAP contracts or a specific instrument.
func (c *Client) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	rawTickers, err := c.getRawTickers(ctx, okxTickersRequest{
		InstType: instTypeSwap,
		InstID:   symbol,
	})
	if err != nil {
		return nil, err
	}

	exchangeTickers := make([]exchange.Ticker, 0, len(rawTickers))
	for i := range rawTickers {
		t := &rawTickers[i]
		last, _ := strconv.ParseFloat(t.Last, 64)
		bid, _ := strconv.ParseFloat(t.BidPx, 64)
		ask, _ := strconv.ParseFloat(t.AskPx, 64)
		ts, _ := strconv.ParseInt(t.Ts, 10, 64)
		vol, _ := strconv.ParseFloat(t.Vol24h, 64)
		amt, _ := strconv.ParseFloat(t.VolCcy24h, 64)

		exchangeTickers = append(exchangeTickers, exchange.Ticker{
			Symbol:    t.InstID,
			LastPrice: last,
			Bid1:      bid,
			Ask1:      ask,
			Volume24:  vol,
			Amount24:  amt * last, // Standardized as USDT volume
			Timestamp: ts,
		})
	}
	return exchangeTickers, nil
}

// GetKlines returns candlestick data for a symbol.
func (c *Client) GetKlines(ctx context.Context, symbol, interval string, start, end int64) ([]exchange.Kline, error) {
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required for GetKlines")
	}

	bar := "1m"
	if interval == "Min1" || interval == "1m" {
		bar = "1m"
	}

	req := okxKlinesRequest{
		InstID: symbol,
		Bar:    bar,
		Limit:  "100",
	}
	if start > 0 {
		req.Before = fmt.Sprintf("%d", start)
	}
	if end > 0 {
		req.After = fmt.Sprintf("%d", end)
	}

	rawData, err := c.getRawKlines(ctx, req)
	if err != nil {
		return nil, err
	}

	klines := make([]exchange.Kline, 0, len(rawData))
	for _, row := range slices.Backward(rawData) { // OKX returns newest first, so we reverse it
		if len(row) < 6 {
			continue
		}

		ts, _ := strconv.ParseInt(row[0], 10, 64)
		o, _ := strconv.ParseFloat(row[1], 64)
		h, _ := strconv.ParseFloat(row[2], 64)
		l, _ := strconv.ParseFloat(row[3], 64)
		c, _ := strconv.ParseFloat(row[4], 64)
		v, _ := strconv.ParseFloat(row[5], 64)
		a, _ := strconv.ParseFloat(row[6], 64)

		klines = append(klines, exchange.Kline{
			Timestamp: ts,
			Open:      o,
			High:      h,
			Low:       l,
			Close:     c,
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

	sz := "400"
	if limit > 0 && limit <= 5 {
		sz = "5"
	} else if limit > 0 && limit <= 20 {
		sz = "20"
	}

	res, err := c.getRawDepthSnapshot(ctx, okxDepthRequest{
		InstID: symbol,
		Sz:     sz,
	})
	if err != nil {
		return nil, err
	}

	ob := &exchange.OrderBook{
		Symbol: symbol,
		Asks:   make([]exchange.OrderBookEntry, 0, len(res.Asks)),
		Bids:   make([]exchange.OrderBookEntry, 0, len(res.Bids)),
	}

	for _, level := range res.Asks {
		if len(level) < 2 {
			continue
		}
		p, _ := strconv.ParseFloat(level[0], 64)
		v, _ := strconv.ParseFloat(level[1], 64)
		ob.Asks = append(ob.Asks, exchange.OrderBookEntry{Price: p, Volume: v})
	}

	for _, level := range res.Bids {
		if len(level) < 2 {
			continue
		}
		p, _ := strconv.ParseFloat(level[0], 64)
		v, _ := strconv.ParseFloat(level[1], 64)
		ob.Bids = append(ob.Bids, exchange.OrderBookEntry{Price: p, Volume: v})
	}

	return ob, nil
}

// GetDepthCommits is not supported on OKX.
func (c *Client) GetDepthCommits(ctx context.Context, symbol string, limit int) ([]exchange.DepthCommit, error) {
	return nil, fmt.Errorf("GetDepthCommits not supported on OKX")
}
