package gate

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
	"crypto-bot/pkg/xjson"
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

func (c *Client) rawGetContractDetails(ctx context.Context, req gateContractsRequest) ([]gateContract, error) {
	path := fmt.Sprintf("/futures/%s/contracts", req.Settle)
	body, err := c.RawRequest(ctx, "GET", path, nil, nil)
	if err != nil {
		return nil, err
	}
	var result []gateContract
	if err := xjson.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal gate response: %w", err)
	}
	return result, nil
}

func (c *Client) rawGetTickers(ctx context.Context, req gateTickersRequest) ([]gateFuturesTicker, error) {
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
	if err := xjson.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal gate response: %w", err)
	}
	return result, nil
}

// Public mapper methods implementing the exchange.MarketDataProvider interface.

// GetContractDetails returns all contract specifications.
func (c *Client) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	contracts, err := c.rawGetContractDetails(ctx, gateContractsRequest{Settle: gateSettleUsdt})
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
	rawTickers, err := c.rawGetTickers(ctx, gateTickersRequest{Settle: gateSettleUsdt, Contract: symbol})
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
	contracts, err := c.rawGetContractDetails(ctx, gateContractsRequest{Settle: settle})
	if err != nil {
		return fmt.Errorf("gate.io list %s contracts: %w", settle, err)
	}
	for i := range contracts {
		contractMap[contracts[i].Name] = &contracts[i]
	}
	return nil
}

func filterGateTickers(tickers []gateFuturesTicker, minVol24h, maxVol24h float64, whitelistMap, blacklistMap map[string]bool) ([]string, map[string]float64, map[string]float64) {
	var filteredSymbols []string
	volMap := make(map[string]float64)
	priceMap := make(map[string]float64)

	for i := range tickers {
		t := &tickers[i]
		if blacklistMap[t.Contract] {
			continue
		}
		if len(whitelistMap) > 0 && !whitelistMap[t.Contract] {
			continue
		}

		vol := decmath.ParseFloat(t.Volume24hQuote)
		if vol < minVol24h {
			continue
		}
		if maxVol24h > 0 && vol > maxVol24h {
			continue
		}

		filteredSymbols = append(filteredSymbols, t.Contract)
		volMap[t.Contract] = vol
		priceMap[t.Contract] = decmath.ParseFloat(t.Last)
	}
	return filteredSymbols, volMap, priceMap
}

func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	// 1. Fetch tickers for usdt settle coin
	tickers, err := c.rawGetTickers(ctx, gateTickersRequest{Settle: gateSettleUsdt})
	if err != nil {
		return nil, fmt.Errorf("gate.io list tickers: %w", err)
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
	filteredSymbols, volMap, priceMap := filterGateTickers(tickers, minVol24h, maxVol24h, whitelistMap, blacklistMap)
	if len(filteredSymbols) == 0 {
		return nil, nil
	}

	// 4. Fetch contracts for usdt settle coin
	contracts, err := c.rawGetContractDetails(ctx, gateContractsRequest{Settle: gateSettleUsdt})
	if err != nil {
		return nil, fmt.Errorf("gate.io list contracts: %w", err)
	}

	contractMap := make(map[string]*gateContract)
	for i := range contracts {
		contractMap[contracts[i].Name] = &contracts[i]
	}

	// 5. Build results
	var results []exchange.PotentialFundingResult
	for _, sym := range filteredSymbols {
		contract, exists := contractMap[sym]
		if !exists {
			continue
		}

		results = append(results, exchange.PotentialFundingResult{
			Symbol:     sym,
			Rate:       decmath.ParseFloat(contract.FundingRate),
			SettleTime: int64(contract.FundingNextApply * 1000),
			Volume24h:  volMap[sym],
			Price:      priceMap[sym],
		})
	}

	return results, nil
}

// FetchKlines fetches public K-lines for Gate.io.
func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
	q := url.Values{}
	q.Set("contract", symbol)
	q.Set("interval", string(interval))
	q.Set("limit", "100")

	if !start.IsZero() {
		q.Set("from", strconv.FormatInt(start.Unix(), 10))
	}
	if !end.IsZero() {
		q.Set("to", strconv.FormatInt(end.Unix(), 10))
	}

	var data [][]any
	err := c.sendRequest(ctx, http.MethodGet, "/futures/usdt/candlesticks", q, nil, &data)
	if err != nil {
		return nil, fmt.Errorf("gate fetch klines: %w", err)
	}

	var klines []exchange.Kline
	for _, k := range data {
		if len(k) < 6 {
			continue
		}
		tsVal, ok := k[0].(float64)
		if !ok {
			tsStr, okStr := k[0].(string)
			if okStr {
				if parsed, err := strconv.ParseInt(tsStr, 10, 64); err == nil {
					tsVal = float64(parsed)
				} else {
					continue
				}
			} else {
				continue
			}
		}
		closePrice, err := strconv.ParseFloat(fmt.Sprintf("%v", k[2]), 64)
		if err != nil {
			continue
		}
		open, err := strconv.ParseFloat(fmt.Sprintf("%v", k[3]), 64)
		if err != nil {
			open = closePrice
		}
		high, err := strconv.ParseFloat(fmt.Sprintf("%v", k[4]), 64)
		if err != nil {
			high = closePrice
		}
		low, err := strconv.ParseFloat(fmt.Sprintf("%v", k[5]), 64)
		if err != nil {
			low = closePrice
		}
		vol, err := strconv.ParseFloat(fmt.Sprintf("%v", k[1]), 64)
		if err != nil {
			vol = 0
		}

		klines = append(klines, exchange.Kline{
			Timestamp: int64(tsVal) * 1000,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     closePrice,
			Volume:    vol,
		})
	}
	return klines, nil
}
