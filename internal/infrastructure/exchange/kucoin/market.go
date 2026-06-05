package kucoin

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

type kucoinContract struct {
	Symbol                  string  `json:"symbol"`
	BaseCurrency            string  `json:"baseCurrency"`
	QuoteCurrency           string  `json:"quoteCurrency"`
	SettleCurrency          string  `json:"settleCurrency"`
	LotSize                 int64   `json:"lotSize"`
	TickSize                float64 `json:"tickSize"`
	Multiplier              float64 `json:"multiplier"`
	Status                  string  `json:"status"`
	TurnoverOf24h           float64 `json:"turnoverOf24h"`
	VolumeOf24h             float64 `json:"volumeOf24h"`
	FundingFeeRate          float64 `json:"fundingFeeRate"`
	NextFundingRateDateTime int64   `json:"nextFundingRateDateTime"`
}

type kucoinServerTimeRequest struct{}

type kucoinContractsRequest struct{}

type kucoinTickersRequest struct{}

type kucoinTickerSingleRequest struct {
	Symbol string `json:"symbol"`
}

type kucoinKlinesRequest struct {
	Symbol      string `json:"symbol"`
	Granularity string `json:"granularity"`
	From        string `json:"from,omitempty"`
	To          string `json:"to,omitempty"`
}

type kucoinDepthRequest struct {
	Symbol string `json:"symbol"`
}

type kucoinSingleTicker struct {
	Symbol       string `json:"symbol"`
	BestBidPrice string `json:"bestBidPrice"`
	BestAskPrice string `json:"bestAskPrice"`
	Price        string `json:"price"`
	Size         string `json:"size"`
	Ts           string `json:"ts"`
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

type kucoinDepth struct {
	Asks [][]float64 `json:"asks"`
	Bids [][]float64 `json:"bids"`
	Ts   int64       `json:"ts"`
}

// Private raw methods invoking the KuCoin REST API.

func (c *Client) getRawServerTime(ctx context.Context, _ kucoinServerTimeRequest) (json.RawMessage, error) {
	body, err := c.GetCtx(ctx, pathServerTime, nil)
	if err != nil {
		return nil, err
	}
	return body, nil
}

func (c *Client) getRawContractDetails(ctx context.Context, _ kucoinContractsRequest) ([]kucoinContract, error) {
	body, err := c.GetCtx(ctx, pathContracts, nil)
	if err != nil {
		return nil, err
	}
	return ParseResponse[[]kucoinContract](body, "contract_details")
}

func (c *Client) getRawTickerSingle(ctx context.Context, req kucoinTickerSingleRequest) (*kucoinSingleTicker, error) {
	params := map[string]string{
		paramSymbol: req.Symbol,
	}
	body, err := c.GetCtx(ctx, pathTickerSingle, params)
	if err != nil {
		return nil, err
	}
	res, err := ParseResponse[kucoinSingleTicker](body, "ticker_single")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *Client) getRawTickers(ctx context.Context, _ kucoinTickersRequest) ([]kucoinTicker, error) {
	body, err := c.GetCtx(ctx, pathTickers, nil)
	if err != nil {
		return nil, err
	}
	return ParseResponse[[]kucoinTicker](body, "tickers")
}

func (c *Client) getRawKlines(ctx context.Context, req kucoinKlinesRequest) ([][]float64, error) {
	params := map[string]string{
		paramSymbol:   req.Symbol,
		"granularity": req.Granularity,
	}
	if req.From != "" {
		params["from"] = req.From
	}
	if req.To != "" {
		params["to"] = req.To
	}
	body, err := c.GetCtx(ctx, pathKlines, params)
	if err != nil {
		return nil, err
	}
	return ParseResponse[[][]float64](body, "klines")
}

func (c *Client) getRawDepthSnapshot(ctx context.Context, req kucoinDepthRequest) (*kucoinDepth, error) {
	params := map[string]string{
		paramSymbol: req.Symbol,
	}
	body, err := c.GetCtx(ctx, pathDepth, params)
	if err != nil {
		return nil, err
	}
	res, err := ParseResponse[kucoinDepth](body, "depth_snapshot")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

// Public mapper methods implementing the exchange.MarketDataProvider interface.

// GetServerTime returns the KuCoin server timestamp in milliseconds.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	body, err := c.getRawServerTime(ctx, kucoinServerTimeRequest{})
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
	instruments, err := c.getRawContractDetails(ctx, kucoinContractsRequest{})
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

		tickSize := inst.TickSize

		priceScale := decmath.DecimalPlaces(strconv.FormatFloat(tickSize, 'f', -1, 64))
		if priceScale <= 0 {
			priceScale = 2
		}

		details = append(details, exchange.ContractDetail{
			Symbol:           inst.Symbol,
			DisplayName:      inst.Symbol,
			DisplayNameEn:    inst.Symbol,
			PositionOpenType: 1, // Isolated/Cross both supported.
			BaseCoin:         inst.BaseCurrency,
			QuoteCoin:        inst.QuoteCurrency,
			SettleCoin:       inst.SettleCurrency,
			ContractSize:     inst.Multiplier,
			MinLeverage:      1,
			MaxLeverage:      100,
			PriceScale:       priceScale,
			VolScale:         0, // Defaults to 0 (unit contracts).
			PriceUnit:        tickSize,
			MinVol:           int(inst.LotSize),
			State:            stateVal,
		})
	}

	return details, nil
}

