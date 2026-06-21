package kucoin

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
)

type kucoinContract struct {
	Symbol                  string  `json:"symbol"`
	BaseCurrency            string  `json:"baseCurrency"`
	QuoteCurrency           string  `json:"quoteCurrency"`
	SettleCurrency          string  `json:"settleCurrency"`
	LotSize                 int64   `json:"lotSize"`
	TickSize                float64 `json:"tickSize"`
	Multiplier              float64 `json:"multiplier"`
	Status                  string  `json:"status"`
	TurnoverOf24h           float64 `json:"turnoverOf24h"`
	VolumeOf24h             float64 `json:"volumeOf24h"`
	FundingFeeRate          float64 `json:"fundingFeeRate"`
	NextFundingRateDateTime int64   `json:"nextFundingRateDateTime"`
	MaxLeverage             float64 `json:"maxLeverage"`
}

type kucoinServerTimeRequest struct{}

type kucoinContractsRequest struct{}

type kucoinTickersRequest struct{}

type kucoinTickerSingleRequest struct {
	Symbol string `json:"symbol"`
}

type kucoinSingleTicker struct {
	Symbol       string `json:"symbol"`
	BestBidPrice string `json:"bestBidPrice"`
	BestAskPrice string `json:"bestAskPrice"`
	Price        string `json:"price"`
	Size         string `json:"size"`
	Ts           string `json:"ts"`
}

type kucoinTicker struct {
	Symbol       string `json:"symbol"`
	BestBidPrice string `json:"bestBidPrice"`
	BestAskPrice string `json:"bestAskPrice"`
	LastPrice    string `json:"lastPrice"`
	Price        string `json:"price"`
	Volume       string `json:"volume"`
	Vol          string `json:"vol"`
	Ts           int64  `json:"ts"`
}

// Private raw methods invoking the KuCoin REST API.

func (c *Client) getRawServerTime(ctx context.Context, _ kucoinServerTimeRequest) (json.RawMessage, error) {
	body, err := c.RawRequest(ctx, http.MethodGet, pathServerTime, nil, nil)
	if err != nil {
		return nil, err
	}
	return body, nil
}

func (c *Client) getRawContractDetails(ctx context.Context, _ kucoinContractsRequest) ([]kucoinContract, error) {
	body, err := c.RawRequest(ctx, http.MethodGet, pathContracts, nil, nil)
	if err != nil {
		return nil, err
	}
	return ParseResponse[[]kucoinContract](body, "contract_details")
}

func (c *Client) getRawTickerSingle(ctx context.Context, req kucoinTickerSingleRequest) (*kucoinSingleTicker, error) {
	params := map[string]string{
		paramSymbol: req.Symbol,
	}
	body, err := c.GetTickersRaw(ctx, params)
	if err != nil {
		return nil, err
	}
	res, err := ParseResponse[kucoinSingleTicker](body, "ticker_single")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *Client) getRawTickers(ctx context.Context, _ kucoinTickersRequest) ([]kucoinTicker, error) {
	body, err := c.GetTickersRaw(ctx, nil)
	if err != nil {
		return nil, err
	}
	return ParseResponse[[]kucoinTicker](body, "tickers")
}

// Public mapper methods implementing the exchange.MarketDataProvider interface.

// GetServerTime returns the KuCoin server timestamp in milliseconds.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	body, err := c.getRawServerTime(ctx, kucoinServerTimeRequest{})
	if err != nil {
		return 0, err
	}

	var numVal int64
	if err := json.Unmarshal(body, &numVal); err == nil {
		return numVal, nil
	}

	data, err := ParseResponse[int64](body, "server_time")
	if err != nil {
		return 0, err
	}

	return data, nil
}

// GetContractDetails returns specifications for all active Futures contracts.
func (c *Client) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	instruments, err := c.getRawContractDetails(ctx, kucoinContractsRequest{})
	if err != nil {
		return nil, err
	}

	details := make([]exchange.ContractDetail, 0, len(instruments))
	for i := range instruments {
		inst := &instruments[i]

		stateVal := 0
		if inst.Status == statusOpen {
			stateVal = 1
		}

		tickSize := inst.TickSize

		priceScale := decmath.DecimalPlaces(decmath.FormatFloat(tickSize))
		if priceScale <= 0 {
			priceScale = 2
		}

		maxLeverage := int(inst.MaxLeverage)
		if maxLeverage <= 0 {
			maxLeverage = 100
		}

		details = append(details, exchange.ContractDetail{
			Symbol:           inst.Symbol,
			DisplayName:      inst.Symbol,
			DisplayNameEn:    inst.Symbol,
			PositionOpenType: 1, // Isolated/Cross both supported.
			BaseCoin:         inst.BaseCurrency,
			QuoteCoin:        inst.QuoteCurrency,
			SettleCoin:       inst.SettleCurrency,
			ContractSize:     inst.Multiplier,
			MinLeverage:      1,
			MaxLeverage:      maxLeverage,
			PriceScale:       priceScale,
			VolScale:         0, // Defaults to 0 (unit contracts).
			PriceUnit:        tickSize,
			MinVol:           int(inst.LotSize),
			State:            stateVal,
		})
	}

	return details, nil
}

