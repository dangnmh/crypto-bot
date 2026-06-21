package mexc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"crypto-bot/internal/infrastructure/exchange"
)

// Explicit request/response structs for market data endpoints.

type mexcContractDetail struct {
	Symbol                    string   `json:"symbol"`
	DisplayName               string   `json:"displayName"`
	DisplayNameEn             string   `json:"displayNameEn"`
	PositionOpenType          int      `json:"positionOpenType"`
	BaseCoin                  string   `json:"baseCoin"`
	QuoteCoin                 string   `json:"quoteCoin"`
	SettleCoin                string   `json:"settleCoin"`
	ContractSize              float64  `json:"contractSize"`
	MinLeverage               int      `json:"minLeverage"`
	MaxLeverage               int      `json:"maxLeverage"`
	PriceScale                int      `json:"priceScale"`
	VolScale                  int      `json:"volScale"`
	AmountScale               int      `json:"amountScale"`
	PriceUnit                 float64  `json:"priceUnit"`
	VolUnit                   int      `json:"volUnit"`
	MinVol                    int      `json:"minVol"`
	MaxVol                    int      `json:"maxVol"`
	BidLimitPriceRate         float64  `json:"bidLimitPriceRate"`
	AskLimitPriceRate         float64  `json:"askLimitPriceRate"`
	TakerFeeRate              float64  `json:"takerFeeRate"`
	MakerFeeRate              float64  `json:"makerFeeRate"`
	MaintenanceMarginRate     float64  `json:"maintenanceMarginRate"`
	InitialMarginRate         float64  `json:"initialMarginRate"`
	RiskBaseVol               int      `json:"riskBaseVol"`
	RiskIncrVol               int      `json:"riskIncrVol"`
	RiskIncrMmr               float64  `json:"riskIncrMmr"`
	RiskIncrImr               float64  `json:"riskIncrImr"`
	RiskLevelLimit            int      `json:"riskLevelLimit"`
	PriceCoefficientVariation float64  `json:"priceCoefficientVariation"`
	IndexOrigin               []string `json:"indexOrigin"`
	State                     int      `json:"state"`
	IsNew                     bool     `json:"isNew"`
	IsHot                     bool     `json:"isHot"`
	IsHidden                  bool     `json:"isHidden"`
}

type mexcTickersRequest struct {
	Symbol string `json:"symbol,omitempty"`
}

type mexcTicker struct {
	Symbol    string  `json:"symbol"`
	LastPrice float64 `json:"lastPrice"`
	Bid1      float64 `json:"bid1"`
	Ask1      float64 `json:"ask1"`
	Volume24  float64 `json:"volume24"`
	Amount24  float64 `json:"amount24"`
	Timestamp int64   `json:"timestamp"`
}

type mexcFundingRateRequest struct {
	Symbol string `json:"symbol,omitempty"`
}

type mexcFundingRate struct {
	Symbol         string  `json:"symbol"`
	FundingRate    float64 `json:"fundingRate"`
	NextSettleTime int64   `json:"nextSettleTime"`
}

type mexcFundingRateHistoryRequest struct {
	Symbol   string `json:"symbol"`
	PageNum  string `json:"page_num"`
	PageSize string `json:"page_size"`
}

type mexcFundingRateHistory struct {
	Symbol      string  `json:"symbol"`
	FundingRate float64 `json:"fundingRate"`
	SettleTime  int64   `json:"settleTime"`
}

// Private raw methods invoking the MEXC API.

func (c *Client) rawPing(ctx context.Context) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/api/v1/contract/ping", nil, nil)
}

func (c *Client) getRawServerTime(ctx context.Context) (int64, error) {
	body, err := c.rawPing(ctx)
	if err != nil {
		return 0, err
	}
	return ParseResponse[int64](body, "server_time")
}

func (c *Client) getRawContractDetails(ctx context.Context) ([]mexcContractDetail, error) {
	body, err := c.RawRequest(ctx, http.MethodGet, "/api/v1/contract/detail", nil, nil)
	if err != nil {
		return nil, err
	}
	return ParseResponse[[]mexcContractDetail](body, "contract_details")
}

func (c *Client) getRawTickers(ctx context.Context, req mexcTickersRequest) ([]mexcTicker, error) {
	params := map[string]string{}
	if req.Symbol != "" {
		params[paramSymbol] = req.Symbol
	}

	body, err := c.GetTickersRaw(ctx, params)
	if err != nil {
		return nil, err
	}

	raw, err := ParseResponse[json.RawMessage](body, "ticker")
	if err != nil {
		return nil, err
	}

	var list []mexcTicker
	if err := json.Unmarshal(raw, &list); err == nil {
		return list, nil
	}

	var single mexcTicker
	if err := json.Unmarshal(raw, &single); err != nil {
		return nil, fmt.Errorf("parse ticker data: %w", err)
	}
	return []mexcTicker{single}, nil
}

