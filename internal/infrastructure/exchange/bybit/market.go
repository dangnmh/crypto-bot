package bybit

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
)

type bybitLotSizeFilter struct {
	MaxOrderQty string `json:"maxOrderQty"`
	MinOrderQty string `json:"minOrderQty"`
	QtyStep     string `json:"qtyStep"`
}

type bybitPriceFilter struct {
	TickSize string `json:"tickSize"`
}

type bybitLeverageFilter struct {
	MinLeverage string `json:"minLeverage"`
	MaxLeverage string `json:"maxLeverage"`
}

type bybitInstrumentInfo struct {
	Symbol         string              `json:"symbol"`
	Status         string              `json:"status"`
	BaseCoin       string              `json:"baseCoin"`
	QuoteCoin      string              `json:"quoteCoin"`
	SettleCoin     string              `json:"settleCoin"`
	LotSizeFilter  bybitLotSizeFilter  `json:"lotSizeFilter"`
	PriceFilter    bybitPriceFilter    `json:"priceFilter"`
	LeverageFilter bybitLeverageFilter `json:"leverageFilter"`
}

type bybitInstrumentsInfoResult struct {
	Category string                `json:"category"`
	List     []bybitInstrumentInfo `json:"list"`
}

type bybitTicker struct {
	Symbol          string `json:"symbol"`
	LastPrice       string `json:"lastPrice"`
	Bid1Price       string `json:"bid1Price"`
	Ask1Price       string `json:"ask1Price"`
	Volume24h       string `json:"volume24h"`
	Turnover24h     string `json:"turnover24h"`
	IndexPrice      string `json:"indexPrice"`
	MarkPrice       string `json:"markPrice"`
	FundingRate     string `json:"fundingRate"`
	NextFundingTime string `json:"nextFundingTime"`
}

type bybitTickerList struct {
	Category string        `json:"category"`
	List     []bybitTicker `json:"list"`
}

type bybitKlineResult struct {
	Category string     `json:"category"`
	Symbol   string     `json:"symbol"`
	List     [][]string `json:"list"`
}

type bybitOrderbookResult struct {
	Symbol string     `json:"s"`
	Bids   [][]string `json:"b"`
	Asks   [][]string `json:"a"`
	Ts     int64      `json:"ts"`
	U      int64      `json:"u"`
}

// GetServerTime returns the Bybit server timestamp in milliseconds.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	resp, err := c.sdkClient.NewUtaBybitServiceNoParams().GetServerTime(ctx)
	if err != nil {
		return 0, fmt.Errorf("bybit get server time: %w", err)
	}
	if resp.RetCode != 0 {
		return 0, fmt.Errorf("bybit get server time error: retCode=%d, retMsg=%s", resp.RetCode, resp.RetMsg)
	}
	return resp.Time, nil
}

// GetContractDetails returns all contract specifications.
func (c *Client) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	params := map[string]any{
		categoryKey: categoryLinear,
		limitKey:    1000,
	}
	resp, err := c.sdkClient.NewUtaBybitServiceWithParams(params).GetInstrumentInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("bybit list contracts: %w", err)
	}
	if resp.RetCode != 0 {
		return nil, fmt.Errorf("bybit list contracts error: retCode=%d, retMsg=%s", resp.RetCode, resp.RetMsg)
	}

	var res bybitInstrumentsInfoResult
	if err := decodeResult(resp.Result, &res); err != nil {
		return nil, fmt.Errorf("bybit decode instruments: %w", err)
	}

	details := make([]exchange.ContractDetail, 0, len(res.List))
	for i := range res.List {
		raw := &res.List[i]
		if raw.Status != "Trading" {
			continue
		}

		minVol := 1
		volUnit := 1
		qtyStep := decmath.ParseFloat(raw.LotSizeFilter.QtyStep)
		if qtyStep > 0 && qtyStep < 1 {
			minVol = 1
			volUnit = 1
		} else if qtyStep >= 1 {
			minVol = int(decmath.ParseFloat(raw.LotSizeFilter.MinOrderQty))
			volUnit = int(qtyStep)
		}

		details = append(details, exchange.ContractDetail{
			Symbol:        raw.Symbol,
			DisplayName:   raw.Symbol,
			DisplayNameEn: raw.Symbol,
			BaseCoin:      raw.BaseCoin,
			QuoteCoin:     raw.QuoteCoin,
			SettleCoin:    raw.SettleCoin,
			ContractSize:  1.0, // standard USDT perpetual multiplier fallback
			MinLeverage:   decmath.ParseInt(raw.LeverageFilter.MinLeverage),
			MaxLeverage:   decmath.ParseInt(raw.LeverageFilter.MaxLeverage),
			PriceUnit:     decmath.ParseFloat(raw.PriceFilter.TickSize),
			VolUnit:       volUnit,
			MinVol:        minVol,
			PriceScale:    decmath.DecimalPlaces(raw.PriceFilter.TickSize),
			VolScale:      decmath.DecimalPlaces(raw.LotSizeFilter.QtyStep),
			State:         1, // active trading
		})
	}
	return details, nil
}

