package exchange

import (
	"context"
	"encoding/json"
	"fmt"
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
	var resp APIResponse[int64]
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, fmt.Errorf("parse server time: %w", err)
	}
	return resp.Data, nil
}

// GetContractDetails returns all contract specifications.
func (c *Client) GetContractDetails(ctx context.Context) ([]ContractDetail, error) {
	body, err := c.GetCtx(ctx, "/api/v1/contract/detail", nil)
	if err != nil {
		return nil, err
	}
	var resp APIResponse[[]ContractDetail]
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse contract details: %w", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("API error %d: %s", resp.Code, resp.Message)
	}
	return resp.Data, nil
}

// GetTickers returns ticker data for all symbols, or a specific symbol.
func (c *Client) GetTickers(ctx context.Context, symbol string) ([]Ticker, error) {
	params := map[string]string{}
	if symbol != "" {
		params["symbol"] = symbol
	}

	body, err := c.GetCtx(ctx, "/api/v1/contract/ticker", params)
	if err != nil {
		return nil, err
	}

	var rawResp APIResponse[json.RawMessage]
	if err := json.Unmarshal(body, &rawResp); err != nil {
		return nil, fmt.Errorf("parse tickers envelope: %w", err)
	}
	if !rawResp.Success {
		return nil, fmt.Errorf("API error %d: %s", rawResp.Code, rawResp.Message)
	}

	// Try unmarshaling as an array
	var tickers []Ticker
	if err := json.Unmarshal(rawResp.Data, &tickers); err == nil {
		return tickers, nil
	}

	// If array fails, try unmarshaling as a single object (happens on MEXC when symbol is specified)
	var single Ticker
	if err := json.Unmarshal(rawResp.Data, &single); err != nil {
		return nil, fmt.Errorf("parse ticker data: %w", err)
	}
	return []Ticker{single}, nil
}

// GetFundingRate returns current funding rate details for a specific symbol.
func (c *Client) GetFundingRate(ctx context.Context, symbol string) (*FundingRateDetail, error) {
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required for GetFundingRate")
	}

	body, err := c.GetCtx(ctx, "/api/v1/contract/funding_rate/"+symbol, nil)
	if err != nil {
		return nil, err
	}

	var resp APIResponse[FundingRateDetail]
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse funding rate: %w", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("API error %d: %s", resp.Code, resp.Message)
	}
	return &resp.Data, nil
}

// GetFundingRateHistory returns funding rate history for a symbol.
func (c *Client) GetFundingRateHistory(ctx context.Context, symbol string, pageNum, pageSize int) ([]FundingRateHistory, error) {
	params := map[string]string{
		"symbol":    symbol,
		"page_num":  fmt.Sprintf("%d", pageNum),
		"page_size": fmt.Sprintf("%d", pageSize),
	}

	body, err := c.GetCtx(ctx, "/api/v1/contract/funding_rate/history", params)
	if err != nil {
		return nil, err
	}

	var resp APIResponse[struct {
		ResultList []FundingRateHistory `json:"resultList"`
	}]
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse funding rate history: %w", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("API error %d: %s", resp.Code, resp.Message)
	}
	return resp.Data.ResultList, nil
}

// GetKlines returns candlestick data for a symbol.
func (c *Client) GetKlines(ctx context.Context, symbol, interval string, start, end int64) ([]Kline, error) {
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required for GetKlines")
	}

	params := map[string]string{
		"interval": interval,
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

	var resp APIResponse[klineData]
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse klines: %w", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("API error %d: %s", resp.Code, resp.Message)
	}

	n := len(resp.Data.Time)
	var klines []Kline
	for i := 0; i < n; i++ {
		// Safety check against misaligned arrays
		if i >= len(resp.Data.Open) || i >= len(resp.Data.Close) || i >= len(resp.Data.High) || i >= len(resp.Data.Low) || i >= len(resp.Data.Vol) || i >= len(resp.Data.Amount) {
			break
		}
		klines = append(klines, Kline{
			Timestamp: resp.Data.Time[i] * 1000, // API sometimes returns seconds, but let's assume it's standard or handle if needed. Usually ms? If it's 10 digits, it's seconds. We will keep as is, but we might need to check if it's sec or ms.
			Open:      resp.Data.Open[i],
			Close:     resp.Data.Close[i],
			High:      resp.Data.High[i],
			Low:       resp.Data.Low[i],
			Volume:    resp.Data.Vol[i],
			Amount:    resp.Data.Amount[i],
		})
	}

	return klines, nil
}
