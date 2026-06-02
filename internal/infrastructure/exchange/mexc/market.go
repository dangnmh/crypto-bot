package mexc

import (
	"context"
	"encoding/json"
	"fmt"

	"crypto-bot/internal/infrastructure/exchange"
)

// Explicit request/response structs for market data endpoints.

type mexcContractDetail struct {
	Symbol                    string   `json:"symbol"`
	DisplayName               string   `json:"displayName"`
	DisplayNameEn             string   `json:"displayNameEn"`
	PositionOpenType          int      `json:"positionOpenType"`
	BaseCoin                  string   `json:"baseCoin"`
	QuoteCoin                 string   `json:"quoteCoin"`
	SettleCoin                string   `json:"settleCoin"`
	ContractSize              float64  `json:"contractSize"`
	MinLeverage               int      `json:"minLeverage"`
	MaxLeverage               int      `json:"maxLeverage"`
	PriceScale                int      `json:"priceScale"`
	VolScale                  int      `json:"volScale"`
	AmountScale               int      `json:"amountScale"`
	PriceUnit                 float64  `json:"priceUnit"`
	VolUnit                   int      `json:"volUnit"`
	MinVol                    int      `json:"minVol"`
	MaxVol                    int      `json:"maxVol"`
	BidLimitPriceRate         float64  `json:"bidLimitPriceRate"`
	AskLimitPriceRate         float64  `json:"askLimitPriceRate"`
	TakerFeeRate              float64  `json:"takerFeeRate"`
	MakerFeeRate              float64  `json:"makerFeeRate"`
	MaintenanceMarginRate     float64  `json:"maintenanceMarginRate"`
	InitialMarginRate         float64  `json:"initialMarginRate"`
	RiskBaseVol               int      `json:"riskBaseVol"`
	RiskIncrVol               int      `json:"riskIncrVol"`
	RiskIncrMmr               float64  `json:"riskIncrMmr"`
	RiskIncrImr               float64  `json:"riskIncrImr"`
	RiskLevelLimit            int      `json:"riskLevelLimit"`
	PriceCoefficientVariation float64  `json:"priceCoefficientVariation"`
	IndexOrigin               []string `json:"indexOrigin"`
	State                     int      `json:"state"`
	IsNew                     bool     `json:"isNew"`
	IsHot                     bool     `json:"isHot"`
	IsHidden                  bool     `json:"isHidden"`
}

type mexcTickersRequest struct {
	Symbol string `json:"symbol,omitempty"`
}

type mexcTicker struct {
	Symbol    string  `json:"symbol"`
	LastPrice float64 `json:"lastPrice"`
	Bid1      float64 `json:"bid1"`
	Ask1      float64 `json:"ask1"`
	Volume24  float64 `json:"volume24"`
	Amount24  float64 `json:"amount24"`
	Timestamp int64   `json:"timestamp"`
}

type mexcFundingRateRequest struct {
	Symbol string `json:"symbol,omitempty"`
}

type mexcFundingRate struct {
	Symbol         string  `json:"symbol"`
	FundingRate    float64 `json:"fundingRate"`
	NextSettleTime int64   `json:"nextSettleTime"`
}

type mexcFundingRateHistoryRequest struct {
	Symbol   string `json:"symbol"`
	PageNum  string `json:"page_num"`
	PageSize string `json:"page_size"`
}

type mexcFundingRateHistory struct {
	Symbol      string  `json:"symbol"`
	FundingRate float64 `json:"fundingRate"`
	SettleTime  int64   `json:"settleTime"`
}

type mexcKlinesRequest struct {
	Symbol   string `json:"symbol"`
	Interval string `json:"interval"`
	Start    string `json:"start,omitempty"`
	End      string `json:"end,omitempty"`
}

type mexcKlineData struct {
	Time   []int64   `json:"time"`
	Open   []float64 `json:"open"`
	Close  []float64 `json:"close"`
	High   []float64 `json:"high"`
	Low    []float64 `json:"low"`
	Vol    []float64 `json:"vol"`
	Amount []float64 `json:"amount"`
}

type mexcDepthRequest struct {
	Symbol string `json:"symbol"`
	Limit  string `json:"limit,omitempty"`
}

type mexcDepthData struct {
	Asks    [][]json.Number `json:"asks"`
	Bids    [][]json.Number `json:"bids"`
	Version int64           `json:"version"`
}

type mexcDepthCommitsRequest struct {
	Symbol string `json:"symbol"`
	Limit  int    `json:"limit"`
}