// GetTickers returns ticker data for a specific symbol or all symbols.
func (c *Client) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	params := map[string]any{
		categoryKey: categoryLinear,
	}
	if symbol != "" {
		params[symbolKey] = symbol
	}

	resp, err := c.sdkClient.NewUtaBybitServiceWithParams(params).GetMarketTickers(ctx)
	if err != nil {
		return nil, fmt.Errorf("bybit list tickers: %w", err)
	}
	if resp.RetCode != 0 {
		return nil, fmt.Errorf("bybit list tickers error: retCode=%d, retMsg=%s", resp.RetCode, resp.RetMsg)
	}

	var res bybitTickerList
	if err := decodeResult(resp.Result, &res); err != nil {
		return nil, fmt.Errorf("bybit decode tickers: %w", err)
	}

	tickers := make([]exchange.Ticker, 0, len(res.List))
	for i := range res.List {
		raw := &res.List[i]
		nextSettle := int64(0)
		if raw.NextFundingTime != "" {
			if parsed, err := strconv.ParseInt(raw.NextFundingTime, 10, 64); err == nil {
				nextSettle = parsed
			}
		}

		tickers = append(tickers, exchange.Ticker{
			Symbol:         raw.Symbol,
			LastPrice:      decmath.ParseFloat(raw.LastPrice),
			Bid1:           decmath.ParseFloat(raw.Bid1Price),
			Ask1:           decmath.ParseFloat(raw.Ask1Price),
			Volume24:       decmath.ParseFloat(raw.Volume24h),
			Amount24:       decmath.ParseFloat(raw.Turnover24h),
			IndexPrice:     decmath.ParseFloat(raw.IndexPrice),
			FairPrice:      decmath.ParseFloat(raw.MarkPrice),
			FundingRate:    decmath.ParseFloat(raw.FundingRate),
			NextSettleTime: nextSettle,
			Timestamp:      time.Now().UnixMilli(),
		})
	}
	return tickers, nil
}

// GetFundingRate returns current funding rate details for a specific symbol.
func (c *Client) GetFundingRate(ctx context.Context, symbol string) (*exchange.FundingRateDetail, error) {
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required for GetFundingRate")
	}

	tickers, err := c.GetTickers(ctx, symbol)
	if err != nil {
		return nil, err
	}
	if len(tickers) == 0 {
		return nil, fmt.Errorf("bybit ticker not found for: %s", symbol)
	}
	t := tickers[0]

	return &exchange.FundingRateDetail{
		Symbol:         symbol,
		FundingRate:    t.FundingRate,
		NextSettleTime: t.NextSettleTime,
		Timestamp:      time.Now().UnixMilli(),
	}, nil
}

