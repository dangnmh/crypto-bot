package gate

import (
	"context"
	"fmt"
	"strings"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"

	"github.com/antihax/optional"
	"github.com/gateio/gateapi-go/v7"
)

// GetServerTime returns the Gate.io server timestamp in milliseconds.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	resp, httpResp, err := c.apiClient.SpotApi.GetSystemTime(ctx)
	if httpResp != nil && httpResp.Body != nil {
		_ = httpResp.Body.Close()
	}
	if err != nil {
		return 0, fmt.Errorf("gate.io get server time: %w", err)
	}
	return resp.ServerTime, nil
}

// GetContractDetails returns all contract specifications.
func (c *Client) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	contracts, httpResp, err := c.apiClient.FuturesApi.ListFuturesContracts(ctx, "usdt", nil)
	if httpResp != nil && httpResp.Body != nil {
		defer func() { _ = httpResp.Body.Close() }()
	}
	if err != nil {
		return nil, fmt.Errorf("gate.io list contracts: %w", err)
	}

	details := make([]exchange.ContractDetail, 0, len(contracts))
	for i := range contracts {
		raw := &contracts[i]
		parts := strings.Split(raw.Name, "_")
		baseCoin := ""
		quoteCoin := ""
		settleCoin := "USDT"
		if len(parts) == 2 {
			baseCoin = parts[0]
			quoteCoin = parts[1]
			settleCoin = parts[1]
		}

		details = append(details, exchange.ContractDetail{
			Symbol:        raw.Name,
			DisplayName:   raw.Name,
			DisplayNameEn: raw.Name,
			BaseCoin:      baseCoin,
			QuoteCoin:     quoteCoin,
			SettleCoin:    settleCoin,
			ContractSize:  decmath.ParseFloat(raw.QuantoMultiplier),
			MinLeverage:   decmath.ParseInt(raw.LeverageMin),
			MaxLeverage:   decmath.ParseInt(raw.LeverageMax),
			PriceUnit:     decmath.ParseFloat(raw.OrderPriceRound),
			MakerFeeRate:  decmath.ParseFloat(raw.MakerFeeRate),
			TakerFeeRate:  decmath.ParseFloat(raw.TakerFeeRate),
			PriceScale:    8, // standard precision scale fallback
			State:         1, // active
		})
	}
	return details, nil
}

func (c *Client) getGateFundingRates(ctx context.Context, symbol string) ([]exchange.FundingRateResult, error) {
	var opts *gateapi.ListFuturesTickersOpts
	if symbol != "" {
		opts = &gateapi.ListFuturesTickersOpts{
			Contract: optional.NewString(symbol),
		}
	}
	rawTickers, httpResp, err := c.apiClient.FuturesApi.ListFuturesTickers(ctx, "usdt", opts)
	if httpResp != nil && httpResp.Body != nil {
		defer func() { _ = httpResp.Body.Close() }()
	}
	if err != nil {
		return nil, fmt.Errorf("gate.io list tickers: %w", err)
	}

	rates := make([]exchange.FundingRateResult, 0, len(rawTickers))
	for i := range rawTickers {
		raw := &rawTickers[i]
		rates = append(rates, exchange.FundingRateResult{
			Symbol:     raw.Contract,
			Rate:       decmath.ParseFloat(raw.FundingRate),
			SettleTime: 0,
		})
	}
	return rates, nil
}

// GetTickers returns ticker data for a specific symbol or all symbols.
func (c *Client) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	var opts *gateapi.ListFuturesTickersOpts
	if symbol != "" {
		opts = &gateapi.ListFuturesTickersOpts{
			Contract: optional.NewString(symbol),
		}
	}

	rawTickers, httpResp, err := c.apiClient.FuturesApi.ListFuturesTickers(ctx, "usdt", opts)
	if httpResp != nil && httpResp.Body != nil {
		defer func() { _ = httpResp.Body.Close() }()
	}
	if err != nil {
		return nil, fmt.Errorf("gate.io list tickers: %w", err)
	}

	tickers := make([]exchange.Ticker, 0, len(rawTickers))
	for i := range rawTickers {
		raw := &rawTickers[i]
		tickers = append(tickers, exchange.Ticker{
			Symbol:         raw.Contract,
			LastPrice:      decmath.ParseFloat(raw.Last),
			Bid1:           decmath.ParseFloat(raw.HighestBid),
			Ask1:           decmath.ParseFloat(raw.LowestAsk),
			Volume24:       decmath.ParseFloat(raw.Volume24h),
			Amount24:       decmath.ParseFloat(raw.Volume24hQuote),
			FundingRate:    decmath.ParseFloat(raw.FundingRate),
			NextSettleTime: 0, // not present in REST ticker, resolved via GetFundingRate
			Timestamp:      time.Now().UnixMilli(),
		})
	}
	return tickers, nil
}

