package mexc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/exchange"

	"crypto-bot/pkg/xjson"
)

// Explicit request/response structs for market data endpoints.

type mexcContractDetail struct {
	Symbol                    string              `json:"symbol"`
	DisplayName               string              `json:"displayName"`
	DisplayNameEn             string              `json:"displayNameEn"`
	PositionOpenType          int                 `json:"positionOpenType"`
	BaseCoin                  string              `json:"baseCoin"`
	QuoteCoin                 string              `json:"quoteCoin"`
	SettleCoin                string              `json:"settleCoin"`
	ContractSize              float64             `json:"contractSize"`
	MinLeverage               int                 `json:"minLeverage"`
	MaxLeverage               int                 `json:"maxLeverage"`
	PriceScale                int                 `json:"priceScale"`
	VolScale                  int                 `json:"volScale"`
	AmountScale               int                 `json:"amountScale"`
	PriceUnit                 float64             `json:"priceUnit"`
	VolUnit                   int                 `json:"volUnit"`
	MinVol                    int                 `json:"minVol"`
	MaxVol                    int                 `json:"maxVol"`
	BidLimitPriceRate         float64             `json:"bidLimitPriceRate"`
	AskLimitPriceRate         float64             `json:"askLimitPriceRate"`
	TakerFeeRate              float64             `json:"takerFeeRate"`
	MakerFeeRate              float64             `json:"makerFeeRate"`
	MaintenanceMarginRate     float64             `json:"maintenanceMarginRate"`
	InitialMarginRate         float64             `json:"initialMarginRate"`
	RiskBaseVol               int                 `json:"riskBaseVol"`
	RiskIncrVol               int                 `json:"riskIncrVol"`
	RiskIncrMmr               float64             `json:"riskIncrMmr"`
	RiskIncrImr               float64             `json:"riskIncrImr"`
	RiskLevelLimit            int                 `json:"riskLevelLimit"`
	PriceCoefficientVariation float64             `json:"priceCoefficientVariation"`
	IndexOrigin               []string            `json:"indexOrigin"`
	State                     int                 `json:"state"`
	IsNew                     bool                `json:"isNew"`
	IsHot                     bool                `json:"isHot"`
	IsHidden                  bool                `json:"isHidden"`
	RiskLimitType             string              `json:"riskLimitType"`
	RiskLimitMode             string              `json:"riskLimitMode"`
	RiskLimitCustom           []mexcRiskLimitTier `json:"riskLimitCustom"`
}

type mexcRiskLimitTier struct {
	Level       int     `json:"level"`
	MaxVol      float64 `json:"maxVol"`
	MMR         float64 `json:"mmr"`
	IMR         float64 `json:"imr"`
	MaxLeverage int     `json:"maxLeverage"`
}

type mexcTickersRequest struct {
	Symbol string `json:"symbol,omitempty"`
}

