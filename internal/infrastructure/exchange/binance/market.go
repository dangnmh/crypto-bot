package binance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
)

// Explicit request/response structs for market data endpoints.

type binanceServerTimeRequest struct{}
type binanceContractDetailsRequest struct{}

type binanceVolumes24hRequest struct {
	Symbol string
}

type binanceBookTickersRequest struct {
	Symbol string
}

type binanceMarkPricesRequest struct {
	Symbol string
}

type binanceKlinesRequest struct {
	Symbol    string
	Interval  string
	StartTime int64
	EndTime   int64
}

type binanceDepthRequest struct {
	Symbol string
	Limit  int
}

// Private raw methods invoking the Binance API directly.

func (c *Client) getRawServerTime(ctx context.Context, _ binanceServerTimeRequest) (*checkServerTimeResponse, error) {
	var resp checkServerTimeResponse
	err := c.request(ctx, http.MethodGet, "/fapi/v1/time", nil, false, &resp)
	if err != nil {
		return nil, fmt.Errorf("binance check server time: %w", err)
	}
	return &resp, nil
}

func (c *Client) getRawContractDetails(ctx context.Context, _ binanceContractDetailsRequest) (*exchangeInformationResponse, error) {
	var resp exchangeInformationResponse
	err := c.request(ctx, http.MethodGet, "/fapi/v1/exchangeInfo", nil, false, &resp)
	if err != nil {
		return nil, fmt.Errorf("binance exchange information: %w", err)
	}
	return &resp, nil
}

func getRawList[T any](c *Client, ctx context.Context, path, symbol, label string) ([]T, error) {
	reqURL, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("parse path: %w", err)
	}

	params := make(map[string]any)
	for k, vs := range reqURL.Query() {
		if len(vs) > 0 {
			params[k] = vs[0]
		}
	}
	if symbol != "" {
		params["symbol"] = symbol
	}

	if len(params) > 0 {
		reqURL.RawQuery = c.encodeParams(params, false)
	}

	fullURL := c.baseURL + reqURL.String()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, http.NoBody)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("binance %s HTTP request: %w", label, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("binance %s error: status=%d body=%s", label, resp.StatusCode, string(body))
	}

	var list []T
	if len(body) > 0 && body[0] == '[' {
		if err := json.Unmarshal(body, &list); err != nil {
			return nil, err
		}
	} else {
		var single T
		if err := json.Unmarshal(body, &single); err != nil {
			return nil, err
		}
		list = []T{single}
	}

	return list, nil
}

func (c *Client) getRawVolumes24h(ctx context.Context, req binanceVolumes24hRequest) ([]ticker24hStats, error) {
	return getRawList[ticker24hStats](c, ctx, "/fapi/v1/ticker/24hr", req.Symbol, "ticker 24h stats")
}

func (c *Client) getRawBookTickers(ctx context.Context, req binanceBookTickersRequest) ([]bookTicker, error) {
	return getRawList[bookTicker](c, ctx, "/fapi/v1/ticker/bookTicker", req.Symbol, "book tickers")
}

func (c *Client) getRawMarkPrices(ctx context.Context, req binanceMarkPricesRequest) ([]markPriceInfo, error) {
	return getRawList[markPriceInfo](c, ctx, "/fapi/v1/premiumIndex", req.Symbol, "premium index")
}

func (c *Client) getRawKlines(ctx context.Context, req binanceKlinesRequest) ([][]any, error) {
	params := make(map[string]any)
	params["symbol"] = req.Symbol
	params["interval"] = req.Interval
	if req.StartTime > 0 {
		params["startTime"] = req.StartTime
	}
	if req.EndTime > 0 {
		params["endTime"] = req.EndTime
	}

	var resp [][]any
	err := c.request(ctx, http.MethodGet, "/fapi/v1/klines", params, false, &resp)
	if err != nil {
		return nil, fmt.Errorf("binance klines: %w", err)
	}
	return resp, nil
}

func (c *Client) getRawDepthSnapshot(ctx context.Context, req binanceDepthRequest) (*depthResponse, error) {
	params := make(map[string]any)
	params["symbol"] = req.Symbol
	if req.Limit > 0 {
		params["limit"] = req.Limit
	}

	var resp depthResponse
	err := c.request(ctx, http.MethodGet, "/fapi/v1/depth", params, false, &resp)
	if err != nil {
		return nil, fmt.Errorf("binance orderbook snapshot: %w", err)
	}
	return &resp, nil
}

// Public mapper methods implementing the exchange.MarketDataProvider interface.