func (c *Client) getRawFundingRate(ctx context.Context, req mexcFundingRateRequest) (*mexcFundingRate, error) {
	params := map[string]string{}
	if req.Symbol != "" {
		params[paramSymbol] = req.Symbol
	}
	body, err := c.GetFundingRateRaw(ctx, params)
	if err != nil {
		return nil, err
	}
	rawRates, err := ParseResponse[[]mexcFundingRate](body, "funding_rates")
	if err != nil {
		return nil, err
	}
	for i := range rawRates {
		if rawRates[i].Symbol == req.Symbol {
			return &rawRates[i], nil
		}
	}
	return nil, fmt.Errorf("mexc funding rate not found for symbol: %s", req.Symbol)
}

func (c *Client) getRawFundingRateHistory(ctx context.Context, req mexcFundingRateHistoryRequest) ([]mexcFundingRateHistory, error) {
	params := map[string]string{
		paramSymbol: req.Symbol,
		pageNumKey:  req.PageNum,
		pageSizeKey: req.PageSize,
	}
	body, err := c.RawRequest(ctx, http.MethodGet, "/api/v1/contract/funding_rate/history", params, nil)
	if err != nil {
		return nil, err
	}
	type resultWrapper struct {
		ResultList []mexcFundingRateHistory `json:"resultList"`
	}
	data, err := ParseResponse[resultWrapper](body, "funding_rate_history")
	if err != nil {
		return nil, err
	}
	return data.ResultList, nil
}

// Public mapper methods implementing the exchange.MarketDataProvider interface.

// Ping checks connectivity to the MEXC API server.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.rawPing(ctx)
	return err
}

// GetServerTime returns the MEXC server timestamp in milliseconds.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	return c.getRawServerTime(ctx)
}

// GetContractDetails returns all contract specifications.
func (c *Client) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	rawList, err := c.getRawContractDetails(ctx)
	if err != nil {
		return nil, err
	}

	details := make([]exchange.ContractDetail, 0, len(rawList))
	for i := range rawList {
		raw := &rawList[i]
		details = append(details, exchange.ContractDetail{
			Symbol:                    raw.Symbol,
			DisplayName:               raw.DisplayName,
			DisplayNameEn:             raw.DisplayNameEn,
			PositionOpenType:          raw.PositionOpenType,
			BaseCoin:                  raw.BaseCoin,
			QuoteCoin:                 raw.QuoteCoin,
			SettleCoin:                raw.SettleCoin,
			ContractSize:              raw.ContractSize,
			MinLeverage:               raw.MinLeverage,
			MaxLeverage:               raw.MaxLeverage,
			PriceScale:                raw.PriceScale,
			VolScale:                  raw.VolScale,
			AmountScale:               raw.AmountScale,
			PriceUnit:                 raw.PriceUnit,
			VolUnit:                   raw.VolUnit,
			MinVol:                    raw.MinVol,
			MaxVol:                    raw.MaxVol,
			BidLimitPriceRate:         raw.BidLimitPriceRate,
			AskLimitPriceRate:         raw.AskLimitPriceRate,
			TakerFeeRate:              raw.TakerFeeRate,
			MakerFeeRate:              raw.MakerFeeRate,
			MaintenanceMarginRate:     raw.MaintenanceMarginRate,
			InitialMarginRate:         raw.InitialMarginRate,
			RiskBaseVol:               raw.RiskBaseVol,
			RiskIncrVol:               raw.RiskIncrVol,
			RiskIncrMmr:               raw.RiskIncrMmr,
			RiskIncrImr:               raw.RiskIncrImr,
			RiskLevelLimit:            raw.RiskLevelLimit,
			PriceCoefficientVariation: raw.PriceCoefficientVariation,
			IndexOrigin:               raw.IndexOrigin,
			State:                     raw.State,
			IsNew:                     raw.IsNew,
			IsHot:                     raw.IsHot,
			IsHidden:                  raw.IsHidden,
		})
	}
	return details, nil
}

