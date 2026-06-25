package deepcoin

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"

	"crypto-bot/pkg/xjson"
)

const (
	paramInstType = "instType"
	instTypeSwap  = "SWAP"
	instTypeSwapU = "SwapU"
)

// Raw request/response models for market endpoints.

type deepcoinServerTimeResponse struct {
	Ts xjson.Number `json:"ts"`
}

type deepcoinInstrument struct {
	InstID    string `json:"instId"`
	BaseCcy   string `json:"baseCcy"`
	SettleCcy string `json:"settleCcy"`
	CtVal     string `json:"ctVal"`
	Lever     string `json:"lever"`
	TickSz    string `json:"tickSz"`
	LotSz     string `json:"lotSz"`
	MinSz     string `json:"minSz"`
	State     string `json:"state"`
}

type deepcoinTicker struct {
	InstID    string `json:"instId"`
	Last      string `json:"last"`
	BidPx     string `json:"bidPx"`
	AskPx     string `json:"askPx"`
	Vol24h    string `json:"vol24h"`
	VolCcy24h string `json:"volCcy24h"`
	Ts        string `json:"ts"`
}

type deepcoinFundingRateCycle struct {
	SettleInterval int64  `json:"settleInterval"`
	InstrumentID   string `json:"instrumentID"`
	NextSettleTime int64  `json:"nextSettleTime"`
}

type deepcoinCurrentFundingRate struct {
	InstrumentID string  `json:"instrumentId"`
	FundingRate  float64 `json:"fundingRate"`
}

type deepcoinCurrentFundingRatesData struct {
	CurrentFundRates []deepcoinCurrentFundingRate `json:"current_fund_rates"`
}

// REST endpoints implementation.

func (c *Client) getRawServerTime(ctx context.Context) (*deepcoinServerTimeResponse, error) {
	body, err := c.RawRequest(ctx, http.MethodGet, "/deepcoin/market/time", nil, nil)
	if err != nil {
		return nil, err
	}
	res, err := ParseResponseFirst[deepcoinServerTimeResponse](body, "server_time")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *Client) getRawContractDetails(ctx context.Context) ([]deepcoinInstrument, error) {
	params := map[string]string{
		paramInstType: instTypeSwap,
	}
	body, err := c.RawRequest(ctx, http.MethodGet, "/deepcoin/market/instruments", params, nil)
	if err != nil {
		return nil, err
	}
	return ParseResponse[deepcoinInstrument](body, "contract_details")
}

func (c *Client) getRawTickers(ctx context.Context, symbol string) ([]deepcoinTicker, error) {
	params := map[string]string{
		paramInstType: instTypeSwap,
	}
	if symbol != "" {
		params["instId"] = symbol
	}
	body, err := c.RawRequest(ctx, http.MethodGet, "/deepcoin/market/tickers", params, nil)
	if err != nil {
		return nil, err
	}
	return ParseResponse[deepcoinTicker](body, "tickers")
}

func (c *Client) getRawFundingRateCycles(ctx context.Context) ([]deepcoinFundingRateCycle, error) {
	params := map[string]string{
		paramInstType: instTypeSwapU,
	}
	body, err := c.RawRequest(ctx, http.MethodGet, "/deepcoin/trade/funding-rate", params, nil)
	if err != nil {
		return nil, err
	}
	return ParseResponse[deepcoinFundingRateCycle](body, "funding_rate_cycles")
}

func (c *Client) getRawCurrentFundingRates(ctx context.Context) (*deepcoinCurrentFundingRatesData, error) {
	params := map[string]string{
		paramInstType: instTypeSwapU,
	}
	body, err := c.RawRequest(ctx, http.MethodGet, "/deepcoin/trade/fund-rate/current-funding-rate", params, nil)
	if err != nil {
		return nil, err
	}
	res, err := ParseResponseFirst[deepcoinCurrentFundingRatesData](body, "current_funding_rates")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

// Public MarketDataProvider methods.

func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	res, err := c.getRawServerTime(ctx)
	if err != nil {
		return 0, err
	}
	val, err := strconv.ParseInt(string(res.Ts), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse server time: %w", err)
	}
	return val, nil
}

func (c *Client) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	instruments, err := c.getRawContractDetails(ctx)
	if err != nil {
		return nil, err
	}

	details := make([]exchange.ContractDetail, 0, len(instruments))
	for i := range instruments {
		inst := &instruments[i]
		ctVal, _ := strconv.ParseFloat(inst.CtVal, 64)
		lever, _ := strconv.Atoi(inst.Lever)
		priceUnit, _ := strconv.ParseFloat(inst.TickSz, 64)

		stateVal := 0
		if inst.State == stateLive {
			stateVal = 1
		}

		priceScale := decmath.DecimalPlaces(inst.TickSz)
		volScale := decmath.DecimalPlaces(inst.LotSz)

		details = append(details, exchange.ContractDetail{
			Symbol:           inst.InstID,
			DisplayName:      inst.InstID,
			DisplayNameEn:    inst.InstID,
			PositionOpenType: 1, // Isolated
			BaseCoin:         inst.BaseCcy,
			QuoteCoin:        inst.SettleCcy,
			SettleCoin:       inst.SettleCcy,
			ContractSize:     ctVal,
			MinLeverage:      1,
			MaxLeverage:      lever,
			PriceScale:       priceScale,
			VolScale:         volScale,
			PriceUnit:        priceUnit,
			MinVol:           1,
			State:            stateVal,
		})
	}
	return details, nil
}