// GetServerTime returns the Binance server timestamp in milliseconds.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	resp, err := c.getRawServerTime(ctx, binanceServerTimeRequest{})
	if err != nil {
		return 0, err
	}
	return resp.ServerTime, nil
}

// GetContractDetails returns all USD-M futures contract specifications.
func (c *Client) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	resp, err := c.getRawContractDetails(ctx, binanceContractDetailsRequest{})
	if err != nil {
		return nil, err
	}

	rawSymbols := resp.Symbols
	details := make([]exchange.ContractDetail, 0, len(rawSymbols))

	for i := range rawSymbols {
		raw := &rawSymbols[i]

		// Filter active perpetual contracts.
		if raw.Status != "TRADING" || raw.ContractType != "PERPETUAL" {
			continue
		}

		priceUnit := 0.0
		minVol := 0.0
		stepSize := 0.0

		for _, f := range raw.Filters {
			switch f.FilterType {
			case "PRICE_FILTER":
				priceUnit = decmath.ParseFloat(f.TickSize)
			case "LOT_SIZE":
				minVol = decmath.ParseFloat(f.MinQty)
				stepSize = decmath.ParseFloat(f.StepSize)
			}
		}

		details = append(details, exchange.ContractDetail{
			Symbol:        raw.Symbol,
			DisplayName:   raw.Symbol,
			DisplayNameEn: raw.Symbol,
			BaseCoin:      raw.BaseAsset,
			QuoteCoin:     raw.QuoteAsset,
			SettleCoin:    raw.MarginAsset,
			ContractSize:  1.0, // standard linear perpetual.
			MinLeverage:   1,
			MaxLeverage:   125, // common max limit.
			PriceUnit:     priceUnit,
			MinVol:        int(minVol),
			VolUnit:       int(stepSize),
			PriceScale:    int(raw.PricePrecision),
			VolScale:      int(raw.QuantityPrecision),
			State:         1, // active.
		})
	}

	return details, nil
}

func (c *Client) getBinanceVolumes24h(ctx context.Context, symbol string) (vols, amounts, lasts map[string]float64, err error) {
	resp, err := c.getRawVolumes24h(ctx, binanceVolumes24hRequest{Symbol: symbol})
	if err != nil {
		return nil, nil, nil, err
	}

	vols = make(map[string]float64)
	amounts = make(map[string]float64)
	lasts = make(map[string]float64)
	for i := range resp {
		t := &resp[i]
		vols[t.Symbol] = decmath.ParseFloat(t.Volume)
		amounts[t.Symbol] = decmath.ParseFloat(t.QuoteVolume)
		lasts[t.Symbol] = decmath.ParseFloat(t.LastPrice)
	}

	return vols, amounts, lasts, nil
}

func (c *Client) getBinanceBookTickers(ctx context.Context, symbol string) (bids, asks map[string]float64, err error) {
	resp, err := c.getRawBookTickers(ctx, binanceBookTickersRequest{Symbol: symbol})
	if err != nil {
		return nil, nil, err
	}

	bids = make(map[string]float64)
	asks = make(map[string]float64)
	for i := range resp {
		b := &resp[i]
		bids[b.Symbol] = decmath.ParseFloat(b.BidPrice)
		asks[b.Symbol] = decmath.ParseFloat(b.AskPrice)
	}

	return bids, asks, nil
}

func (c *Client) getBinanceMarkPrices(ctx context.Context, symbol string) ([]markPriceInfo, error) {
	resp, err := c.getRawMarkPrices(ctx, binanceMarkPricesRequest{Symbol: symbol})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) GetFundingRates(ctx context.Context, symbols []string) ([]exchange.FundingRateResult, error) {
	if len(symbols) == 0 {
		return nil, nil
	}

	mpList, err := c.getBinanceMarkPrices(ctx, "")
	if err != nil {
		return nil, err
	}

	symbolMap := make(map[string]bool)
	for _, sym := range symbols {
		symbolMap[sym] = true
	}

	rates := make([]exchange.FundingRateResult, 0)
	for i := range mpList {
		item := &mpList[i]
		if symbolMap[item.Symbol] {
			rates = append(rates, exchange.FundingRateResult{
				Symbol:     item.Symbol,
				Rate:       decmath.ParseFloat(item.LastFundingRate),
				SettleTime: item.NextFundingTime,
			})
		}
	}

	return rates, nil
}