type mexcRawCommit struct {
	Version int64           `json:"version"`
	Asks    [][]json.Number `json:"asks"`
	Bids    [][]json.Number `json:"bids"`
}

// Private raw methods invoking the MEXC API.

func (c *Client) rawPing(ctx context.Context) ([]byte, error) {
	return c.GetCtx(ctx, "/api/v1/contract/ping", nil)
}

func (c *Client) getRawServerTime(ctx context.Context) (int64, error) {
	body, err := c.rawPing(ctx)
	if err != nil {
		return 0, err
	}
	return ParseResponse[int64](body, "server_time")
}

func (c *Client) getRawContractDetails(ctx context.Context) ([]mexcContractDetail, error) {
	body, err := c.GetCtx(ctx, "/api/v1/contract/detail", nil)
	if err != nil {
		return nil, err
	}
	return ParseResponse[[]mexcContractDetail](body, "contract_details")
}

func (c *Client) getRawTickers(ctx context.Context, req mexcTickersRequest) ([]mexcTicker, error) {
	params := map[string]any{}
	if req.Symbol != "" {
		params[paramSymbol] = req.Symbol
	}

	body, err := c.GetCtx(ctx, "/api/v1/contract/ticker", params)
	if err != nil {
		return nil, err
	}

	raw, err := ParseResponse[json.RawMessage](body, "ticker")
	if err != nil {
		return nil, err
	}

	var list []mexcTicker
	if err := json.Unmarshal(raw, &list); err == nil {
		return list, nil
	}

	var single mexcTicker
	if err := json.Unmarshal(raw, &single); err != nil {
		return nil, fmt.Errorf("parse ticker data: %w", err)
	}
	return []mexcTicker{single}, nil
}

func (c *Client) getRawFundingRate(ctx context.Context, req mexcFundingRateRequest) (*mexcFundingRate, error) {
	params := map[string]any{}
	if req.Symbol != "" {
		params[paramSymbol] = req.Symbol
	}
	body, err := c.GetCtx(ctx, "/api/v1/contract/funding_rate", params)
	if err != nil {
		return nil, err
	}
	rawRates, err := ParseResponse[[]mexcFundingRate](body, "funding_rates")
	if err != nil {
		return nil, err
	}
	for i := range rawRates {
		if rawRates[i].Symbol == req.Symbol {
			return &rawRates[i], nil
		}
	}
	return nil, fmt.Errorf("mexc funding rate not found for symbol: %s", req.Symbol)
}

func (c *Client) getRawFundingRateHistory(ctx context.Context, req mexcFundingRateHistoryRequest) ([]mexcFundingRateHistory, error) {
	params := map[string]any{
		paramSymbol: req.Symbol,
		pageNumKey:  req.PageNum,
		pageSizeKey: req.PageSize,
	}
	body, err := c.GetCtx(ctx, "/api/v1/contract/funding_rate/history", params)
	if err != nil {
		return nil, err
	}
	type resultWrapper struct {
		ResultList []mexcFundingRateHistory `json:"resultList"`
	}
	data, err := ParseResponse[resultWrapper](body, "funding_rate_history")
	if err != nil {
		return nil, err
	}
	return data.ResultList, nil
}

func (c *Client) getRawKlines(ctx context.Context, req mexcKlinesRequest) (*mexcKlineData, error) {
	params := map[string]any{
		paramInterval: req.Interval,
	}
	if req.Start != "" {
		params["start"] = req.Start
	}
	if req.End != "" {
		params["end"] = req.End
	}
	body, err := c.GetCtx(ctx, "/api/v1/contract/kline/"+req.Symbol, params)
	if err != nil {
		return nil, err
	}
	res, err := ParseResponse[mexcKlineData](body, "klines")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *Client) getRawDepthSnapshot(ctx context.Context, req mexcDepthRequest) (*mexcDepthData, error) {
	params := map[string]any{}
	if req.Limit != "" {
		params["limit"] = req.Limit
	}
	body, err := c.GetCtx(ctx, "/api/v1/contract/depth/"+req.Symbol, params)
	if err != nil {
		return nil, err
	}
	res, err := ParseResponse[mexcDepthData](body, "depth_snapshot")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *Client) getRawDepthCommits(ctx context.Context, req mexcDepthCommitsRequest) ([]mexcRawCommit, error) {
	path := fmt.Sprintf("/api/v1/contract/depth_commits/%s/%d", req.Symbol, req.Limit)
	body, err := c.GetCtx(ctx, path, nil)
	if err != nil {
		return nil, err
	}
	return ParseResponse[[]mexcRawCommit](body, "depth_commits")
}