type mexcTicker struct {
	ContractID    int     `json:"contractId"`
	Symbol        string  `json:"symbol"`
	LastPrice     float64 `json:"lastPrice"`
	Bid1          float64 `json:"bid1"`
	Ask1          float64 `json:"ask1"`
	Volume24      float64 `json:"volume24"`
	Amount24      float64 `json:"amount24"`
	HoldVol       float64 `json:"holdVol"`
	Lower24Price  float64 `json:"lower24Price"`
	High24Price   float64 `json:"high24Price"`
	RiseFallRate  float64 `json:"riseFallRate"`
	RiseFallValue float64 `json:"riseFallValue"`
	IndexPrice    float64 `json:"indexPrice"`
	FairPrice     float64 `json:"fairPrice"`
	FundingRate   float64 `json:"fundingRate"`
	MaxBidPrice   float64 `json:"maxBidPrice"`
	MinAskPrice   float64 `json:"minAskPrice"`
	Timestamp     int64   `json:"timestamp"`
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

func (c *Client) getRawContractDetails(ctx context.Context, symbol string) ([]mexcContractDetail, error) {
	params := map[string]string{}
	if symbol != "" {
		params[paramSymbol] = symbol
	}
	body, err := c.RawRequest(ctx, http.MethodGet, "/api/v1/contract/detail", params, nil)
	if err != nil {
		return nil, err
	}
	raw, err := ParseResponse[json.RawMessage](body, "contract_details")
	if err != nil {
		return nil, err
	}

	var list []mexcContractDetail
	if err := xjson.Unmarshal(raw, &list); err == nil {
		return list, nil
	}

	var single mexcContractDetail
	if err := xjson.Unmarshal(raw, &single); err != nil {
		return nil, fmt.Errorf("parse contract details: %w", err)
	}
	return []mexcContractDetail{single}, nil
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
	if err := xjson.Unmarshal(raw, &list); err == nil {
		return list, nil
	}

	var single mexcTicker
	if err := xjson.Unmarshal(raw, &single); err != nil {
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

// GetContractDetails returns all contract specifications.
func (c *Client) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	rawList, err := c.getRawContractDetails(ctx, "")
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
	for i := range rawList {
		raw := &rawList[i]
		amtUSDT := raw.Amount24
		if amtUSDT == 0 && raw.Volume24 > 0 && raw.LastPrice > 0 {
			amtUSDT = raw.Volume24 * raw.LastPrice
		}
		tickers = append(tickers, exchange.Ticker{
			Symbol:       raw.Symbol,
			LastPrice:    raw.LastPrice,
			Bid1:         raw.Bid1,
			Ask1:         raw.Ask1,
			Volume24:     raw.Volume24,
			AmountUSDT24: amtUSDT,
			Timestamp:    raw.Timestamp,
		})
	}
	return tickers, nil
}

// GetTopGainer returns tickers sorted by 24h price change percentage descending.
func (c *Client) GetTopGainer(ctx context.Context, req exchange.TopGainerRequest) ([]exchange.TopGainerResult, error) {
	rawList, err := c.getRawTickers(ctx, mexcTickersRequest{})
	if err != nil {
		return nil, fmt.Errorf("mexc get top gainer: %w", err)
	}

	results := make([]exchange.TopGainerResult, 0, len(rawList))
	for i := range rawList {
		raw := &rawList[i]
		volUSDT := raw.Amount24
		if volUSDT == 0 {
			volUSDT = raw.Volume24 * raw.LastPrice
		}
		spreadPct := 0.0
		if raw.Bid1 > 0 && raw.Ask1 > 0 {
			spreadPct = ((raw.Ask1 - raw.Bid1) / raw.Bid1) * 100.0
		}
		results = append(results, exchange.TopGainerResult{
			Symbol:        raw.Symbol,
			LastPrice:     raw.LastPrice,
			Bid1:          raw.Bid1,
			Ask1:          raw.Ask1,
			Volume24hUSDT: volUSDT,
			Gain24hPct:    raw.RiseFallRate * 100.0,
			SpreadPct:     spreadPct,
			Timestamp:     raw.Timestamp,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Gain24hPct > results[j].Gain24hPct
	})

	if req.Limit > 0 && req.Limit < len(results) {
		results = results[:req.Limit]
	}

	return results, nil
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

func filterMexcTickers(tickers []mexcTicker, minVol24h, maxVol24h float64, whitelistMap, blacklistMap map[string]bool) ([]string, map[string]float64, map[string]float64) {
	var filteredSymbols []string
	volMap := make(map[string]float64)
	priceMap := make(map[string]float64)

	for i := range tickers {
		t := &tickers[i]
		if blacklistMap[t.Symbol] {
			continue
		}
		if len(whitelistMap) > 0 && !whitelistMap[t.Symbol] {
			continue
		}

		vol := t.Amount24
		if vol == 0 && t.Volume24 > 0 && t.LastPrice > 0 {
			vol = t.Volume24 * t.LastPrice
		}
		if vol < minVol24h {
			continue
		}
		if maxVol24h > 0 && vol > maxVol24h {
			continue
		}

		filteredSymbols = append(filteredSymbols, t.Symbol)
		volMap[t.Symbol] = vol
		priceMap[t.Symbol] = t.LastPrice
	}
	return filteredSymbols, volMap, priceMap
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
	filteredSymbols, volMap, priceMap := filterMexcTickers(tickers, minVol24h, maxVol24h, whitelistMap, blacklistMap)
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
			Price:      priceMap[sym],
		})
	}

	return results, nil
}

// FetchKlines fetches public K-lines for Mexc.

//nolint:cyclop,goconst // Switch statements mapping intervals are naturally complex but easy to read
func mapMexcInterval(interval exchange.Interval) string {
	switch interval {
	case exchange.Interval1m:
		return "Min1"
	case exchange.Interval3m:
		return "Min3"
	case exchange.Interval5m:
		return "Min5"
	case exchange.Interval15m:
		return "Min15"
	case exchange.Interval30m:
		return "Min30"
	case exchange.Interval1h:
		return "Hour1"
	case exchange.Interval2h:
		return "Hour2"
	case exchange.Interval4h:
		return "Hour4"
	case exchange.Interval6h:
		return "Hour6"
	case exchange.Interval8h:
		return "Hour8"
	case exchange.Interval12h:
		return "Hour12"
	case exchange.Interval1d:
		return "Day1"
	case exchange.Interval1w:
		return "Week1"
	case exchange.Interval1M:
		return "Month1"
	default:
		return "Min1"
	}
}

func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
	path := fmt.Sprintf("/api/v1/contract/kline/%s", symbol)
	params := map[string]string{
		"interval": mapMexcInterval(interval),
		"start":    strconv.FormatInt(start.Unix(), 10),
		"end":      strconv.FormatInt(end.Unix(), 10),
	}
	body, err := c.RawRequest(ctx, http.MethodGet, path, params, nil)
	if err != nil {
		return nil, fmt.Errorf("mexc fetch klines: %w", err)
	}

	type klineResp struct {
		Time  []int64   `json:"time"`
		Close []float64 `json:"close"`
		Open  []float64 `json:"open"`
		High  []float64 `json:"high"`
		Low   []float64 `json:"low"`
		Vol   []float64 `json:"vol"`
	}

	res, err := ParseResponse[klineResp](body, "mexc fetch klines")
	if err != nil {
		return nil, err
	}

	var klines []exchange.Kline
	limit := min(len(res.Close), len(res.Time))
	for i := range limit {
		var open, high, low, vol float64
		if i < len(res.Open) {
			open = res.Open[i]
		}
		if i < len(res.High) {
			high = res.High[i]
		}
		if i < len(res.Low) {
			low = res.Low[i]
		}
		if i < len(res.Vol) {
			vol = res.Vol[i]
		}
		klines = append(klines, exchange.Kline{
			Timestamp: res.Time[i] * 1000,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     res.Close[i],
			Volume:    vol,
		})
	}
	return klines, nil
}

