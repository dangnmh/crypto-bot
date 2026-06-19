package bybit

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
)

// Explicit request/response structs for market data endpoints.

type bybitServerTimeRequest struct{}

type bybitInstrumentInfoRequest struct {
	Category string `json:"category"`
	Limit    int    `json:"limit,omitempty"`
}

type bybitMarketTickersRequest struct {
	Category string `json:"category"`
	Symbol   string `json:"symbol,omitempty"`
}

type bybitKlinesRequest struct {
	Category string `json:"category"`
	Symbol   string `json:"symbol"`
	Interval string `json:"interval"`
	Start    int64  `json:"start,omitempty"`
	End      int64  `json:"end,omitempty"`
}

type bybitOrderbookRequest struct {
	Category string `json:"category"`
	Symbol   string `json:"symbol"`
	Limit    int    `json:"limit,omitempty"`
}

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

// Private raw methods invoking the Bybit SDK.

func (c *Client) getRawServerTime(ctx context.Context, _ bybitServerTimeRequest) (int64, error) {
	body, err := c.RawRequest(ctx, http.MethodGet, "/v5/market/time", nil, nil)
	if err != nil {
		return 0, fmt.Errorf("bybit get server time: %w", err)
	}
	var resp struct {
		RetCode int    `json:"retCode"`
		RetMsg  string `json:"retMsg"`
		Time    int64  `json:"time"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, fmt.Errorf("bybit get server time json unmarshal: %w", err)
	}
	if resp.RetCode != 0 {
		return 0, fmt.Errorf("bybit get server time error: retCode=%d, retMsg=%s", resp.RetCode, resp.RetMsg)
	}
	return resp.Time, nil
}

func (c *Client) getRawInstrumentInfo(ctx context.Context, req bybitInstrumentInfoRequest) (*bybitInstrumentsInfoResult, error) {
	params := map[string]string{}
	if req.Category != "" {
		params["category"] = req.Category
	}
	if req.Limit > 0 {
		params["limit"] = fmt.Sprintf("%d", req.Limit)
	}
	body, err := c.RawRequest(ctx, http.MethodGet, "/v5/market/instruments-info", params, nil)
	if err != nil {
		return nil, fmt.Errorf("bybit list contracts: %w", err)
	}
	res, err := parseResponse[bybitInstrumentsInfoResult](body, "bybit list contracts")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *Client) getRawMarketTickers(ctx context.Context, req bybitMarketTickersRequest) (*bybitTickerList, error) {
	params := map[string]string{}
	if req.Category != "" {
		params["category"] = req.Category
	}
	if req.Symbol != "" {
		params["symbol"] = req.Symbol
	}
	body, err := c.GetTickersRaw(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("bybit list tickers: %w", err)
	}
	res, err := parseResponse[bybitTickerList](body, "bybit list tickers")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *Client) getRawFundingRate(ctx context.Context, symbol string) (*bybitTicker, error) {
	res, err := c.getRawMarketTickers(ctx, bybitMarketTickersRequest{
		Category: categoryLinear,
		Symbol:   symbol,
	})
	if err != nil {
		return nil, err
	}
	if len(res.List) == 0 {
		return nil, fmt.Errorf("bybit ticker not found for symbol: %s", symbol)
	}
	return &res.List[0], nil
}

func (c *Client) getRawKlines(ctx context.Context, req bybitKlinesRequest) (*bybitKlineResult, error) {
	params := map[string]string{}
	if req.Category != "" {
		params["category"] = req.Category
	}
	if req.Symbol != "" {
		params["symbol"] = req.Symbol
	}
	if req.Interval != "" {
		params["interval"] = req.Interval
	}
	if req.Start > 0 {
		params["start"] = fmt.Sprintf("%d", req.Start)
	}
	if req.End > 0 {
		params["end"] = fmt.Sprintf("%d", req.End)
	}
	body, err := c.RawRequest(ctx, http.MethodGet, "/v5/market/kline", params, nil)
	if err != nil {
		return nil, fmt.Errorf("bybit get klines: %w", err)
	}
	res, err := parseResponse[bybitKlineResult](body, "bybit get klines")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *Client) getRawDepthSnapshot(ctx context.Context, req bybitOrderbookRequest) (*bybitOrderbookResult, error) {
	params := map[string]string{}
	if req.Category != "" {
		params["category"] = req.Category
	}
	if req.Symbol != "" {
		params["symbol"] = req.Symbol
	}
	if req.Limit > 0 {
		params["limit"] = fmt.Sprintf("%d", req.Limit)
	}
	body, err := c.RawRequest(ctx, http.MethodGet, "/v5/market/orderbook", params, nil)
	if err != nil {
		return nil, fmt.Errorf("bybit order book: %w", err)
	}
	res, err := parseResponse[bybitOrderbookResult](body, "bybit order book")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

// Public mapper methods implementing the exchange.MarketDataProvider interface.

// GetServerTime returns the Bybit server timestamp in milliseconds.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	return c.getRawServerTime(ctx, bybitServerTimeRequest{})
}

// GetContractDetails returns all contract specifications.
func (c *Client) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	res, err := c.getRawInstrumentInfo(ctx, bybitInstrumentInfoRequest{
		Category: categoryLinear,
		Limit:    1000,
	})
	if err != nil {
		return nil, err
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

// GetFundingRates returns current funding rate details for the specified symbols.
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
		nextSettle := int64(0)
		if raw.NextFundingTime != "" {
			if parsed, err := strconv.ParseInt(raw.NextFundingTime, 10, 64); err == nil {
				nextSettle = parsed
			}
		}
		rates = append(rates, exchange.FundingRateResult{
			Symbol:     raw.Symbol,
			Rate:       decmath.ParseFloat(raw.FundingRate),
			SettleTime: nextSettle,
		})
	}
	return rates, nil
}

// GetTickers returns ticker data for a specific symbol or all symbols.
func (c *Client) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	res, err := c.getRawMarketTickers(ctx, bybitMarketTickersRequest{
		Category: categoryLinear,
		Symbol:   symbol,
	})
	if err != nil {
		return nil, err
	}

	tickers := make([]exchange.Ticker, 0, len(res.List))
	for i := range res.List {
		raw := &res.List[i]
		amt := decmath.ParseFloat(raw.Turnover24h)
		tickers = append(tickers, exchange.Ticker{
			Symbol:       raw.Symbol,
			LastPrice:    decmath.ParseFloat(raw.LastPrice),
			Bid1:         decmath.ParseFloat(raw.Bid1Price),
			Ask1:         decmath.ParseFloat(raw.Ask1Price),
			Volume24:     decmath.ParseFloat(raw.Volume24h),
			AmountUSDT24: amt,
			Timestamp:    time.Now().UnixMilli(),
		})
	}
	return tickers, nil
}

// GetKlines returns candlestick data for a symbol.
func (c *Client) GetKlines(ctx context.Context, symbol, interval string, start, end int64) ([]exchange.Kline, error) {
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required for GetKlines")
	}

	bybitInterval := mapInterval(interval)
	res, err := c.getRawKlines(ctx, bybitKlinesRequest{
		Category: categoryLinear,
		Symbol:   symbol,
		Interval: bybitInterval,
		Start:    start,
		End:      end,
	})
	if err != nil {
		return nil, err
	}

	klines := make([]exchange.Kline, 0, len(res.List))
	// Bybit returns klines in reverse chronological order (newest first). We reverse them
	// to standardize oldest first.
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

	ob, err := c.getRawDepthSnapshot(ctx, bybitOrderbookRequest{
		Category: categoryLinear,
		Symbol:   symbol,
		Limit:    limit,
	})
	if err != nil {
		return nil, err
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