// GetKlines returns candlestick data for a symbol.
func (c *Client) GetKlines(ctx context.Context, symbol, interval string, start, end int64) ([]exchange.Kline, error) {
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required for GetKlines")
	}

	bybitInterval := mapInterval(interval)

	params := map[string]any{
		categoryKey: categoryLinear,
		symbolKey:   symbol,
		"interval":  bybitInterval,
	}
	if start > 0 {
		params["start"] = start
	}
	if end > 0 {
		params["end"] = end
	}

	resp, err := c.sdkClient.NewUtaBybitServiceWithParams(params).GetMarketKline(ctx)
	if err != nil {
		return nil, fmt.Errorf("bybit get klines: %w", err)
	}
	if resp.RetCode != 0 {
		return nil, fmt.Errorf("bybit get klines error: retCode=%d, retMsg=%s", resp.RetCode, resp.RetMsg)
	}

	var res bybitKlineResult
	if err := decodeResult(resp.Result, &res); err != nil {
		return nil, fmt.Errorf("bybit decode klines: %w", err)
	}

	klines := make([]exchange.Kline, 0, len(res.List))
	// Bybit returns klines in reverse chronological order (newest first). Let's reverse them if needed,
	// or parse exactly as Mexc/Gate which standardizes oldest first.
	for _, candle := range slices.Backward(res.List) {
		if len(candle) < 7 {
			continue
		}
		ts, _ := strconv.ParseInt(candle[0], 10, 64)
		klines = append(klines, exchange.Kline{
			Timestamp: ts,
			Open:      decmath.ParseFloat(candle[1]),
			High:      decmath.ParseFloat(candle[2]),
			Low:       decmath.ParseFloat(candle[3]),
			Close:     decmath.ParseFloat(candle[4]),
			Volume:    decmath.ParseFloat(candle[5]),
			Amount:    decmath.ParseFloat(candle[6]),
		})
	}
	return klines, nil
}

// GetDepthSnapshot returns full orderbook snapshot via REST.
func (c *Client) GetDepthSnapshot(ctx context.Context, symbol string, limit int) (*domain.OrderBook, error) {
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required for GetDepthSnapshot")
	}

	params := map[string]any{
		categoryKey: categoryLinear,
		symbolKey:   symbol,
	}
	if limit > 0 {
		params[limitKey] = limit
	}

	resp, err := c.sdkClient.NewUtaBybitServiceWithParams(params).GetOrderBookInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("bybit order book: %w", err)
	}
	if resp.RetCode != 0 {
		return nil, fmt.Errorf("bybit order book error: retCode=%d, retMsg=%s", resp.RetCode, resp.RetMsg)
	}

	var ob bybitOrderbookResult
	if err := decodeResult(resp.Result, &ob); err != nil {
		return nil, fmt.Errorf("bybit decode orderbook: %w", err)
	}

	book := &domain.OrderBook{
		Symbol:  symbol,
		Version: ob.U,
		Asks:    make([]exchange.OrderBookEntry, 0, len(ob.Asks)),
		Bids:    make([]exchange.OrderBookEntry, 0, len(ob.Bids)),
	}

	for _, item := range ob.Asks {
		if len(item) < 2 {
			continue
		}
		p := decmath.ParseFloat(item[0])
		v := decmath.ParseFloat(item[1])
		if p > 0 {
			book.Asks = append(book.Asks, exchange.OrderBookEntry{Price: p, Volume: v})
		}
	}

	for _, item := range ob.Bids {
		if len(item) < 2 {
			continue
		}
		p := decmath.ParseFloat(item[0])
		v := decmath.ParseFloat(item[1])
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

func decodeResult(result, dest any) error {
	bytes, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return json.Unmarshal(bytes, dest)
}

func mapInterval(interval string) string {
	switch interval {
	case "Min1", "1m":
		return "1"
	case "Min5", "5m":
		return "5"
	case "Min15", "15m":
		return "15"
	case "Min30", "30m":
		return "30"
	case "Hour1", "1h":
		return "60"
	case "Hour4", "4h":
		return "240"
	case "Day1", "1d":
		return "D"
	default:
		return "1"
	}
}