func (c *Client) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	rawTickers, err := c.getRawTickers(ctx, symbol)
	if err != nil {
		return nil, err
	}

	exchangeTickers := make([]exchange.Ticker, 0, len(rawTickers))
	for i := range rawTickers {
		t := &rawTickers[i]
		last, _ := strconv.ParseFloat(t.Last, 64)
		bid, _ := strconv.ParseFloat(t.BidPx, 64)
		ask, _ := strconv.ParseFloat(t.AskPx, 64)
		ts, _ := strconv.ParseInt(t.Ts, 10, 64)
		baseVol, _ := strconv.ParseFloat(t.Vol24h, 64)
		quoteVol, _ := strconv.ParseFloat(t.VolCcy24h, 64)

		exchangeTickers = append(exchangeTickers, exchange.Ticker{
			Symbol:       t.InstID,
			LastPrice:    last,
			Bid1:         bid,
			Ask1:         ask,
			Volume24:     baseVol,
			AmountUSDT24: quoteVol,
			Timestamp:    ts,
		})
	}
	return exchangeTickers, nil
}

func (c *Client) GetFundingRates(ctx context.Context, symbols []string) ([]exchange.FundingRateResult, error) {
	if len(symbols) == 0 {
		return nil, nil
	}

	// 1. Fetch settlement cycles
	cycles, err := c.getRawFundingRateCycles(ctx)
	if err != nil {
		return nil, err
	}

	// 2. Fetch current funding rates
	ratesData, err := c.getRawCurrentFundingRates(ctx)
	if err != nil {
		return nil, err
	}

	// Build lookup maps using normalized symbol names to avoid formatting issues
	cycleMap := make(map[string]int64)
	for _, cycle := range cycles {
		norm := normalizeSymbol(cycle.InstrumentID)
		cycleMap[norm] = cycle.NextSettleTime * 1000 // Convert seconds to milliseconds
	}

	rateMap := make(map[string]float64)
	for _, rate := range ratesData.CurrentFundRates {
		norm := normalizeSymbol(rate.InstrumentID)
		rateMap[norm] = rate.FundingRate
	}

	results := make([]exchange.FundingRateResult, 0, len(symbols))
	for _, sym := range symbols {
		norm := normalizeSymbol(sym)
		settleTime := cycleMap[norm]
		rate := rateMap[norm]

		results = append(results, exchange.FundingRateResult{
			Symbol:     sym,
			Rate:       rate,
			SettleTime: settleTime,
		})
	}

	return results, nil
}

func normalizeSymbol(s string) string {
	upper := strings.ToUpper(s)
	upper = strings.ReplaceAll(upper, "-", "")
	upper = strings.ReplaceAll(upper, "_", "")
	upper = strings.TrimSuffix(upper, "SWAP")
	return upper
}

func filterTickers(tickers []deepcoinTicker, minVol24h, maxVol24h float64, whitelistMap, blacklistMap map[string]bool) ([]deepcoinTicker, map[string]float64, map[string]float64) {
	var filteredTickers []deepcoinTicker
	volMap := make(map[string]float64)
	priceMap := make(map[string]float64)

	for i := range tickers {
		t := &tickers[i]
		norm := normalizeSymbol(t.InstID)
		if blacklistMap[norm] {
			continue
		}
		if len(whitelistMap) > 0 && !whitelistMap[norm] {
			continue
		}

		vol, _ := strconv.ParseFloat(t.VolCcy24h, 64)
		if vol < minVol24h {
			continue
		}
		if maxVol24h > 0 && vol > maxVol24h {
			continue
		}

		filteredTickers = append(filteredTickers, *t)
		volMap[norm] = vol
		price, _ := strconv.ParseFloat(t.Last, 64)
		priceMap[norm] = price
	}
	return filteredTickers, volMap, priceMap
}

func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	// 1. Fetch all tickers
	tickers, err := c.getRawTickers(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("deepcoin list tickers: %w", err)
	}

	// 2. Build maps
	whitelistMap := make(map[string]bool)
	for _, sym := range whitelist {
		whitelistMap[normalizeSymbol(sym)] = true
	}

	blacklistMap := make(map[string]bool)
	for _, sym := range blacklist {
		blacklistMap[normalizeSymbol(sym)] = true
	}

	// 3. Filter symbols by whitelist, blacklist, and 24h volume
	filteredTickers, volMap, priceMap := filterTickers(tickers, minVol24h, maxVol24h, whitelistMap, blacklistMap)
	if len(filteredTickers) == 0 {
		return nil, nil
	}

	// 4. Fetch settlement cycles
	cycles, err := c.getRawFundingRateCycles(ctx)
	if err != nil {
		return nil, fmt.Errorf("deepcoin get funding rate cycles: %w", err)
	}

	// 5. Fetch current funding rates
	ratesData, err := c.getRawCurrentFundingRates(ctx)
	if err != nil {
		return nil, fmt.Errorf("deepcoin get current funding rates: %w", err)
	}

	cycleMap := make(map[string]int64)
	for _, cycle := range cycles {
		norm := normalizeSymbol(cycle.InstrumentID)
		cycleMap[norm] = cycle.NextSettleTime * 1000
	}

	rateMap := make(map[string]float64)
	for _, rate := range ratesData.CurrentFundRates {
		norm := normalizeSymbol(rate.InstrumentID)
		rateMap[norm] = rate.FundingRate
	}

	// 6. Combine results
	var results []exchange.PotentialFundingResult
	for _, t := range filteredTickers {
		norm := normalizeSymbol(t.InstID)
		settleTime := cycleMap[norm]
		rate := rateMap[norm]

		results = append(results, exchange.PotentialFundingResult{
			Symbol:     t.InstID,
			Rate:       rate,
			SettleTime: settleTime,
			Volume24h:  volMap[norm],
			Price:      priceMap[norm],
		})
	}

	return results, nil
}
