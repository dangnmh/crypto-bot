package gate

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
)

// Explicit request/response structs for market data endpoints.

type gateContractsRequest struct {
	Settle string `json:"settle"`
}

type gateTickersRequest struct {
	Settle   string `json:"settle"`
	Contract string `json:"contract,omitempty"`
}

type gateKlinesRequest struct {
	Settle   string `json:"settle"`
	Contract string `json:"contract"`
	Interval string `json:"interval"`
	From     int64  `json:"from,omitempty"`
	To       int64  `json:"to,omitempty"`
}

type gateDepthRequest struct {
	Settle   string `json:"settle"`
	Contract string `json:"contract"`
	Limit    int    `json:"limit,omitempty"`
}

// Private raw methods using raw HTTP requests.

func (c *Client) getRawServerTime(ctx context.Context) (*gateSystemTime, error) {
	var result gateSystemTime
	err := c.sendRequest(ctx, "GET", "/spot/time", nil, nil, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) getRawContractDetails(ctx context.Context, req gateContractsRequest) ([]gateContract, error) {
	var result []gateContract
	path := fmt.Sprintf("/futures/%s/contracts", req.Settle)
	err := c.sendRequest(ctx, "GET", path, nil, nil, &result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) getRawTickers(ctx context.Context, req gateTickersRequest) ([]gateFuturesTicker, error) {
	var result []gateFuturesTicker
	query := url.Values{}
	if req.Contract != "" {
		query.Set("contract", req.Contract)
	}
	path := fmt.Sprintf("/futures/%s/tickers", req.Settle)
	err := c.sendRequest(ctx, "GET", path, query, nil, &result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) getRawKlines(ctx context.Context, req gateKlinesRequest) ([]gateFuturesCandlestick, error) {
	var result []gateFuturesCandlestick
	query := url.Values{}
	query.Set("contract", req.Contract)
	query.Set("interval", req.Interval)
	if req.From > 0 {
		query.Set("from", strconv.FormatInt(req.From, 10))
	}
	if req.To > 0 {
		query.Set("to", strconv.FormatInt(req.To, 10))
	}
	path := fmt.Sprintf("/futures/%s/candlesticks", req.Settle)
	err := c.sendRequest(ctx, "GET", path, query, nil, &result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) getRawDepthSnapshot(ctx context.Context, req gateDepthRequest) (*gateFuturesOrderBook, error) {
	var result gateFuturesOrderBook
	query := url.Values{}
	query.Set("contract", req.Contract)
	if req.Limit > 0 {
		query.Set("limit", strconv.Itoa(req.Limit))
	}
	path := fmt.Sprintf("/futures/%s/order_book", req.Settle)
	err := c.sendRequest(ctx, "GET", path, query, nil, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// Public mapper methods implementing the exchange.MarketDataProvider interface.

// GetServerTime returns the Gate.io server timestamp in milliseconds.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	resp, err := c.getRawServerTime(ctx)
	if err != nil {
		return 0, fmt.Errorf("gate.io get server time: %w", err)
	}
	return resp.ServerTime, nil
}

// GetContractDetails returns all contract specifications.
func (c *Client) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	contracts, err := c.getRawContractDetails(ctx, gateContractsRequest{Settle: gateSettleUsdt})
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

		minVol := int(raw.OrderSizeMin)
		if minVol <= 0 {
			minVol = 1
		}
		maxVol := int(raw.OrderSizeMax)
		if maxVol <= 0 {
			maxVol = 1000000
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
			PriceScale:    decmath.DecimalPlaces(raw.OrderPriceRound),
			VolScale:      0,
			MinVol:        minVol,
			MaxVol:        maxVol,
			State:         1, // active
		})
	}
	return details, nil
}

// GetTickers returns ticker data for a specific symbol or all symbols.
func (c *Client) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	rawTickers, err := c.getRawTickers(ctx, gateTickersRequest{Settle: gateSettleUsdt, Contract: symbol})
	if err != nil {
		return nil, fmt.Errorf("gate.io list tickers: %w", err)
	}

	tickers := make([]exchange.Ticker, 0, len(rawTickers))
	for i := range rawTickers {
		raw := &rawTickers[i]
		tickers = append(tickers, exchange.Ticker{
			Symbol:    raw.Contract,
			LastPrice: decmath.ParseFloat(raw.Last),
			Bid1:      decmath.ParseFloat(raw.HighestBid),
			Ask1:      decmath.ParseFloat(raw.LowestAsk),
			Volume24:  decmath.ParseFloat(raw.Volume24h),
			Amount24:  decmath.ParseFloat(raw.Volume24hQuote),
			Timestamp: time.Now().UnixMilli(),
		})
	}
	return tickers, nil
}

// GetFundingRates returns current funding rate details for the specified symbols.
func (c *Client) GetFundingRates(ctx context.Context, symbols []string) ([]exchange.FundingRateResult, error) {
	if len(symbols) == 0 {
		return nil, nil
	}

	needUsdt, needBtc := determineNeededSettleCoins(symbols)
	contractMap := make(map[string]*gateContract)

	if needUsdt {
		if err := c.fetchContracts(ctx, gateSettleUsdt, contractMap); err != nil {
			return nil, err
		}
	}
	if needBtc {
		if err := c.fetchContracts(ctx, "btc", contractMap); err != nil {
			return nil, err
		}
	}

	rates := make([]exchange.FundingRateResult, 0, len(symbols))
	for _, sym := range symbols {
		contract, exists := contractMap[sym]
		if !exists {
			return nil, fmt.Errorf("gate.io contract not found for symbol: %s", sym)
		}
		rates = append(rates, exchange.FundingRateResult{
			Symbol:     sym,
			Rate:       decmath.ParseFloat(contract.FundingRate),
			SettleTime: int64(contract.FundingNextApply * 1000),
		})
	}

	return rates, nil
}

func determineNeededSettleCoins(symbols []string) (needUsdt, needBtc bool) {
	for _, sym := range symbols {
		if strings.HasSuffix(strings.ToLower(sym), "_usd") {
			needBtc = true
		} else {
			needUsdt = true
		}
	}
	return
}

func (c *Client) fetchContracts(ctx context.Context, settle string, contractMap map[string]*gateContract) error {
	contracts, err := c.getRawContractDetails(ctx, gateContractsRequest{Settle: settle})
	if err != nil {
		return fmt.Errorf("gate.io list %s contracts: %w", settle, err)
	}
	for i := range contracts {
		contractMap[contracts[i].Name] = &contracts[i]
	}
	return nil
}

// GetKlines returns candlestick data for a symbol.
func (c *Client) GetKlines(ctx context.Context, symbol, interval string, start, end int64) ([]exchange.Kline, error) {
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required for GetKlines")
	}

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

	req := gateKlinesRequest{
		Settle:   gateSettleUsdt,
		Contract: symbol,
		Interval: gateInterval,
	}
	if start > 0 {
		req.From = start / 1000 // ms to seconds
	}
	if end > 0 {
		req.To = end / 1000 // ms to seconds
	}

	candles, err := c.getRawKlines(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("gate.io list klines: %w", err)
	}

	klines := make([]exchange.Kline, 0, len(candles))
	for _, candle := range candles {
		klines = append(klines, exchange.Kline{
			Timestamp: candle.T * 1000, // convert to ms
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

	ob, err := c.getRawDepthSnapshot(ctx, gateDepthRequest{
		Settle:   gateSettleUsdt,
		Contract: symbol,
		Limit:    limit,
	})
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
		v := item.S
		if p > 0 {
			book.Asks = append(book.Asks, exchange.OrderBookEntry{Price: p, Volume: v})
		}
	}

	for _, item := range ob.Bids {
		p := decmath.ParseFloat(item.P)
		v := item.S
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
