package gate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

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

// Private raw methods using raw HTTP requests.

func (c *Client) getRawServerTime(ctx context.Context) (*gateSystemTime, error) {
	body, err := c.RawRequest(ctx, "GET", "/spot/time", nil, nil)
	if err != nil {
		return nil, err
	}
	var result gateSystemTime
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal gate response: %w", err)
	}
	return &result, nil
}

func (c *Client) getRawContractDetails(ctx context.Context, req gateContractsRequest) ([]gateContract, error) {
	path := fmt.Sprintf("/futures/%s/contracts", req.Settle)
	body, err := c.RawRequest(ctx, "GET", path, nil, nil)
	if err != nil {
		return nil, err
	}
	var result []gateContract
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal gate response: %w", err)
	}
	return result, nil
}

func (c *Client) getRawTickers(ctx context.Context, req gateTickersRequest) ([]gateFuturesTicker, error) {
	params := map[string]string{
		paramSettle: req.Settle,
	}
	if req.Contract != "" {
		params[paramContract] = req.Contract
	}
	body, err := c.GetTickersRaw(ctx, params)
	if err != nil {
		return nil, err
	}
	var result []gateFuturesTicker
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal gate response: %w", err)
	}
	return result, nil
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
		amt := decmath.ParseFloat(raw.Volume24hQuote)
		tickers = append(tickers, exchange.Ticker{
			Symbol:       raw.Contract,
			LastPrice:    decmath.ParseFloat(raw.Last),
			Bid1:         decmath.ParseFloat(raw.HighestBid),
			Ask1:         decmath.ParseFloat(raw.LowestAsk),
			Volume24:     decmath.ParseFloat(raw.Volume24h),
			AmountUSDT24: amt,
			Timestamp:    time.Now().UnixMilli(),
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