// GetFundingRates returns funding rates for specific symbols.
func (c *Client) GetFundingRates(ctx context.Context, symbols []string) ([]exchange.FundingRateResult, error) {
	if len(symbols) == 0 {
		return nil, nil
	}

	rates := make([]exchange.FundingRateResult, 0, len(symbols))
	for _, sym := range symbols {
		raw, err := c.getRawFundingRate(ctx, mexcFundingRateRequest{Symbol: sym})
		if err != nil {
			return nil, err
		}
		rates = append(rates, exchange.FundingRateResult{
			Symbol:     raw.Symbol,
			Rate:       raw.FundingRate,
			SettleTime: raw.NextSettleTime,
		})
	}
	return rates, nil
}

// GetTickers returns ticker data for all symbols, or a specific symbol.
func (c *Client) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	rawList, err := c.getRawTickers(ctx, mexcTickersRequest{Symbol: symbol})
	if err != nil {
		return nil, err
	}

	tickers := make([]exchange.Ticker, 0, len(rawList))
	for _, raw := range rawList {
		tickers = append(tickers, exchange.Ticker{
			Symbol:       raw.Symbol,
			LastPrice:    raw.LastPrice,
			Bid1:         raw.Bid1,
			Ask1:         raw.Ask1,
			Volume24:     raw.Volume24,
			AmountUSDT24: raw.Amount24,
			Timestamp:    raw.Timestamp,
		})
	}
	return tickers, nil
}

// GetFundingRateHistory returns funding rate history for a symbol.
func (c *Client) GetFundingRateHistory(ctx context.Context, symbol string, pageNum, pageSize int) ([]exchange.FundingRateHistory, error) {
	rawList, err := c.getRawFundingRateHistory(ctx, mexcFundingRateHistoryRequest{
		Symbol:   symbol,
		PageNum:  fmt.Sprintf("%d", pageNum),
		PageSize: fmt.Sprintf("%d", pageSize),
	})
	if err != nil {
		return nil, err
	}

	history := make([]exchange.FundingRateHistory, 0, len(rawList))
	for _, raw := range rawList {
		history = append(history, exchange.FundingRateHistory{
			Symbol:      raw.Symbol,
			FundingRate: raw.FundingRate,
			SettleTime:  raw.SettleTime,
		})
	}
	return history, nil
}

func filterMexcTickers(tickers []mexcTicker, minVol24h, maxVol24h float64, whitelistMap, blacklistMap map[string]bool) ([]string, map[string]float64) {
	var filteredSymbols []string
	volMap := make(map[string]float64)

	for i := range tickers {
		t := &tickers[i]
		if blacklistMap[t.Symbol] {
			continue
		}
		if len(whitelistMap) > 0 && !whitelistMap[t.Symbol] {
			continue
		}

		vol := t.Amount24
		if vol < minVol24h {
			continue
		}
		if maxVol24h > 0 && vol > maxVol24h {
			continue
		}

		filteredSymbols = append(filteredSymbols, t.Symbol)
		volMap[t.Symbol] = vol
	}
	return filteredSymbols, volMap
}

func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	// 1. Fetch all tickers
	tickers, err := c.getRawTickers(ctx, mexcTickersRequest{})
	if err != nil {
		return nil, fmt.Errorf("mexc list tickers: %w", err)
	}

	// 2. Build maps
	whitelistMap := make(map[string]bool)
	for _, sym := range whitelist {
		whitelistMap[sym] = true
	}

	blacklistMap := make(map[string]bool)
	for _, sym := range blacklist {
		blacklistMap[sym] = true
	}

	// 3. Filter symbols by whitelist, blacklist, and 24h volume
	filteredSymbols, volMap := filterMexcTickers(tickers, minVol24h, maxVol24h, whitelistMap, blacklistMap)
	if len(filteredSymbols) == 0 {
		return nil, nil
	}

	// 4. Fetch all funding rates in bulk
	body, err := c.GetFundingRateRaw(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("mexc list funding rates raw: %w", err)
	}

	rawRates, err := ParseResponse[[]mexcFundingRate](body, "funding_rates")
	if err != nil {
		return nil, fmt.Errorf("mexc parse funding rates: %w", err)
	}

	fundingMap := make(map[string]*mexcFundingRate)
	for i := range rawRates {
		fundingMap[rawRates[i].Symbol] = &rawRates[i]
	}

	// 5. Combine results
	var results []exchange.PotentialFundingResult
	for _, sym := range filteredSymbols {
		rateInfo, exists := fundingMap[sym]
		if !exists {
			continue
		}

		results = append(results, exchange.PotentialFundingResult{
			Symbol:     sym,
			Rate:       rateInfo.FundingRate,
			SettleTime: rateInfo.NextSettleTime,
			Volume24h:  volMap[sym],
		})
	}

	return results, nil
}
