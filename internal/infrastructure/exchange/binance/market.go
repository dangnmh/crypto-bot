package binance

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"

	"github.com/binance/binance-connector-go/clients/derivativestradingusdsfutures/src/restapi/models"
)

// GetServerTime returns the Binance server timestamp in milliseconds.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	req := c.sdkClient.RestApi.MarketDataAPI.TestConnectivity(ctx)
	_, err := c.sdkClient.RestApi.MarketDataAPI.TestConnectivityExecute(req)
	if err != nil {
		return 0, fmt.Errorf("binance test connectivity: %w", err)
	}
	reqTime := c.sdkClient.RestApi.MarketDataAPI.CheckServerTime(ctx)
	resp, err := c.sdkClient.RestApi.MarketDataAPI.CheckServerTimeExecute(reqTime)
	if err != nil {
		return 0, fmt.Errorf("binance check server time: %w", err)
	}
	return resp.Data.GetServerTime(), nil
}

// GetContractDetails returns all USD-M futures contract specifications.
func (c *Client) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	req := c.sdkClient.RestApi.MarketDataAPI.ExchangeInformation(ctx)
	resp, err := c.sdkClient.RestApi.MarketDataAPI.ExchangeInformationExecute(req)
	if err != nil {
		return nil, fmt.Errorf("binance exchange information: %w", err)
	}

	rawSymbols := resp.Data.GetSymbols()
	details := make([]exchange.ContractDetail, 0, len(rawSymbols))

	for i := range rawSymbols {
		raw := &rawSymbols[i]

		// Filter active perpetual contracts
		if raw.GetStatus() != "TRADING" || raw.GetContractType() != "PERPETUAL" {
			continue
		}

		priceUnit := 0.0
		minVol := 0.0
		stepSize := 0.0

		for _, f := range raw.GetFilters() {
			switch f.GetFilterType() {
			case "PRICE_FILTER":
				priceUnit = decmath.ParseFloat(f.GetTickSize())
			case "LOT_SIZE":
				minVol = decmath.ParseFloat(f.GetMinQty())
				stepSize = decmath.ParseFloat(f.GetStepSize())
			}
		}

		details = append(details, exchange.ContractDetail{
			Symbol:        raw.GetSymbol(),
			DisplayName:   raw.GetSymbol(),
			DisplayNameEn: raw.GetSymbol(),
			BaseCoin:      raw.GetBaseAsset(),
			QuoteCoin:     raw.GetQuoteAsset(),
			SettleCoin:    raw.GetMarginAsset(),
			ContractSize:  1.0, // standard linear perpetual
			MinLeverage:   1,
			MaxLeverage:   125, // common max limit
			PriceUnit:     priceUnit,
			MinVol:        int(minVol),
			VolUnit:       int(stepSize),
			PriceScale:    int(raw.GetPricePrecision()),
			VolScale:      int(raw.GetQuantityPrecision()),
			State:         1, // active
		})
	}

	return details, nil
}