// Public mapper methods implementing the exchange.MarketDataProvider interface.

// Ping checks connectivity to the MEXC API server.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.rawPing(ctx)
	return err
}

// GetServerTime returns the MEXC server timestamp in milliseconds.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	return c.getRawServerTime(ctx)
}

// GetContractDetails returns all contract specifications.
func (c *Client) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	rawList, err := c.getRawContractDetails(ctx)
	if err != nil {
		return nil, err
	}

	details := make([]exchange.ContractDetail, 0, len(rawList))
	for i := range rawList {
		raw := &rawList[i]
		details = append(details, exchange.ContractDetail{
			Symbol:                    raw.Symbol,
			DisplayName:               raw.DisplayName,
			DisplayNameEn:             raw.DisplayNameEn,
			PositionOpenType:          raw.PositionOpenType,
			BaseCoin:                  raw.BaseCoin,
			QuoteCoin:                 raw.QuoteCoin,
			SettleCoin:                raw.SettleCoin,
			ContractSize:              raw.ContractSize,
			MinLeverage:               raw.MinLeverage,
			MaxLeverage:               raw.MaxLeverage,
			PriceScale:                raw.PriceScale,
			VolScale:                  raw.VolScale,
			AmountScale:               raw.AmountScale,
			PriceUnit:                 raw.PriceUnit,
			VolUnit:                   raw.VolUnit,
			MinVol:                    raw.MinVol,
			MaxVol:                    raw.MaxVol,
			BidLimitPriceRate:         raw.BidLimitPriceRate,
			AskLimitPriceRate:         raw.AskLimitPriceRate,
			TakerFeeRate:              raw.TakerFeeRate,
			MakerFeeRate:              raw.MakerFeeRate,
			MaintenanceMarginRate:     raw.MaintenanceMarginRate,
			InitialMarginRate:         raw.InitialMarginRate,
			RiskBaseVol:               raw.RiskBaseVol,
			RiskIncrVol:               raw.RiskIncrVol,
			RiskIncrMmr:               raw.RiskIncrMmr,
			RiskIncrImr:               raw.RiskIncrImr,
			RiskLevelLimit:            raw.RiskLevelLimit,
			PriceCoefficientVariation: raw.PriceCoefficientVariation,
			IndexOrigin:               raw.IndexOrigin,
			State:                     raw.State,
			IsNew:                     raw.IsNew,
			IsHot:                     raw.IsHot,
			IsHidden:                  raw.IsHidden,
		})
	}
	return details, nil
}

// GetFundingRates returns funding rates for specific symbols.
func (c *Client) GetFundingRates(ctx context.Context, symbols []string) ([]exchange.FundingRateResult, error) {
	if len(symbols) == 0 {
		return nil, nil
	}

	rates := make([]exchange.FundingRateResult, 0, len(symbols))
	for _, sym := range symbols {
		raw, err := c.getRawFundingRate(ctx, mexcFundingRateRequest{Symbol: sym})
		if err != nil {
			return nil, err
		}
		rates = append(rates, exchange.FundingRateResult{
			Symbol:     raw.Symbol,
			Rate:       raw.FundingRate,
			SettleTime: raw.NextSettleTime,
		})
	}
	return rates, nil
}

// GetTickers returns ticker data for all symbols, or a specific symbol.
func (c *Client) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	rawList, err := c.getRawTickers(ctx, mexcTickersRequest{Symbol: symbol})
	if err != nil {
		return nil, err
	}

	tickers := make([]exchange.Ticker, 0, len(rawList))
	for _, raw := range rawList {
		tickers = append(tickers, exchange.Ticker{
			Symbol:    raw.Symbol,
			LastPrice: raw.LastPrice,
			Bid1:      raw.Bid1,
			Ask1:      raw.Ask1,
			Volume24:  raw.Volume24,
			Amount24:  raw.Amount24,
			Timestamp: raw.Timestamp,
		})
	}
	return tickers, nil
}

// GetFundingRateHistory returns funding rate history for a symbol.
func (c *Client) GetFundingRateHistory(ctx context.Context, symbol string, pageNum, pageSize int) ([]exchange.FundingRateHistory, error) {
	rawList, err := c.getRawFundingRateHistory(ctx, mexcFundingRateHistoryRequest{
		Symbol:   symbol,
		PageNum:  fmt.Sprintf("%d", pageNum),
		PageSize: fmt.Sprintf("%d", pageSize),
	})
	if err != nil {
		return nil, err
	}

	history := make([]exchange.FundingRateHistory, 0, len(rawList))
	for _, raw := range rawList {
		history = append(history, exchange.FundingRateHistory{
			Symbol:      raw.Symbol,
			FundingRate: raw.FundingRate,
			SettleTime:  raw.SettleTime,
		})
	}
	return history, nil
}

