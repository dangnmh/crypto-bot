package bitmart

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"

	"crypto-bot/pkg/xjson"
)

type bitmartSymbolDetail struct {
	Symbol       string `json:"symbol"`
	LastPrice    string `json:"last_price"`
	Volume24h    string `json:"volume_24h"`
	Turnover24h  string `json:"turnover_24h"`
	ContractSize string `json:"contract_size"`
	FundingRate  string `json:"funding_rate"`
	FundingTime  int64  `json:"funding_time"`
}

type bitmartData struct {
	Symbols []bitmartSymbolDetail `json:"symbols"`
}

type bitmartResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    bitmartData `json:"data"`
}

// GetTickers returns 24hr ticker price change statistics.
func (c *Client) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	query := make(map[string]string)
	if symbol != "" {
		query["symbol"] = symbol
	}

	body, err := c.request(ctx, http.MethodGet, "/contract/public/details", query)
	if err != nil {
		return nil, err
	}

	var resp bitmartResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal bitmart response: %w", err)
	}

	if resp.Code != 1000 {
		return nil, fmt.Errorf("bitmart API error: %d - %s", resp.Code, resp.Message)
	}

	tickers := make([]exchange.Ticker, 0, len(resp.Data.Symbols))
	for i := range resp.Data.Symbols {
		item := &resp.Data.Symbols[i]
		last, _ := strconv.ParseFloat(item.LastPrice, 64)

		bid := last
		ask := last

		vol, _ := strconv.ParseFloat(item.Volume24h, 64)
		if contractSize, err := strconv.ParseFloat(item.ContractSize, 64); err == nil && contractSize > 0 {
			vol *= contractSize
		}

		amt, _ := strconv.ParseFloat(item.Turnover24h, 64)

		tickers = append(tickers, exchange.Ticker{
			Symbol:       item.Symbol,
			LastPrice:    last,
			Bid1:         bid,
			Ask1:         ask,
			Volume24:     vol,
			AmountUSDT24: amt,
			Timestamp:    item.FundingTime,
		})
	}

	return tickers, nil
}

// GetFundingRates returns funding rates for specific standard symbols.
func (c *Client) GetFundingRates(ctx context.Context, symbols []string) ([]exchange.FundingRateResult, error) {
	if len(symbols) == 0 {
		return nil, nil
	}

	body, err := c.request(ctx, http.MethodGet, "/contract/public/details", nil)
	if err != nil {
		return nil, err
	}

	var resp bitmartResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal bitmart response: %w", err)
	}

	if resp.Code != 1000 {
		return nil, fmt.Errorf("bitmart API error: %d - %s", resp.Code, resp.Message)
	}

	rateMap := make(map[string]*bitmartSymbolDetail)
	for i := range resp.Data.Symbols {
		item := &resp.Data.Symbols[i]
		stdSym := item.Symbol
		rateMap[stdSym] = item
	}

	results := make([]exchange.FundingRateResult, 0, len(symbols))
	for _, sym := range symbols {
		stdSym := sym
		item, exists := rateMap[stdSym]
		if !exists {
			continue
		}

		rate, _ := strconv.ParseFloat(item.FundingRate, 64)

		results = append(results, exchange.FundingRateResult{
			Symbol:     sym,
			Rate:       rate,
			SettleTime: item.FundingTime,
		})
	}

	return results, nil
}

func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	body, err := c.request(ctx, http.MethodGet, "/contract/public/details", nil)
	if err != nil {
		return nil, err
	}

	var resp bitmartResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal bitmart response: %w", err)
	}

	if resp.Code != 1000 {
		return nil, fmt.Errorf("bitmart API error: %d - %s", resp.Code, resp.Message)
	}

	whitelistMap := make(map[string]bool)
	for _, sym := range whitelist {
		whitelistMap[sym] = true
	}

	blacklistMap := make(map[string]bool)
	for _, sym := range blacklist {
		blacklistMap[sym] = true
	}

	var results []exchange.PotentialFundingResult
	for i := range resp.Data.Symbols {
		item := &resp.Data.Symbols[i]
		stdSym := item.Symbol
		if blacklistMap[stdSym] {
			continue
		}
		if len(whitelistMap) > 0 && !whitelistMap[stdSym] {
			continue
		}

		amt, _ := strconv.ParseFloat(item.Turnover24h, 64)
		if amt < minVol24h {
			continue
		}
		if maxVol24h > 0 && amt > maxVol24h {
			continue
		}

		rate, _ := strconv.ParseFloat(item.FundingRate, 64)
		price, _ := strconv.ParseFloat(item.LastPrice, 64)
		results = append(results, exchange.PotentialFundingResult{
			Symbol:     item.Symbol,
			Rate:       rate,
			SettleTime: item.FundingTime,
			Volume24h:  amt,
			Price:      price,
		})
	}

	return results, nil
}