// GetTickers returns ticker data for all symbols or a single symbol.
//
//nolint:cyclop // standard SDK mapping logic contains branch complexity
func (c *Client) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	// 1. Fetch 24h Ticker statistics (to get volumes)
	tickerReq := c.sdkClient.RestApi.MarketDataAPI.Ticker24hrPriceChangeStatistics(ctx)
	if symbol != "" {
		tickerReq = tickerReq.Symbol(symbol)
	}
	tickerResp, err := c.sdkClient.RestApi.MarketDataAPI.Ticker24hrPriceChangeStatisticsExecute(tickerReq)
	if err != nil {
		return nil, fmt.Errorf("binance ticker 24h stats: %w", err)
	}

	volMap := make(map[string]float64)
	amountMap := make(map[string]float64)
	bestBidMap := make(map[string]float64)
	bestAskMap := make(map[string]float64)
	lastMap := make(map[string]float64)

	if tickerResp.Data.Ticker24hrPriceChangeStatisticsResponse2 != nil {
		items := tickerResp.Data.Ticker24hrPriceChangeStatisticsResponse2.Items
		for i := range items {
			t := items[i]
			sym := t.GetSymbol()
			volMap[sym] = decmath.ParseFloat(t.GetVolume())
			amountMap[sym] = decmath.ParseFloat(t.GetQuoteVolume())
			lastMap[sym] = decmath.ParseFloat(t.GetLastPrice())
		}
	} else if tickerResp.Data.Ticker24hrPriceChangeStatisticsResponse1 != nil {
		t := tickerResp.Data.Ticker24hrPriceChangeStatisticsResponse1
		sym := t.GetSymbol()
		volMap[sym] = decmath.ParseFloat(t.GetVolume())
		amountMap[sym] = decmath.ParseFloat(t.GetQuoteVolume())
		lastMap[sym] = decmath.ParseFloat(t.GetLastPrice())
	}

	// 2. Fetch Best Book Prices (bid/ask) using SymbolOrderBookTicker
	bookReq := c.sdkClient.RestApi.MarketDataAPI.SymbolOrderBookTicker(ctx)
	if symbol != "" {
		bookReq = bookReq.Symbol(symbol)
	}
	bookResp, err := c.sdkClient.RestApi.MarketDataAPI.SymbolOrderBookTickerExecute(bookReq)
	if err == nil {
		if bookResp.Data.SymbolOrderBookTickerResponse2 != nil {
			for _, b := range bookResp.Data.SymbolOrderBookTickerResponse2.Items {
				sym := b.GetSymbol()
				bestBidMap[sym] = decmath.ParseFloat(b.GetBidPrice())
				bestAskMap[sym] = decmath.ParseFloat(b.GetAskPrice())
			}
		} else if bookResp.Data.SymbolOrderBookTickerResponse1 != nil {
			b := bookResp.Data.SymbolOrderBookTickerResponse1
			sym := b.GetSymbol()
			bestBidMap[sym] = decmath.ParseFloat(b.GetBidPrice())
			bestAskMap[sym] = decmath.ParseFloat(b.GetAskPrice())
		}
	}

	// 3. Fetch Funding Rates & Mark Prices using PremiumIndex/MarkPrice
	mpReq := c.sdkClient.RestApi.MarketDataAPI.MarkPrice(ctx)
	if symbol != "" {
		mpReq = mpReq.Symbol(symbol)
	}
	mpResp, err := c.sdkClient.RestApi.MarketDataAPI.MarkPriceExecute(mpReq)
	if err != nil {
		return nil, fmt.Errorf("binance mark price premium index: %w", err)
	}

	var tickers []exchange.Ticker
	now := time.Now().UnixMilli()

	if mpResp.Data.MarkPriceResponse2 != nil {
		for _, item := range mpResp.Data.MarkPriceResponse2.Items {
			sym := item.GetSymbol()
			tickers = append(tickers, exchange.Ticker{
				Symbol:         sym,
				LastPrice:      lastMap[sym],
				Bid1:           bestBidMap[sym],
				Ask1:           bestAskMap[sym],
				Volume24:       volMap[sym],
				Amount24:       amountMap[sym],
				IndexPrice:     decmath.ParseFloat(item.GetIndexPrice()),
				FairPrice:      decmath.ParseFloat(item.GetMarkPrice()),
				FundingRate:    decmath.ParseFloat(item.GetLastFundingRate()),
				NextSettleTime: item.GetNextFundingTime(),
				Timestamp:      now,
			})
		}
	} else if mpResp.Data.MarkPriceResponse1 != nil {
		item := mpResp.Data.MarkPriceResponse1
		sym := item.GetSymbol()
		tickers = append(tickers, exchange.Ticker{
			Symbol:         sym,
			LastPrice:      lastMap[sym],
			Bid1:           bestBidMap[sym],
			Ask1:           bestAskMap[sym],
			Volume24:       volMap[sym],
			Amount24:       amountMap[sym],
			IndexPrice:     decmath.ParseFloat(item.GetIndexPrice()),
			FairPrice:      decmath.ParseFloat(item.GetMarkPrice()),
			FundingRate:    decmath.ParseFloat(item.GetLastFundingRate()),
			NextSettleTime: item.GetNextFundingTime(),
			Timestamp:      now,
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
		return nil, fmt.Errorf("binance ticker not found for: %s", symbol)
	}
	t := tickers[0]

	return &exchange.FundingRateDetail{
		Symbol:         symbol,
		FundingRate:    t.FundingRate,
		NextSettleTime: t.NextSettleTime,
		Timestamp:      time.Now().UnixMilli(),
	}, nil
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

	req := c.sdkClient.RestApi.MarketDataAPI.KlineCandlestickData(ctx).
		Symbol(symbol).
		Interval(models.ContinuousContractKlineCandlestickDataIntervalParameter(binanceInterval))

	if start > 0 {
		req = req.StartTime(start)
	}
	if end > 0 {
		req = req.EndTime(end)
	}

	resp, err := c.sdkClient.RestApi.MarketDataAPI.KlineCandlestickDataExecute(req)
	if err != nil {
		return nil, fmt.Errorf("binance klines: %w", err)
	}

	items := resp.Data.Items
	klines := make([]exchange.Kline, 0, len(items))

	for _, item := range items {
		if len(item.Items) < 8 {
			continue
		}

		openTime := getInt64(item.Items[0])
		openPrice := getString(item.Items[1])
		highPrice := getString(item.Items[2])
		lowPrice := getString(item.Items[3])
		closePrice := getString(item.Items[4])
		volume := getString(item.Items[5])
		quoteVolume := getString(item.Items[7])

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

func getInt64(inner models.KlineCandlestickDataResponseItemInner) int64 {
	if inner.Int64 != nil {
		return *inner.Int64
	}
	if inner.String != nil {
		val, _ := strconv.ParseInt(*inner.String, 10, 64)
		return val
	}
	return 0
}

func getString(inner models.KlineCandlestickDataResponseItemInner) string {
	if inner.String != nil {
		return *inner.String
	}
	if inner.Int64 != nil {
		return strconv.FormatInt(*inner.Int64, 10)
	}
	return ""
}

// GetDepthSnapshot returns full orderbook snapshot.
func (c *Client) GetDepthSnapshot(ctx context.Context, symbol string, limit int) (*domain.OrderBook, error) {
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required for GetDepthSnapshot")
	}

	req := c.sdkClient.RestApi.MarketDataAPI.OrderBook(ctx).Symbol(symbol)
	if limit > 0 {
		req = req.Limit(int64(limit))
	}

	resp, err := c.sdkClient.RestApi.MarketDataAPI.OrderBookExecute(req)
	if err != nil {
		return nil, fmt.Errorf("binance orderbook snapshot: %w", err)
	}

	ob := resp.Data
	book := &domain.OrderBook{
		Symbol:  symbol,
		Version: ob.GetLastUpdateId(),
		Asks:    make([]exchange.OrderBookEntry, 0, len(ob.GetAsks())),
		Bids:    make([]exchange.OrderBookEntry, 0, len(ob.GetBids())),
	}

	for _, ask := range ob.GetAsks() {
		if len(ask.Items) < 2 {
			continue
		}
		p := decmath.ParseFloat(ask.Items[0])
		v := decmath.ParseFloat(ask.Items[1])
		if p > 0 {
			book.Asks = append(book.Asks, exchange.OrderBookEntry{Price: p, Volume: v})
		}
	}

	for _, bid := range ob.GetBids() {
		if len(bid.Items) < 2 {
			continue
		}
		p := decmath.ParseFloat(bid.Items[0])
		v := decmath.ParseFloat(bid.Items[1])
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
