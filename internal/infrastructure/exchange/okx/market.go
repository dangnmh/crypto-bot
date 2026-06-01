package okx

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"sync"

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

type okxTicker struct {
	InstID    string `json:"instId"`
	Last      string `json:"last"`
	BidPx     string `json:"bidPx"`
	AskPx     string `json:"askPx"`
	Vol24h    string `json:"vol24h"`
	VolCcy24h string `json:"volCcy24h"`
	Ts        string `json:"ts"`
}

// getOKXVolumes24h fetches tickers from OKX and returns maps of contract volume and USDT volume.
func (c *Client) getOKXVolumes24h(ctx context.Context, symbol string) (vols map[string]float64, amts map[string]float64, rawTickers []okxTicker, err error) {
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

	tickers, err := ParseResponse[okxTicker](body, "tickers")
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

// getOKXFundingRates fetches funding rates concurrently using a worker pool for targeted active symbols.
func (c *Client) getOKXFundingRates(ctx context.Context, symbols []string, amts map[string]float64) ([]exchange.FundingRateResult, error) {
	rates := make([]exchange.FundingRateResult, 0, len(symbols))

	if len(symbols) == 0 {
		return rates, nil
	}

	type frResult struct {
		instID string
		rate   float64
		settle int64
	}

	jobs := make(chan string, len(symbols))
	results := make(chan frResult, len(symbols))
	var wg sync.WaitGroup

	numWorkers := 15
	if len(symbols) < numWorkers {
		numWorkers = len(symbols)
	}

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for instID := range jobs {
				url := fmt.Sprintf("/api/v5/public/funding-rate?instId=%s", instID)
				frBody, err := c.GetCtx(ctx, url, nil)
				if err != nil {
					continue
				}

				type okxFundingRate struct {
					InstID          string `json:"instId"`
					FundingRate     string `json:"fundingRate"`
					NextFundingTime string `json:"nextFundingTime"`
				}
				frList, err := ParseResponse[okxFundingRate](frBody, "funding_rate")
				if err == nil && len(frList) > 0 {
					fr, _ := strconv.ParseFloat(frList[0].FundingRate, 64)
					ns, _ := strconv.ParseInt(frList[0].NextFundingTime, 10, 64)
					results <- frResult{instID: instID, rate: fr, settle: ns}
				}
			}
		}()
	}

	for _, sym := range symbols {
		jobs <- sym
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(results)
	}()

	for res := range results {
		rates = append(rates, exchange.FundingRateResult{
			Symbol:     res.instID,
			Rate:       res.rate,
			SettleTime: res.settle,
			Volume24h:  amts[res.instID],
		})
	}

	return rates, nil
}

func (c *Client) GetFundingRates(ctx context.Context) ([]exchange.FundingRateResult, error) {
	_, amts, rawTickers, err := c.getOKXVolumes24h(ctx, "")
	if err != nil {
		return nil, err
	}

	var activeSymbols []string
	for _, t := range rawTickers {
		if amts[t.InstID] >= 50000 {
			activeSymbols = append(activeSymbols, t.InstID)
		}
	}

	return c.getOKXFundingRates(ctx, activeSymbols, amts)
}

// GetTickers returns ticker data for all SWAP contracts or a specific instrument.
func (c *Client) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	vols, amts, rawTickers, err := c.getOKXVolumes24h(ctx, symbol)
	if err != nil {
		return nil, err
	}

	// Filter active symbols that require funding rate query (volume >= 50k USDT)
	var activeSymbols []string
	if symbol != "" {
		activeSymbols = []string{symbol}
	} else {
		for _, t := range rawTickers {
			if amts[t.InstID] >= 50000 {
				activeSymbols = append(activeSymbols, t.InstID)
			}
		}
	}

	rates, err := c.getOKXFundingRates(ctx, activeSymbols, amts)
	if err != nil {
		return nil, err
	}

	ratesMap := make(map[string]exchange.FundingRateResult)
	for _, r := range rates {
		ratesMap[r.Symbol] = r
	}

	exchangeTickers := make([]exchange.Ticker, 0, len(rawTickers))
	for _, t := range rawTickers {
		last, _ := strconv.ParseFloat(t.Last, 64)
		bid, _ := strconv.ParseFloat(t.BidPx, 64)
		ask, _ := strconv.ParseFloat(t.AskPx, 64)
		ts, _ := strconv.ParseInt(t.Ts, 10, 64)

		exchangeTickers = append(exchangeTickers, exchange.Ticker{
			Symbol:         t.InstID,
			LastPrice:      last,
			Bid1:           bid,
			Ask1:           ask,
			Volume24:       vols[t.InstID],
			Amount24:       amts[t.InstID],
			FundingRate:    ratesMap[t.InstID].Rate,
			NextSettleTime: ratesMap[t.InstID].SettleTime,
			Timestamp:      ts,
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