type serverTimeResponse struct {
	Code int `json:"code"`
	Data struct {
		ServerTime int64 `json:"server_time"`
	} `json:"data"`
}

// GetServerTime returns system time from system/time.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	body, err := c.request(ctx, http.MethodGet, "/system/time", nil)
	if err != nil {
		return 0, err
	}
	var resp serverTimeResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return 0, fmt.Errorf("unmarshal server time: %w", err)
	}
	if resp.Code != 1000 {
		return 0, fmt.Errorf("bitmart API error: %d", resp.Code)
	}
	return resp.Data.ServerTime, nil
}

type bitmartContractItem struct {
	Symbol         string `json:"symbol"`
	BaseCurrency   string `json:"base_currency"`
	QuoteCurrency  string `json:"quote_currency"`
	ContractSize   string `json:"contract_size"`
	MinLeverage    string `json:"min_leverage"`
	MaxLeverage    string `json:"max_leverage"`
	PricePrecision string `json:"price_precision"`
	VolPrecision   string `json:"vol_precision"`
	MinVolume      string `json:"min_volume"`
	MaxVolume      string `json:"max_volume"`
	Status         string `json:"status"`
}

type bitmartContractData struct {
	Symbols []bitmartContractItem `json:"symbols"`
}

type bitmartContractResponse struct {
	Code    int                 `json:"code"`
	Message string              `json:"message"`
	Data    bitmartContractData `json:"data"`
}

// GetContractDetails returns contracts specs.
func (c *Client) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	body, err := c.request(ctx, http.MethodGet, "/contract/public/details", nil)
	if err != nil {
		return nil, err
	}
	var resp bitmartContractResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal contract details: %w", err)
	}
	if resp.Code != 1000 {
		return nil, fmt.Errorf("bitmart API error: %d - %s", resp.Code, resp.Message)
	}

	details := make([]exchange.ContractDetail, 0, len(resp.Data.Symbols))
	for i := range resp.Data.Symbols {
		item := &resp.Data.Symbols[i]

		priceUnit := decmath.ParseFloat(item.PricePrecision)
		minVol := int(decmath.ParseFloat(item.MinVolume))
		volUnit := int(decmath.ParseFloat(item.VolPrecision))

		priceScale := decmath.DecimalPlaces(item.PricePrecision)
		volScale := decmath.DecimalPlaces(item.VolPrecision)

		multiplier := decmath.ParseFloat(item.ContractSize)
		if multiplier == 0 {
			multiplier = 1.0
		}

		minLev := int(decmath.ParseFloat(item.MinLeverage))
		if minLev == 0 {
			minLev = 1
		}
		maxLev := int(decmath.ParseFloat(item.MaxLeverage))
		if maxLev == 0 {
			maxLev = 100
		}

		state := 0
		if item.Status == "Trading" {
			state = 1
		}

		details = append(details, exchange.ContractDetail{
			Symbol:        item.Symbol,
			DisplayName:   item.Symbol,
			DisplayNameEn: item.Symbol,
			BaseCoin:      item.BaseCurrency,
			QuoteCoin:     item.QuoteCurrency,
			SettleCoin:    item.QuoteCurrency,
			ContractSize:  multiplier,
			MinLeverage:   minLev,
			MaxLeverage:   maxLev,
			PriceUnit:     priceUnit,
			MinVol:        minVol,
			VolUnit:       volUnit,
			PriceScale:    priceScale,
			VolScale:      volScale,
			State:         state,
		})
	}

	return details, nil
}
