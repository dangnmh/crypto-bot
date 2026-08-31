package futures

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/mexc"
	"crypto-bot/pkg/xjson"
)

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

type mexcFundingRate struct {
	Symbol         string  `json:"symbol"`
	FundingRate    float64 `json:"fundingRate"`
	NextSettleTime int64   `json:"nextSettleTime"`
}

func (c *Client) getRawContractDetails(ctx context.Context, symbol string) ([]mexcContractDetail, error) {
	params := map[string]any{}
	if symbol != "" {
		params["symbol"] = symbol
	}
	body, err := c.base.Request(ctx, http.MethodGet, "/api/v1/contract/detail", params, nil, false)
	if err != nil {
		return nil, err
	}
	raw, err := mexc.ParseFuturesResponse[json.RawMessage](body)
	if err != nil {
		return nil, err
	}

	var list []mexcContractDetail
	if err := xjson.Unmarshal(raw.Data, &list); err == nil {
		return list, nil
	}

	var single mexcContractDetail
	if err := xjson.Unmarshal(raw.Data, &single); err != nil {
		return nil, fmt.Errorf("parse contract details: %w", err)
	}
	return []mexcContractDetail{single}, nil
}

func (c *Client) getRawTickers(ctx context.Context, req mexcTickersRequest) ([]mexcTicker, error) {
	params := map[string]any{}
	if req.Symbol != "" {
		params["symbol"] = req.Symbol
	}

	body, err := c.base.Request(ctx, http.MethodGet, "/api/v1/contract/ticker", params, nil, false)
	if err != nil {
		return nil, err
	}

	raw, err := mexc.ParseFuturesResponse[json.RawMessage](body)
	if err != nil {
		return nil, err
	}

	var list []mexcTicker
	if err := xjson.Unmarshal(raw.Data, &list); err == nil {
		return list, nil
	}

	var single mexcTicker
	if err := xjson.Unmarshal(raw.Data, &single); err != nil {
		return nil, fmt.Errorf("parse ticker data: %w", err)
	}
	return []mexcTicker{single}, nil
}

func (c *Client) getRawFundingRates(ctx context.Context) ([]mexcFundingRate, error) {
	body, err := c.base.Request(ctx, http.MethodGet, "/api/v1/contract/funding_rate", nil, nil, false)
	if err != nil {
		return nil, err
	}
	rawRates, err := mexc.ParseFuturesResponse[[]mexcFundingRate](body)
	if err != nil {
		return nil, err
	}
	return rawRates.Data, nil
}

// GetContractDetails returns all contract specifications.
func (c *Client) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	rawList, err := c.getRawContractDetails(ctx, "")
	if err != nil {
		return nil, err
	}

	details := make([]exchange.ContractDetail, 0, len(rawList))
	for i := range rawList {
		raw := &rawList[i]
		var parsedRiskLimits []exchange.RiskLimitTier
		isByValue := strings.EqualFold(raw.RiskLimitType, "BY_VALUE")
		for _, tier := range raw.RiskLimitCustom {
			rTier := exchange.RiskLimitTier{
				Level:       tier.Level,
				MaxLeverage: tier.MaxLeverage,
			}
			if isByValue {
				rTier.MaxNotional = tier.MaxVol
			} else {
				rTier.MaxQuantity = tier.MaxVol
			}
			parsedRiskLimits = append(parsedRiskLimits, rTier)
		}

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
			RiskLimits:                parsedRiskLimits,
		})
	}
	return details, nil
}

