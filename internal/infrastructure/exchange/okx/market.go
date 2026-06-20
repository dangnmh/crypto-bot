package okx

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
)

// Explicit request/response structs for market data endpoints.

type okxServerTimeResponse struct {
	Ts string `json:"ts"`
}

type okxInstrumentsRequest struct {
	InstType string `json:"instType"`
}

type okxInstrument struct {
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

type okxTickersRequest struct {
	InstType string `json:"instType"`
	InstID   string `json:"instId,omitempty"`
}

type okxTicker struct {
	InstID    string `json:"instId"`
	Last      string `json:"last"`
	BidPx     string `json:"bidPx"`
	AskPx     string `json:"askPx"`
	Vol24h    string `json:"vol24h"`
	VolCcy24h string `json:"volCcy24h"`
	Ts        string `json:"ts"`
}

type okxFundingRateRequest struct {
	InstID string `json:"instId"`
}

type okxFundingRate struct {
	InstID          string `json:"instId"`
	FundingRate     string `json:"fundingRate"`
	NextFundingTime string `json:"nextFundingTime"`
}

// Private raw methods invoking the OKX V5 REST API.

func (c *Client) getRawServerTime(ctx context.Context) (*okxServerTimeResponse, error) {
	body, err := c.RawRequest(ctx, http.MethodGet, pathServerTime, nil, nil)
	if err != nil {
		return nil, err
	}
	res, err := ParseResponseFirst[okxServerTimeResponse](body, "server_time")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *Client) getRawContractDetails(ctx context.Context, req okxInstrumentsRequest) ([]okxInstrument, error) {
	params := map[string]string{
		paramInstType: req.InstType,
	}
	body, err := c.RawRequest(ctx, http.MethodGet, pathInstruments, params, nil)
	if err != nil {
		return nil, err
	}
	return ParseResponse[okxInstrument](body, "contract_details")
}

func (c *Client) getRawTickers(ctx context.Context, req okxTickersRequest) ([]okxTicker, error) {
	params := map[string]string{
		paramInstType: req.InstType,
	}
	if req.InstID != "" {
		params[paramInstId] = req.InstID
	}
	body, err := c.GetTickersRaw(ctx, params)
	if err != nil {
		return nil, err
	}
	return ParseResponse[okxTicker](body, "tickers")
}

func (c *Client) getRawFundingRate(ctx context.Context, req okxFundingRateRequest) (*okxFundingRate, error) {
	params := map[string]string{
		paramInstId: req.InstID,
	}
	body, err := c.GetFundingRateRaw(ctx, params)
	if err != nil {
		return nil, err
	}
	frList, err := ParseResponse[okxFundingRate](body, "funding_rate")
	if err != nil {
		return nil, err
	}
	if len(frList) == 0 {
		return nil, fmt.Errorf("okx funding rate not found for symbol: %s", req.InstID)
	}
	return &frList[0], nil
}

// Public mapper methods implementing the exchange.MarketDataProvider interface.

// GetServerTime returns the OKX server timestamp in milliseconds.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	res, err := c.getRawServerTime(ctx)
	if err != nil {
		return 0, err
	}
	val, err := strconv.ParseInt(res.Ts, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse server time: %w", err)
	}
	return val, nil
}

// GetContractDetails returns specifications for all swap/futures contracts.
func (c *Client) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	instruments, err := c.getRawContractDetails(ctx, okxInstrumentsRequest{InstType: instTypeSwap})
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
		if inst.State == "live" {
			stateVal = 1
		}

		priceScale := decmath.DecimalPlaces(inst.TickSz)
		volScale := decmath.DecimalPlaces(inst.LotSz)

		details = append(details, exchange.ContractDetail{
			Symbol:           inst.InstID,
			DisplayName:      inst.InstID,
			DisplayNameEn:    inst.InstID,
			PositionOpenType: 1, // Isolated by default or cross
			BaseCoin:         inst.BaseCcy,
			QuoteCoin:        inst.SettleCcy,
			SettleCoin:       inst.SettleCcy,
			ContractSize:     ctVal,
			MinLeverage:      1,
			MaxLeverage:      lever,
			PriceScale:       priceScale,
			VolScale:         volScale,
			PriceUnit:        priceUnit,
			MinVol:           1, // default
			State:            stateVal,
		})
	}
	return details, nil
}

// GetFundingRates returns the funding rate for specific contracts.
func (c *Client) GetFundingRates(ctx context.Context, symbols []string) ([]exchange.FundingRateResult, error) {
	if len(symbols) == 0 {
		return nil, nil
	}

	rates := make([]exchange.FundingRateResult, 0, len(symbols))
	for _, sym := range symbols {
		raw, err := c.getRawFundingRate(ctx, okxFundingRateRequest{InstID: sym})
		if err != nil {
			return nil, err
		}
		fr, _ := strconv.ParseFloat(raw.FundingRate, 64)
		ns, _ := strconv.ParseInt(raw.NextFundingTime, 10, 64)
		rates = append(rates, exchange.FundingRateResult{
			Symbol:     raw.InstID,
			Rate:       fr,
			SettleTime: ns,
		})
	}
	return rates, nil
}

// GetTickers returns ticker data for all SWAP contracts or a specific instrument.
func (c *Client) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	rawTickers, err := c.getRawTickers(ctx, okxTickersRequest{
		InstType: instTypeSwap,
		InstID:   symbol,
	})
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
		amt, _ := strconv.ParseFloat(t.VolCcy24h, 64)

		exchangeTickers = append(exchangeTickers, exchange.Ticker{
			Symbol:       t.InstID,
			LastPrice:    last,
			Bid1:         bid,
			Ask1:         ask,
			Volume24:     amt, // base coin volume
			AmountUSDT24: amt * last,
			Timestamp:    ts,
		})
	}
	return exchangeTickers, nil
}