// GetTickers returns ticker data for all contracts.
func (c *Client) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	if symbol != "" {
		raw, err := c.getRawTickerSingle(ctx, kucoinTickerSingleRequest{
			Symbol: symbol,
		})
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
				Timestamp: ts,
			},
		}, nil
	}

	tickers, err := c.getRawTickers(ctx, kucoinTickersRequest{})
	if err != nil {
		return nil, err
	}

	// Fetch active contracts in bulk to populate volume & turnover 24h without separate API calls.
	cVolMap := make(map[string]float64)
	cAmtMap := make(map[string]float64)
	cList, err := c.getRawContractDetails(ctx, kucoinContractsRequest{})
	if err == nil {
		for i := range cList {
			cVolMap[cList[i].Symbol] = cList[i].VolumeOf24h
			cAmtMap[cList[i].Symbol] = cList[i].TurnoverOf24h
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

		exchangeTickers = append(exchangeTickers, exchange.Ticker{
			Symbol:    t.Symbol,
			LastPrice: last,
			Bid1:      bid,
			Ask1:      ask,
			Volume24:  vol,
			Amount24:  amt,
			Timestamp: ts,
		})
	}

	return exchangeTickers, nil
}

// GetFundingRates returns current funding rate details for the specified symbols.
func (c *Client) GetFundingRates(ctx context.Context, symbols []string) ([]exchange.FundingRateResult, error) {
	if len(symbols) == 0 {
		return nil, nil
	}

	contracts, err := c.getRawContractDetails(ctx, kucoinContractsRequest{})
	if err != nil {
		return nil, err
	}

	contractMap := make(map[string]kucoinContract, len(contracts))
	for i := range contracts {
		contractMap[contracts[i].Symbol] = contracts[i]
	}

	rates := make([]exchange.FundingRateResult, 0, len(symbols))
	for _, sym := range symbols {
		contract, exists := contractMap[sym]
		if !exists {
			c.logger.WarnContext(ctx, "Symbol not found in active contracts", slog.String("symbol", sym))
			continue
		}
		rates = append(rates, exchange.FundingRateResult{
			Symbol:     sym,
			Rate:       contract.FundingFeeRate,
			SettleTime: contract.NextFundingRateDateTime,
		})
	}

	return rates, nil
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

	req := kucoinKlinesRequest{
		Symbol:      symbol,
		Granularity: gran,
	}

	if start > 0 {
		req.From = fmt.Sprintf("%d", start)
	}
	if end > 0 {
		req.To = fmt.Sprintf("%d", end)
	}

	rawRows, err := c.getRawKlines(ctx, req)
	if err != nil {
		return nil, err
	}

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

	book, err := c.getRawDepthSnapshot(ctx, kucoinDepthRequest{
		Symbol: symbol,
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
