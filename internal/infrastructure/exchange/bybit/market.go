package bybit

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"

	"crypto-bot/pkg/xjson"
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
	if err := xjson.Unmarshal(body, &resp); err != nil {
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

func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	// 1. Fetch all tickers (which already includes funding rates)
	res, err := c.getRawMarketTickers(ctx, bybitMarketTickersRequest{
		Category: categoryLinear,
	})
	if err != nil {
		return nil, err
	}

	// 2. Build whitelist and blacklist lookup maps
	whitelistMap := make(map[string]bool)
	for _, sym := range whitelist {
		whitelistMap[sym] = true
	}

	blacklistMap := make(map[string]bool)
	for _, sym := range blacklist {
		blacklistMap[sym] = true
	}

	// 3. Filter and map symbols
	var results []exchange.PotentialFundingResult
	for i := range res.List {
		raw := &res.List[i]
		if blacklistMap[raw.Symbol] {
			continue
		}
		if len(whitelistMap) > 0 && !whitelistMap[raw.Symbol] {
			continue
		}

		vol := decmath.ParseFloat(raw.Turnover24h)
		if vol < minVol24h {
			continue
		}
		if maxVol24h > 0 && vol > maxVol24h {
			continue
		}

		nextSettle := int64(0)
		if raw.NextFundingTime != "" {
			if parsed, err := strconv.ParseInt(raw.NextFundingTime, 10, 64); err == nil {
				nextSettle = parsed
			}
		}

		results = append(results, exchange.PotentialFundingResult{
			Symbol:     raw.Symbol,
			Rate:       decmath.ParseFloat(raw.FundingRate),
			SettleTime: nextSettle,
			Volume24h:  vol,
			Price:      decmath.ParseFloat(raw.LastPrice),
		})
	}

	return results, nil
}
