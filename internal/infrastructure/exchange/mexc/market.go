package mexc

import (
	"context"
	"encoding/json"
	"fmt"

	"crypto-bot/internal/infrastructure/exchange"
)

// Ping checks connectivity to the MEXC API server.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.GetCtx(ctx, "/api/v1/contract/ping", nil)
	return err
}

// GetServerTime returns the MEXC server timestamp in milliseconds.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	body, err := c.GetCtx(ctx, "/api/v1/contract/ping", nil)
	if err != nil {
		return 0, err
	}
	return ParseResponse[int64](body, "server_time")
}

// GetContractDetails returns all contract specifications.
func (c *Client) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	body, err := c.GetCtx(ctx, "/api/v1/contract/detail", nil)
	if err != nil {
		return nil, err
	}
	return ParseResponse[[]exchange.ContractDetail](body, "contract_details")
}

func (c *Client) GetFundingRates(ctx context.Context) ([]exchange.FundingRateResult, error) {
	tickers, err := c.GetTickers(ctx, "")
	if err != nil {
		return nil, err
	}
	rates := make([]exchange.FundingRateResult, 0, len(tickers))
	for _, t := range tickers {
		rates = append(rates, exchange.FundingRateResult{
			Symbol:     t.Symbol,
			Rate:       t.FundingRate,
			SettleTime: t.NextSettleTime,
			Volume24h:  t.Amount24,
		})
	}
	return rates, nil
}

// GetTickers returns ticker data for all symbols, or a specific symbol.
func (c *Client) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	params := map[string]any{}
	if symbol != "" {
		params[paramSymbol] = symbol
	}

	body, err := c.GetCtx(ctx, "/api/v1/contract/ticker", params)
	if err != nil {
		return nil, err
	}

	// MEXC ticker endpoint returns either an array or a single object depending on params.
	// Use RawMessage to handle both cases.
	raw, err := ParseResponse[json.RawMessage](body, "ticker")
	if err != nil {
		return nil, err
	}

	// Try unmarshaling as an array first.
	var tickers []exchange.Ticker
	if err := json.Unmarshal(raw, &tickers); err == nil {
		return tickers, nil
	}

	// If array fails, try as a single object (MEXC returns this when symbol is specified).
	var single exchange.Ticker
	if err := json.Unmarshal(raw, &single); err != nil {
		return nil, fmt.Errorf("parse ticker data: %w", err)
	}
	return []exchange.Ticker{single}, nil
}


// GetFundingRateHistory returns funding rate history for a symbol.
func (c *Client) GetFundingRateHistory(ctx context.Context, symbol string, pageNum, pageSize int) ([]exchange.FundingRateHistory, error) {
	params := map[string]any{
		paramSymbol: symbol,
		pageNumKey:  fmt.Sprintf("%d", pageNum),
		pageSizeKey: fmt.Sprintf("%d", pageSize),
	}

	body, err := c.GetCtx(ctx, "/api/v1/contract/funding_rate/history", params)
	if err != nil {
		return nil, err
	}

	type resultWrapper struct {
		ResultList []exchange.FundingRateHistory `json:"resultList"`
	}
	data, err := ParseResponse[resultWrapper](body, "funding_rate_history")
	if err != nil {
		return nil, err
	}
	return data.ResultList, nil
}

// GetKlines returns candlestick data for a symbol.
func (c *Client) GetKlines(ctx context.Context, symbol, interval string, start, end int64) ([]exchange.Kline, error) {
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required for GetKlines")
	}

	params := map[string]any{
		paramInterval: interval,
	}
	if start > 0 {
		params["start"] = fmt.Sprintf("%d", start)
	}
	if end > 0 {
		params["end"] = fmt.Sprintf("%d", end)
	}

	body, err := c.GetCtx(ctx, "/api/v1/contract/kline/"+symbol, params)
	if err != nil {
		return nil, err
	}

	type klineData struct {
		Time   []int64   `json:"time"`
		Open   []float64 `json:"open"`
		Close  []float64 `json:"close"`
		High   []float64 `json:"high"`
		Low    []float64 `json:"low"`
		Vol    []float64 `json:"vol"`
		Amount []float64 `json:"amount"`
	}

	data, err := ParseResponse[klineData](body, "klines")
	if err != nil {
		return nil, err
	}

	n := len(data.Time)
	klines := make([]exchange.Kline, 0, n)
	for i := range n {
		// Safety check against misaligned arrays.
		if i >= len(data.Open) || i >= len(data.Close) || i >= len(data.High) || i >= len(data.Low) || i >= len(data.Vol) || i >= len(data.Amount) {
			break
		}
		klines = append(klines, exchange.Kline{
			Timestamp: data.Time[i] * 1000, // API returns seconds; convert to ms.
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
// Endpoint: GET /api/v1/contract/depth/{symbol}?limit={limit}.
func (c *Client) GetDepthSnapshot(ctx context.Context, symbol string, limit int) (*exchange.OrderBook, error) {
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required for GetDepthSnapshot")
	}

	params := map[string]any{}
	if limit > 0 {
		params["limit"] = fmt.Sprintf("%d", limit)
	}

	body, err := c.GetCtx(ctx, "/api/v1/contract/depth/"+symbol, params)
	if err != nil {
		return nil, err
	}

	type depthData struct {
		Asks    [][]json.Number `json:"asks"`
		Bids    [][]json.Number `json:"bids"`
		Version int64           `json:"version"`
	}

	data, err := ParseResponse[depthData](body, "depth_snapshot")
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
// Endpoint: GET /api/v1/contract/depth_commits/{symbol}/{limit}.
// Used for packet-loss recovery when maintaining a local incremental orderbook.
func (c *Client) GetDepthCommits(ctx context.Context, symbol string, limit int) ([]exchange.DepthCommit, error) {
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required for GetDepthCommits")
	}
	if limit <= 0 {
		limit = 1000
	}

	path := fmt.Sprintf("/api/v1/contract/depth_commits/%s/%d", symbol, limit)
	body, err := c.GetCtx(ctx, path, nil)
	if err != nil {
		return nil, err
	}

	type rawCommit struct {
		Version int64           `json:"version"`
		Asks    [][]json.Number `json:"asks"`
		Bids    [][]json.Number `json:"bids"`
	}

	rawCommits, err := ParseResponse[[]rawCommit](body, "depth_commits")
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
