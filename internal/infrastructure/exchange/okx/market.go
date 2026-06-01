package okx

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
)

// GetServerTime returns the OKX server timestamp in milliseconds.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	body, err := c.GetCtx(ctx, pathServerTime, nil)
	if err != nil {
		return 0, err
	}

	type serverTime struct {
		Epoch string `json:"epoch"`
	}

	data, err := ParseResponseFirst[serverTime](body, "server_time")
	if err != nil {
		return 0, err
	}

	val, err := strconv.ParseInt(data.Epoch, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse server time: %w", err)
	}

	return val, nil
}

// GetContractDetails returns specifications for all swap/futures contracts.
func (c *Client) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	params := map[string]string{
		paramInstType: instTypeSwap,
	}

	body, err := c.GetCtx(ctx, pathInstruments, params)
	if err != nil {
		return nil, err
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

	instruments, err := ParseResponse[okxInstrument](body, "contract_details")
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

type rawTicker struct {
	InstID    string `json:"instId"`
	Last      string `json:"last"`
	BidPx     string `json:"bidPx"`
	AskPx     string `json:"askPx"`
	Vol24h    string `json:"vol24h"`
	VolCcy24h string `json:"volCcy24h"`
	Ts        string `json:"ts"`
}

func (c *Client) getRawVolumes24h(ctx context.Context, symbol string) (vols, amts map[string]float64, rawTickers []rawTicker, err error) {
	params := map[string]string{
		paramInstType: instTypeSwap,
	}
	if symbol != "" {
		params[paramInstId] = symbol
	}

	body, err := c.GetCtx(ctx, pathTickers, params)
	if err != nil {
		return nil, nil, nil, err
	}

	tickers, err := ParseResponse[rawTicker](body, "tickers")
	if err != nil {
		return nil, nil, nil, err
	}

	vols = make(map[string]float64)
	amts = make(map[string]float64)
	for i := range tickers {
		t := &tickers[i]
		last, _ := strconv.ParseFloat(t.Last, 64)
		vol, _ := strconv.ParseFloat(t.Vol24h, 64)
		amt, _ := strconv.ParseFloat(t.VolCcy24h, 64)

		vols[t.InstID] = vol
		amts[t.InstID] = amt * last // Standardized as USDT volume
	}

	return vols, amts, tickers, nil
}

type rawFunding struct {
	InstID          string `json:"instId"`
	FundingRate     string `json:"fundingRate"`
	NextFundingTime string `json:"nextFundingTime"`
}

func (c *Client) getRawFundingRate(ctx context.Context, symbol string) (*rawFunding, error) {
	url := fmt.Sprintf("/api/v5/public/funding-rate?instId=%s", symbol)
	frBody, err := c.GetCtx(ctx, url, nil)
	if err != nil {
		return nil, err
	}
	frList, err := ParseResponse[rawFunding](frBody, "funding_rate")
	if err != nil {
		return nil, err
	}
	if len(frList) == 0 {
		return nil, fmt.Errorf("okx funding rate not found for symbol: %s", symbol)
	}
	return &frList[0], nil
}

func (c *Client) GetFundingRates(ctx context.Context, symbols []string) ([]exchange.FundingRateResult, error) {
	if len(symbols) == 0 {
		return nil, nil
	}

	rates := make([]exchange.FundingRateResult, 0, len(symbols))
	for _, sym := range symbols {
		raw, err := c.getRawFundingRate(ctx, sym)
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
	vols, amts, rawTickers, err := c.getRawVolumes24h(ctx, symbol)
	if err != nil {
		return nil, err
	}

	exchangeTickers := make([]exchange.Ticker, 0, len(rawTickers))
	for _, t := range rawTickers {
		last, _ := strconv.ParseFloat(t.Last, 64)
		bid, _ := strconv.ParseFloat(t.BidPx, 64)
		ask, _ := strconv.ParseFloat(t.AskPx, 64)
		ts, _ := strconv.ParseInt(t.Ts, 10, 64)

		exchangeTickers = append(exchangeTickers, exchange.Ticker{
			Symbol:    t.InstID,
			LastPrice: last,
			Bid1:      bid,
			Ask1:      ask,
			Volume24:  vols[t.InstID],
			Amount24:  amts[t.InstID],
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

	// Map intervals
	bar := "1m"
	if interval == "Min1" || interval == "1m" {
		bar = "1m"
	}

	params := map[string]string{
		paramInstId: symbol,
		"bar":       bar,
		paramLimit:  "100",
	}

	if start > 0 {
		params["before"] = fmt.Sprintf("%d", start) // OKX candle uses before/after
	}
	if end > 0 {
		params["after"] = fmt.Sprintf("%d", end)
	}

	body, err := c.GetCtx(ctx, pathKlines, params)
	if err != nil {
		return nil, err
	}

	// OKX returns array of arrays, e.g., [ [ts, o, h, l, c, vol, volCcy, volCcyQuote, confirm], ... ]
	var resp APIResponse[[]string]
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse klines response: %w", err)
	}
	if resp.Code != "0" {
		codeVal := 0
		_, _ = fmt.Sscanf(resp.Code, "%d", &codeVal)
		return nil, toAPIError(codeVal, resp.Msg, "klines")
	}

	klines := make([]exchange.Kline, 0, len(resp.Data))
	for _, row := range slices.Backward(resp.Data) { // OKX returns newest first, so we reverse it
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

	params := map[string]string{
		paramInstId: symbol,
		"sz":        sz,
	}

	body, err := c.GetCtx(ctx, pathBooks, params)
	if err != nil {
		return nil, err
	}

	type okxBookLevel []string
	type okxBook struct {
		Asks []okxBookLevel `json:"asks"`
		Bids []okxBookLevel `json:"bids"`
		Ts   string         `json:"ts"`
	}

	book, err := ParseResponseFirst[okxBook](body, "depth_snapshot")
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
		p, _ := strconv.ParseFloat(level[0], 64)
		v, _ := strconv.ParseFloat(level[1], 64)
		ob.Asks = append(ob.Asks, exchange.OrderBookEntry{Price: p, Volume: v})
	}

	for _, level := range book.Bids {
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