// GetKlines returns candlestick data for a symbol.
func (c *Client) GetKlines(ctx context.Context, symbol, interval string, start, end int64) ([]exchange.Kline, error) {
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required for GetKlines")
	}

	req := mexcKlinesRequest{
		Symbol:   symbol,
		Interval: interval,
	}
	if start > 0 {
		req.Start = fmt.Sprintf("%d", start)
	}
	if end > 0 {
		req.End = fmt.Sprintf("%d", end)
	}

	data, err := c.getRawKlines(ctx, req)
	if err != nil {
		return nil, err
	}

	n := len(data.Time)
	klines := make([]exchange.Kline, 0, n)
	for i := range n {
		if i >= len(data.Open) || i >= len(data.Close) || i >= len(data.High) || i >= len(data.Low) || i >= len(data.Vol) || i >= len(data.Amount) {
			break
		}
		klines = append(klines, exchange.Kline{
			Timestamp: data.Time[i] * 1000,
			Open:      data.Open[i],
			Close:     data.Close[i],
			High:      data.High[i],
			Low:       data.Low[i],
			Volume:    data.Vol[i],
			Amount:    data.Amount[i],
		})
	}

	return klines, nil
}

// GetDepthSnapshot returns the full orderbook snapshot for a symbol via REST.
func (c *Client) GetDepthSnapshot(ctx context.Context, symbol string, limit int) (*exchange.OrderBook, error) {
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required for GetDepthSnapshot")
	}

	req := mexcDepthRequest{
		Symbol: symbol,
	}
	if limit > 0 {
		req.Limit = fmt.Sprintf("%d", limit)
	}

	data, err := c.getRawDepthSnapshot(ctx, req)
	if err != nil {
		return nil, err
	}

	ob := &exchange.OrderBook{
		Symbol:  symbol,
		Version: data.Version,
		Asks:    make([]exchange.OrderBookEntry, 0, len(data.Asks)),
		Bids:    make([]exchange.OrderBookEntry, 0, len(data.Bids)),
	}

	for _, level := range data.Asks {
		if len(level) < 2 {
			continue
		}
		p, _ := level[0].Float64()
		v, _ := level[1].Float64()
		if p > 0 {
			ob.Asks = append(ob.Asks, exchange.OrderBookEntry{Price: p, Volume: v})
		}
	}

	for _, level := range data.Bids {
		if len(level) < 2 {
			continue
		}
		p, _ := level[0].Float64()
		v, _ := level[1].Float64()
		if p > 0 {
			ob.Bids = append(ob.Bids, exchange.OrderBookEntry{Price: p, Volume: v})
		}
	}

	return ob, nil
}

// GetDepthCommits returns the latest incremental depth commits for a symbol.
func (c *Client) GetDepthCommits(ctx context.Context, symbol string, limit int) ([]exchange.DepthCommit, error) {
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required for GetDepthCommits")
	}
	if limit <= 0 {
		limit = 1000
	}

	rawCommits, err := c.getRawDepthCommits(ctx, mexcDepthCommitsRequest{Symbol: symbol, Limit: limit})
	if err != nil {
		return nil, err
	}

	commits := make([]exchange.DepthCommit, 0, len(rawCommits))
	for _, rc := range rawCommits {
		dc := exchange.DepthCommit{
			Version: rc.Version,
			Asks:    make([]exchange.OrderBookEntry, 0, len(rc.Asks)),
			Bids:    make([]exchange.OrderBookEntry, 0, len(rc.Bids)),
		}
		for _, level := range rc.Asks {
			if len(level) < 2 {
				continue
			}
			p, _ := level[0].Float64()
			v, _ := level[1].Float64()
			dc.Asks = append(dc.Asks, exchange.OrderBookEntry{Price: p, Volume: v})
		}
		for _, level := range rc.Bids {
			if len(level) < 2 {
				continue
			}
			p, _ := level[0].Float64()
			v, _ := level[1].Float64()
			dc.Bids = append(dc.Bids, exchange.OrderBookEntry{Price: p, Volume: v})
		}
		commits = append(commits, dc)
	}

	return commits, nil
}