// GetFundingRates returns funding rates for specific symbols.
func (c *Client) GetFundingRates(ctx context.Context, symbols []string) ([]exchange.FundingRateResult, error) {
	if len(symbols) == 0 {
		return nil, nil
	}

	rawRates, err := c.getRawFundingRates(ctx)
	if err != nil {
		return nil, err
	}

	ratesMap := make(map[string]mexcFundingRate, len(rawRates))
	for i := range rawRates {
		ratesMap[rawRates[i].Symbol] = rawRates[i]
	}

	rates := make([]exchange.FundingRateResult, 0, len(symbols))
	for _, sym := range symbols {
		if raw, ok := ratesMap[sym]; ok {
			rates = append(rates, exchange.FundingRateResult{
				Symbol:     raw.Symbol,
				Rate:       raw.FundingRate,
				SettleTime: raw.NextSettleTime,
			})
		}
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

// GetTopGainer returns top gaining contracts ranked by 24h rise rate.
func (c *Client) GetTopGainer(ctx context.Context, req exchange.TopGainerRequest) ([]exchange.TopGainerResult, error) {
	rawList, err := c.getRawTickers(ctx, mexcTickersRequest{})
	if err != nil {
		return nil, fmt.Errorf("mexc get top gainer tickers: %w", err)
	}

	results := make([]exchange.TopGainerResult, 0, len(rawList))
	for i := range rawList {
		raw := &rawList[i]
		if !strings.HasSuffix(raw.Symbol, "_USDT") && !strings.HasSuffix(raw.Symbol, "_USDC") {
			continue
		}
		if raw.LastPrice <= 0 {
			continue
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
			Volume24hUSDT: raw.Amount24,
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

func matchFundingFilters(symbol string, amtUSDT, minVol24h, maxVol24h float64, whiteMap, blackMap map[string]bool) bool {
	if !strings.HasSuffix(symbol, "_USDT") && !strings.HasSuffix(symbol, "_USDC") {
		return false
	}
	if blackMap[symbol] {
		return false
	}
	if len(whiteMap) > 0 && !whiteMap[symbol] {
		return false
	}
	if amtUSDT < minVol24h {
		return false
	}
	if maxVol24h > 0 && amtUSDT > maxVol24h {
		return false
	}
	return true
}

// GetPotentialFundingSymbols scans symbols meeting volume and whitelist/blacklist criteria.
func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist, blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	rawList, err := c.getRawTickers(ctx, mexcTickersRequest{})
	if err != nil {
		return nil, fmt.Errorf("mexc list tickers: %w", err)
	}

	rawRates, err := c.getRawFundingRates(ctx)
	if err != nil {
		return nil, fmt.Errorf("mexc list funding rates: %w", err)
	}

	rateMap := make(map[string]mexcFundingRate, len(rawRates))
	for i := range rawRates {
		rateMap[rawRates[i].Symbol] = rawRates[i]
	}

	whiteMap := make(map[string]bool, len(whitelist))
	for _, s := range whitelist {
		whiteMap[s] = true
	}
	blackMap := make(map[string]bool, len(blacklist))
	for _, s := range blacklist {
		blackMap[s] = true
	}

	var results []exchange.PotentialFundingResult
	for i := range rawList {
		t := &rawList[i]
		amtUSDT := t.Amount24
		if amtUSDT == 0 && t.Volume24 > 0 && t.LastPrice > 0 {
			amtUSDT = t.Volume24 * t.LastPrice
		}
		if !matchFundingFilters(t.Symbol, amtUSDT, minVol24h, maxVol24h, whiteMap, blackMap) {
			continue
		}

		settleTime := int64(0)
		fundingRate := t.FundingRate
		if fr, ok := rateMap[t.Symbol]; ok {
			settleTime = fr.NextSettleTime
			if fr.FundingRate != 0 {
				fundingRate = fr.FundingRate
			}
		}

		results = append(results, exchange.PotentialFundingResult{
			Symbol:     t.Symbol,
			Rate:       fundingRate,
			SettleTime: settleTime,
			Volume24h:  amtUSDT,
			Price:      t.LastPrice,
		})
	}

	return results, nil
}

// GetDepth retrieves depth orderbook snapshot via REST.
func (c *Client) GetDepth(ctx context.Context, symbol string) (*domain.OrderBook, error) {
	path := fmt.Sprintf("/api/v1/contract/depth/%s", symbol)
	params := map[string]any{"limit": 100}
	body, err := c.base.Request(ctx, http.MethodGet, path, params, nil, false)
	if err != nil {
		return nil, fmt.Errorf("mexc get depth for %s: %w", symbol, err)
	}

	type mexcDepthData struct {
		Asks    [][]xjson.Number `json:"asks"`
		Bids    [][]xjson.Number `json:"bids"`
		Version int64            `json:"version"`
	}

	resp, err := mexc.ParseFuturesResponse[mexcDepthData](body)
	if err != nil {
		return nil, fmt.Errorf("mexc parse depth for %s: %w", symbol, err)
	}

	bids := make([]domain.OrderBookEntry, 0, len(resp.Data.Bids))
	for _, b := range resp.Data.Bids {
		if len(b) >= 2 {
			p, v := xjson.ToFloat64(b[0]), xjson.ToFloat64(b[1])
			if len(b) >= 3 {
				v = xjson.ToFloat64(b[2])
			}
			if p > 0 && v > 0 {
				bids = append(bids, domain.OrderBookEntry{Price: p, Volume: v})
			}
		}
	}

	asks := make([]domain.OrderBookEntry, 0, len(resp.Data.Asks))
	for _, a := range resp.Data.Asks {
		if len(a) >= 2 {
			p, v := xjson.ToFloat64(a[0]), xjson.ToFloat64(a[1])
			if len(a) >= 3 {
				v = xjson.ToFloat64(a[2])
			}
			if p > 0 && v > 0 {
				asks = append(asks, domain.OrderBookEntry{Price: p, Volume: v})
			}
		}
	}

	return &domain.OrderBook{
		Symbol:  symbol,
		Version: resp.Data.Version,
		Bids:    bids,
		Asks:    asks,
	}, nil
}

// GetDepthCommits retrieves orderbook depth commit snapshots.
func (c *Client) GetDepthCommits(ctx context.Context, symbol string, limit int) ([]exchange.DepthCommit, error) {
	path := fmt.Sprintf("/api/v1/contract/depth_commits/%s/%d", symbol, limit)
	body, err := c.base.Request(ctx, http.MethodGet, path, nil, nil, false)
	if err != nil {
		return nil, fmt.Errorf("mexc get depth commits for %s: %w", symbol, err)
	}

	type depthCommitData struct {
		Asks         [][]xjson.Number `json:"asks"`
		Bids         [][]xjson.Number `json:"bids"`
		Version      int64            `json:"version"`
		FirstVersion int64            `json:"firstVersion"`
	}

	resp, err := mexc.ParseFuturesResponse[depthCommitData](body)
	if err != nil {
		return nil, fmt.Errorf("mexc parse depth commits for %s: %w", symbol, err)
	}

	bids := make([]domain.OrderBookEntry, 0, len(resp.Data.Bids))
	for _, b := range resp.Data.Bids {
		if len(b) >= 2 {
			p, v := xjson.ToFloat64(b[0]), xjson.ToFloat64(b[1])
			if len(b) >= 3 {
				v = xjson.ToFloat64(b[2])
			}
			if p > 0 && v > 0 {
				bids = append(bids, domain.OrderBookEntry{Price: p, Volume: v})
			}
		}
	}

	asks := make([]domain.OrderBookEntry, 0, len(resp.Data.Asks))
	for _, a := range resp.Data.Asks {
		if len(a) >= 2 {
			p, v := xjson.ToFloat64(a[0]), xjson.ToFloat64(a[1])
			if len(a) >= 3 {
				v = xjson.ToFloat64(a[2])
			}
			if p > 0 && v > 0 {
				asks = append(asks, domain.OrderBookEntry{Price: p, Volume: v})
			}
		}
	}

	return []exchange.DepthCommit{
		{
			Version: resp.Data.Version,
			Bids:    bids,
			Asks:    asks,
		},
	}, nil
}

// FetchKlines fetches historical klines for a futures symbol.
func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
	path := fmt.Sprintf("/api/v1/contract/kline/%s", symbol)
	params := map[string]any{
		"interval": mapMexcInterval(interval),
		"start":    strconv.FormatInt(start.Unix(), 10),
		"end":      strconv.FormatInt(end.Unix(), 10),
	}
	body, err := c.base.Request(ctx, http.MethodGet, path, params, nil, false)
	if err != nil {
		return nil, err
	}

	type klineResponse struct {
		Time   []int64   `json:"time"`
		Open   []float64 `json:"open"`
		Close  []float64 `json:"close"`
		High   []float64 `json:"high"`
		Low    []float64 `json:"low"`
		Vol    []float64 `json:"vol"`
		Amount []float64 `json:"amount"`
	}

	resp, err := mexc.ParseFuturesResponse[klineResponse](body)
	if err != nil {
		return nil, err
	}

	data := resp.Data
	n := len(data.Time)
	klines := make([]exchange.Kline, 0, n)
	for i := range n {
		klines = append(klines, exchange.Kline{
			Timestamp: data.Time[i] * 1000,
			Open:      data.Open[i],
			High:      data.High[i],
			Low:       data.Low[i],
			Close:     data.Close[i],
			Volume:    data.Vol[i],
			Amount:    data.Amount[i],
		})
	}
	return klines, nil
}

const intervalMin1 = "Min1"

func mapMexcInterval(interval exchange.Interval) string {
	switch interval {
	case exchange.Interval1m:
		return intervalMin1
	case exchange.Interval5m:
		return "Min5"
	case exchange.Interval15m:
		return "Min15"
	case exchange.Interval30m:
		return "Min30"
	case exchange.Interval1h:
		return "Min60"
	case exchange.Interval4h:
		return "Hour4"
	case exchange.Interval8h:
		return "Hour8"
	case exchange.Interval1d:
		return "Day1"
	case exchange.Interval1w:
		return "Week1"
	case exchange.Interval1M:
		return "Month1"
	default:
		return intervalMin1
	}
}