// GetTickers returns ticker data for all symbols or a single symbol.
func (c *Client) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	volMap, amountMap, lastMap, err := c.getBinanceVolumes24h(ctx, symbol)
	if err != nil {
		return nil, err
	}

	bestBidMap, bestAskMap, err := c.getBinanceBookTickers(ctx, symbol)
	if err != nil {
		return nil, err
	}

	mpList, err := c.getBinanceMarkPrices(ctx, symbol)
	if err != nil {
		return nil, err
	}

	now := time.Now().UnixMilli()
	tickers := make([]exchange.Ticker, 0, len(mpList))
	for i := range mpList {
		sym := mpList[i].Symbol
		amt := amountMap[sym]
		tickers = append(tickers, exchange.Ticker{
			Symbol:       sym,
			LastPrice:    lastMap[sym],
			Bid1:         bestBidMap[sym],
			Ask1:         bestAskMap[sym],
			Volume24:     volMap[sym],
			AmountUSDT24: amt,
			Timestamp:    now,
		})
	}
	return tickers, nil
}

// GetKlines returns candlestick data.
func (c *Client) GetKlines(ctx context.Context, symbol, interval string, start, end int64) ([]exchange.Kline, error) {
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required for GetKlines")
	}

	binanceInterval := "1m"
	switch interval {
	case "Min1", "1m":
		binanceInterval = "1m"
	case "Min5", "5m":
		binanceInterval = "5m"
	case "Min15", interval15m:
		binanceInterval = interval15m
	case "Min30", interval30m:
		binanceInterval = interval30m
	case "Hour1", "1h":
		binanceInterval = "1h"
	case "Hour4", "4h":
		binanceInterval = "4h"
	case "Day1", "1d":
		binanceInterval = "1d"
	}

	resp, err := c.getRawKlines(ctx, binanceKlinesRequest{
		Symbol:    symbol,
		Interval:  binanceInterval,
		StartTime: start,
		EndTime:   end,
	})
	if err != nil {
		return nil, err
	}

	klines := make([]exchange.Kline, 0, len(resp))

	for _, item := range resp {
		if len(item) < 8 {
			continue
		}

		openTime := getAnyInt64(item[0])
		openPrice := getAnyString(item[1])
		highPrice := getAnyString(item[2])
		lowPrice := getAnyString(item[3])
		closePrice := getAnyString(item[4])
		volume := getAnyString(item[5])
		quoteVolume := getAnyString(item[7])

		klines = append(klines, exchange.Kline{
			Timestamp: openTime,
			Open:      decmath.ParseFloat(openPrice),
			Close:     decmath.ParseFloat(closePrice),
			High:      decmath.ParseFloat(highPrice),
			Low:       decmath.ParseFloat(lowPrice),
			Volume:    decmath.ParseFloat(volume),
			Amount:    decmath.ParseFloat(quoteVolume),
		})
	}

	return klines, nil
}

func getAnyInt64(val any) int64 {
	switch v := val.(type) {
	case float64:
		return int64(v)
	case string:
		res, _ := strconv.ParseInt(v, 10, 64)
		return res
	default:
		return 0
	}
}

func getAnyString(val any) string {
	switch v := val.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return ""
	}
}

// GetDepthSnapshot returns full orderbook snapshot.
func (c *Client) GetDepthSnapshot(ctx context.Context, symbol string, limit int) (*domain.OrderBook, error) {
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required for GetDepthSnapshot")
	}

	resp, err := c.getRawDepthSnapshot(ctx, binanceDepthRequest{
		Symbol: symbol,
		Limit:  limit,
	})
	if err != nil {
		return nil, err
	}

	book := &domain.OrderBook{
		Symbol:  symbol,
		Version: resp.LastUpdateId,
		Asks:    make([]exchange.OrderBookEntry, 0, len(resp.Asks)),
		Bids:    make([]exchange.OrderBookEntry, 0, len(resp.Bids)),
	}

	for _, ask := range resp.Asks {
		if len(ask) < 2 {
			continue
		}
		p := decmath.ParseFloat(ask[0])
		v := decmath.ParseFloat(ask[1])
		if p > 0 {
			book.Asks = append(book.Asks, exchange.OrderBookEntry{Price: p, Volume: v})
		}
	}

	for _, bid := range resp.Bids {
		if len(bid) < 2 {
			continue
		}
		p := decmath.ParseFloat(bid[0])
		v := decmath.ParseFloat(bid[1])
		if p > 0 {
			book.Bids = append(book.Bids, exchange.OrderBookEntry{Price: p, Volume: v})
		}
	}

	return book, nil
}

// GetDepthCommits incremental depth updates. Unused.
func (c *Client) GetDepthCommits(ctx context.Context, symbol string, limit int) ([]exchange.DepthCommit, error) {
	return nil, nil
}