var _ exchange.RiskLimitLeverageProvider = (*Client)(nil)

func findTargetDetail(rawList []mexcContractDetail, symbol string) (*mexcContractDetail, error) {
	for i := range rawList {
		if rawList[i].Symbol == symbol {
			return &rawList[i], nil
		}
	}
	if len(rawList) > 0 {
		return &rawList[0], nil
	}
	return nil, fmt.Errorf("mexc contract detail not found for symbol %s", symbol)
}

func (c *Client) computeTargetCompare(ctx context.Context, symbol string, value float64, detail *mexcContractDetail) float64 {
	if strings.EqualFold(detail.RiskLimitType, "BY_VALUE") {
		return value
	}

	var lastPrice float64
	if tickers, err := c.GetTickers(ctx, symbol); err == nil && len(tickers) > 0 {
		lastPrice = tickers[0].LastPrice
	}

	switch {
	case lastPrice > 0 && detail.ContractSize > 0:
		return value / (detail.ContractSize * lastPrice)
	case detail.ContractSize > 0:
		return value / detail.ContractSize
	default:
		return value
	}
}

// GetMaxLeverageForValue implements exchange.RiskLimitLeverageProvider.
// It queries risk limits for the specified symbol and returns the maximum leverage allowed for a target position value in USDT.
func (c *Client) GetMaxLeverageForValue(ctx context.Context, symbol string, value float64) (int, error) {
	rawList, err := c.getRawContractDetails(ctx, symbol)
	if err != nil {
		return 0, fmt.Errorf("mexc get contract detail for %s: %w", symbol, err)
	}

	targetDetail, err := findTargetDetail(rawList, symbol)
	if err != nil {
		return 0, err
	}

	if len(targetDetail.RiskLimitCustom) == 0 {
		if targetDetail.MaxLeverage > 0 {
			return targetDetail.MaxLeverage, nil
		}
		return 0, fmt.Errorf("mexc no risk limit tiers or max leverage for symbol %s", symbol)
	}

	sort.Slice(targetDetail.RiskLimitCustom, func(i, j int) bool {
		return targetDetail.RiskLimitCustom[i].Level < targetDetail.RiskLimitCustom[j].Level
	})

	targetCompare := c.computeTargetCompare(ctx, symbol, value, targetDetail)

	for _, tier := range targetDetail.RiskLimitCustom {
		if targetCompare <= tier.MaxVol && tier.MaxLeverage > 0 {
			return tier.MaxLeverage, nil
		}
	}

	lastTier := targetDetail.RiskLimitCustom[len(targetDetail.RiskLimitCustom)-1]
	if lastTier.MaxLeverage > 0 {
		return lastTier.MaxLeverage, nil
	}

	if targetDetail.MaxLeverage > 0 {
		return targetDetail.MaxLeverage, nil
	}

	return 0, fmt.Errorf("mexc could not determine max leverage for symbol %s value %f", symbol, value)
}