// GetFundingRates returns current funding rate details for the specified symbols.
func (c *Client) GetFundingRates(ctx context.Context, symbols []string) ([]exchange.FundingRateResult, error) {
	if len(symbols) == 0 {
		return nil, nil
	}
	allRates, err := c.getGateFundingRates(ctx, "")
	if err != nil {
		return nil, err
	}
	symbolMap := make(map[string]bool)
	for _, sym := range symbols {
		symbolMap[sym] = true
	}
	var rates []exchange.FundingRateResult
	for _, r := range allRates {
		if symbolMap[r.Symbol] {
			rates = append(rates, r)
		}
	}
	return rates, nil
}

// GetKlines returns candlestick data for a symbol.
func (c *Client) GetKlines(ctx context.Context, symbol, interval string, start, end int64) ([]exchange.Kline, error) {
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required for GetKlines")
	}

	// Map interval to Gate.io structure
	gateInterval := "1m"
	switch interval {
	case "Min1", "1m":
		gateInterval = "1m"
	case "Min5", "5m":
		gateInterval = "5m"
	case "Min15", gateInterval15m:
		gateInterval = gateInterval15m
	case "Min30", gateInterval30m:
		gateInterval = gateInterval30m
	case "Hour1", "1h":
		gateInterval = "1h"
	case "Hour4", "4h":
		gateInterval = "4h"
	case "Day1", "1d":
		gateInterval = "1d"
	}

	opts := &gateapi.ListFuturesCandlesticksOpts{
		Interval: optional.NewString(gateInterval),
	}
	if start > 0 {
		opts.From = optional.NewInt64(start / 1000) // ms to seconds
	}
	if end > 0 {
		opts.To = optional.NewInt64(end / 1000) // ms to seconds
	}

	candles, httpResp, err := c.apiClient.FuturesApi.ListFuturesCandlesticks(ctx, "usdt", symbol, opts)
	if httpResp != nil && httpResp.Body != nil {
		defer func() { _ = httpResp.Body.Close() }()
	}
	if err != nil {
		return nil, fmt.Errorf("gate.io list klines: %w", err)
	}

	klines := make([]exchange.Kline, 0, len(candles))
	for _, candle := range candles {
		klines = append(klines, exchange.Kline{
			Timestamp: int64(candle.T * 1000), // convert to ms
			Open:      decmath.ParseFloat(candle.O),
			Close:     decmath.ParseFloat(candle.C),
			High:      decmath.ParseFloat(candle.H),
			Low:       decmath.ParseFloat(candle.L),
			Volume:    float64(candle.V),
			Amount:    decmath.ParseFloat(candle.Sum),
		})
	}
	return klines, nil
}

// GetDepthSnapshot returns full orderbook snapshot via REST.
func (c *Client) GetDepthSnapshot(ctx context.Context, symbol string, limit int) (*domain.OrderBook, error) {
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required for GetDepthSnapshot")
	}

	var opts *gateapi.ListFuturesOrderBookOpts
	if limit > 0 {
		opts = &gateapi.ListFuturesOrderBookOpts{
			Limit: optional.NewInt32(int32(limit)),
		}
	}

	ob, httpResp, err := c.apiClient.FuturesApi.ListFuturesOrderBook(ctx, "usdt", symbol, opts)
	if httpResp != nil && httpResp.Body != nil {
		defer func() { _ = httpResp.Body.Close() }()
	}
	if err != nil {
		return nil, fmt.Errorf("gate.io order book: %w", err)
	}

	book := &domain.OrderBook{
		Symbol:  symbol,
		Version: ob.Id,
		Asks:    make([]exchange.OrderBookEntry, 0, len(ob.Asks)),
		Bids:    make([]exchange.OrderBookEntry, 0, len(ob.Bids)),
	}

	for _, item := range ob.Asks {
		p := decmath.ParseFloat(item.P)
		v := float64(item.S)
		if p > 0 {
			book.Asks = append(book.Asks, exchange.OrderBookEntry{Price: p, Volume: v})
		}
	}

	for _, item := range ob.Bids {
		p := decmath.ParseFloat(item.P)
		v := float64(item.S)
		if p > 0 {
			book.Bids = append(book.Bids, exchange.OrderBookEntry{Price: p, Volume: v})
		}
	}

	return book, nil
}

// GetDepthCommits returns incremental commits. Unused in application logic.
func (c *Client) GetDepthCommits(ctx context.Context, symbol string, limit int) ([]exchange.DepthCommit, error) {
	return nil, nil
}