func (c *Client) getSingleTicker(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	raw, err := c.getRawTickerSingle(ctx, kucoinTickerSingleRequest{
		Symbol: symbol,
	})
	if err != nil {
		return nil, err
	}

	last := decmath.ParseFloat(raw.Price)
	bid := decmath.ParseFloat(raw.BestBidPrice)
	ask := decmath.ParseFloat(raw.BestAskPrice)
	ts := decmath.ParseInt64(raw.Ts)

	var vol, amt float64
	cList, err := c.getRawContractDetails(ctx, kucoinContractsRequest{})
	if err == nil {
		for i := range cList {
			if cList[i].Symbol == symbol {
				vol = cList[i].VolumeOf24h
				amt = cList[i].TurnoverOf24h
				break
			}
		}
	}
	if amt == 0 && vol > 0 {
		amt = vol * last
	}

	return []exchange.Ticker{
		{
			Symbol:       raw.Symbol,
			LastPrice:    last,
			Bid1:         bid,
			Ask1:         ask,
			Volume24:     vol,
			AmountUSDT24: amt,
			Timestamp:    ts,
		},
	}, nil
}

// GetTickers returns ticker data for all contracts.
func (c *Client) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	if symbol != "" {
		return c.getSingleTicker(ctx, symbol)
	}

	tickers, err := c.getRawTickers(ctx, kucoinTickersRequest{})
	if err != nil {
		return nil, err
	}

	// Fetch active contracts in bulk to populate volume & turnover 24h without separate API calls.
	cVolMap := make(map[string]float64)
	cAmtMap := make(map[string]float64)
	cList, err := c.getRawContractDetails(ctx, kucoinContractsRequest{})
	if err == nil {
		for i := range cList {
			cVolMap[cList[i].Symbol] = cList[i].VolumeOf24h
			cAmtMap[cList[i].Symbol] = cList[i].TurnoverOf24h
		}
	}

	exchangeTickers := make([]exchange.Ticker, 0, len(tickers))
	for i := range tickers {
		t := &tickers[i]

		last := decmath.ParseFloat(t.LastPrice)
		if last == 0 {
			last = decmath.ParseFloat(t.Price)
		}

		bid := decmath.ParseFloat(t.BestBidPrice)
		ask := decmath.ParseFloat(t.BestAskPrice)
		ts := t.Ts

		vol := cVolMap[t.Symbol]
		amt := cAmtMap[t.Symbol]
		if vol == 0 {
			vol = decmath.ParseFloat(t.Volume)
			if vol == 0 {
				vol = decmath.ParseFloat(t.Vol)
			}
			amt = vol * last
		}

		exchangeTickers = append(exchangeTickers, exchange.Ticker{
			Symbol:       t.Symbol,
			LastPrice:    last,
			Bid1:         bid,
			Ask1:         ask,
			Volume24:     vol,
			AmountUSDT24: amt,
			Timestamp:    ts,
		})
	}

	return exchangeTickers, nil
}

// GetFundingRates returns current funding rate details for the specified symbols.
func (c *Client) GetFundingRates(ctx context.Context, symbols []string) ([]exchange.FundingRateResult, error) {
	if len(symbols) == 0 {
		return nil, nil
	}

	contracts, err := c.getRawContractDetails(ctx, kucoinContractsRequest{})
	if err != nil {
		return nil, err
	}

	contractMap := make(map[string]kucoinContract, len(contracts))
	for i := range contracts {
		contractMap[contracts[i].Symbol] = contracts[i]
	}

	rates := make([]exchange.FundingRateResult, 0, len(symbols))
	for _, sym := range symbols {
		contract, exists := contractMap[sym]
		if !exists {
			c.logger.WarnContext(ctx, "Symbol not found in active contracts", slog.String("symbol", sym))
			continue
		}
		rates = append(rates, exchange.FundingRateResult{
			Symbol:     sym,
			Rate:       contract.FundingFeeRate,
			SettleTime: contract.NextFundingRateDateTime,
		})
	}

	return rates, nil
}

func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	contracts, err := c.getRawContractDetails(ctx, kucoinContractsRequest{})
	if err != nil {
		return nil, err
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
	for i := range contracts {
		inst := &contracts[i]
		if blacklistMap[inst.Symbol] {
			continue
		}
		if len(whitelistMap) > 0 && !whitelistMap[inst.Symbol] {
			continue
		}
		if inst.Status != statusOpen {
			continue
		}

		vol := inst.TurnoverOf24h
		if vol < minVol24h {
			continue
		}
		if maxVol24h > 0 && vol > maxVol24h {
			continue
		}

		results = append(results, exchange.PotentialFundingResult{
			Symbol:     inst.Symbol,
			Rate:       inst.FundingFeeRate,
			SettleTime: inst.NextFundingRateDateTime,
			Volume24h:  vol,
		})
	}

	return results, nil
}
