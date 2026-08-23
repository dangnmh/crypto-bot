package futures

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
	"crypto-bot/pkg/xjson"
)

type toobitTicker struct {
	T   int64  `json:"t"`
	A   string `json:"a"`
	B   string `json:"b"`
	S   string `json:"s"`
	C   string `json:"c"`
	O   string `json:"o"`
	H   string `json:"h"`
	L   string `json:"l"`
	V   string `json:"v"`
	Qv  string `json:"qv"`
	Pc  string `json:"pc"`
	Pcp string `json:"pcp"`
}

type toobitFundingRate struct {
	Symbol          string       `json:"symbol"`
	Rate            string       `json:"rate"`
	Period          string       `json:"period"`
	NextFundingTime xjson.Number `json:"nextFundingTime"`
}

type toobitExchangeInfo struct {
	Contracts []toobitContract `json:"contracts"`
	Symbols   []toobitContract `json:"symbols"`
}

type toobitRiskLimit struct {
	Level          int          `json:"level,omitempty"`
	Quantity       xjson.Number `json:"quantity,omitempty"`
	Value          xjson.Number `json:"value,omitempty"`
	MaintainMargin xjson.Number `json:"maintainMargin,omitempty"`
	InitialMargin  xjson.Number `json:"initialMargin,omitempty"`
	MaxLeverage    xjson.Number `json:"maxLeverage"`
}

type toobitContract struct {
	Symbol             string            `json:"symbol"`
	BaseAsset          string            `json:"baseAsset"`
	QuoteAsset         string            `json:"quoteAsset"`
	MarginAsset        string            `json:"marginAsset"`
	ContractMultiplier string            `json:"contractMultiplier"`
	Filters            []toobitFilter    `json:"filters"`
	RiskLimits         []toobitRiskLimit `json:"riskLimits"`
}

type toobitFilter struct {
	FilterType string `json:"filterType"`
	TickSize   string `json:"tickSize,omitempty"`
	StepSize   string `json:"stepSize,omitempty"`
	MinQty     string `json:"minQty,omitempty"`
	MaxQty     string `json:"maxQty,omitempty"`
	MinPrice   string `json:"minPrice,omitempty"`
}

type toobitDepthResponse struct {
	Time int64            `json:"time"`
	Bids [][]xjson.Number `json:"bids"`
	Asks [][]xjson.Number `json:"asks"`
}

func parseToobitRiskLimits(rawLimits []toobitRiskLimit) (int, []exchange.RiskLimitTier) {
	maxLeverage := 100
	if len(rawLimits) == 0 {
		return maxLeverage, nil
	}
	var parsedRiskLimits []exchange.RiskLimitTier
	highestLev := 0.0
	for _, rl := range rawLimits {
		lev, _ := rl.MaxLeverage.Float64()
		val, _ := rl.Value.Float64()
		qty, _ := rl.Quantity.Float64()
		if lev > highestLev {
			highestLev = lev
		}
		if lev > 0 {
			parsedRiskLimits = append(parsedRiskLimits, exchange.RiskLimitTier{
				Level:       rl.Level,
				MaxLeverage: int(lev),
				MaxNotional: val,
				MaxQuantity: qty,
			})
		}
	}
	if highestLev > 0 {
		maxLeverage = int(highestLev)
	}
	return maxLeverage, parsedRiskLimits
}

func isToobitValidSymbol(symbol string) bool {
	if strings.HasPrefix(symbol, "TBV_") || strings.HasPrefix(symbol, "TEST") {
		return false
	}
	return strings.HasSuffix(symbol, "-SWAP-USDT") || strings.HasSuffix(symbol, "-SWAP-USDC")
}

func isToobitValidTicker(symbol string, timestamp, nowMs int64) bool {
	if !isToobitValidSymbol(symbol) {
		return false
	}
	if timestamp > 0 && nowMs > 0 {
		diff := nowMs - timestamp
		if diff < 0 {
			diff = -diff
		}
		if diff > int64(24*time.Hour/time.Millisecond) {
			return false
		}
	}
	return true
}

// GetContractDetails returns contracts specifications for all active futures.
func (c *Client) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	body, err := c.base.Request(ctx, http.MethodGet, "/api/v1/exchangeInfo", nil, false)
	if err != nil {
		return nil, err
	}
	var resp toobitExchangeInfo
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal exchange info: %w", err)
	}

	contracts := resp.Contracts
	if len(contracts) == 0 {
		contracts = resp.Symbols
	}

	details := make([]exchange.ContractDetail, 0, len(contracts))
	for i := range contracts {
		raw := &contracts[i]
		if !isToobitValidSymbol(raw.Symbol) {
			continue
		}

		priceUnit := 0.0
		minVol := 0.0
		maxVol := 0.0
		stepSize := 0.0
		tickSizeStr := ""
		stepSizeStr := ""

		for _, f := range raw.Filters {
			switch f.FilterType {
			case "PRICE_FILTER":
				priceUnit = decmath.ParseFloat(f.TickSize)
				tickSizeStr = f.TickSize
			case "LOT_SIZE":
				minVol = decmath.ParseFloat(f.MinQty)
				maxVol = decmath.ParseFloat(f.MaxQty)
				stepSize = decmath.ParseFloat(f.StepSize)
				stepSizeStr = f.StepSize
			}
		}

		priceScale := decmath.DecimalPlaces(tickSizeStr)
		volScale := decmath.DecimalPlaces(stepSizeStr)

		multiplier := 1.0
		if raw.ContractMultiplier != "" {
			multiplier = decmath.ParseFloat(raw.ContractMultiplier)
		}

		displayName := raw.Symbol
		displayName = strings.ReplaceAll(displayName, "-SWAP", "")
		displayName = strings.ReplaceAll(displayName, "-", "")
		displayName = strings.ReplaceAll(displayName, "_", "")

		maxLeverage, parsedRiskLimits := parseToobitRiskLimits(raw.RiskLimits)

		maxVolVal := int(maxVol)
		if maxVolVal <= 0 {
			maxVolVal = 1000000000
		}

		details = append(details, exchange.ContractDetail{
			Symbol:        raw.Symbol,
			DisplayName:   displayName,
			DisplayNameEn: displayName,
			BaseCoin:      raw.BaseAsset,
			QuoteCoin:     raw.QuoteAsset,
			SettleCoin:    raw.MarginAsset,
			ContractSize:  multiplier,
			MinLeverage:   1,
			MaxLeverage:   maxLeverage,
			PriceUnit:     priceUnit,
			MinVol:        int(minVol),
			MaxVol:        maxVolVal,
			VolUnit:       int(stepSize),
			PriceScale:    priceScale,
			VolScale:      volScale,
			State:         1,
			RiskLimits:    parsedRiskLimits,
		})
	}

	return details, nil
}

// GetTickers returns 24hr ticker price change statistics for futures.
func (c *Client) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	query := make(map[string]string)
	if symbol != "" {
		query["symbol"] = symbol
	}
	body, err := c.base.Request(ctx, http.MethodGet, "/quote/v1/contract/ticker/24hr", query, false)
	if err != nil {
		return nil, err
	}

	var rawList []toobitTicker
	if err := xjson.Unmarshal(body, &rawList); err != nil {
		return nil, fmt.Errorf("unmarshal tickers: %w", err)
	}

	nowMs := int64(0)
	if c.base.Clock() != nil {
		nowMs = c.base.Clock().Now().UnixMilli()
	}

	tickers := make([]exchange.Ticker, 0, len(rawList))
	for i := range rawList {
		item := &rawList[i]
		if symbol == "" && !isToobitValidTicker(item.S, item.T, nowMs) {
			continue
		}
		last, _ := strconv.ParseFloat(item.C, 64)
		bid, _ := strconv.ParseFloat(item.B, 64)
		ask, _ := strconv.ParseFloat(item.A, 64)
		vol, _ := strconv.ParseFloat(item.V, 64)
		amt, _ := strconv.ParseFloat(item.Qv, 64)

		tickers = append(tickers, exchange.Ticker{
			Symbol:       item.S,
			LastPrice:    last,
			Bid1:         bid,
			Ask1:         ask,
			Volume24:     vol,
			AmountUSDT24: amt,
			Timestamp:    item.T,
		})
	}

	return tickers, nil
}

// GetFundingRates returns funding rates for specific futures symbols.
func (c *Client) GetFundingRates(ctx context.Context, symbols []string) ([]exchange.FundingRateResult, error) {
	if len(symbols) == 0 {
		return nil, nil
	}

	body, err := c.base.Request(ctx, http.MethodGet, "/api/v1/futures/fundingRate", nil, false)
	if err != nil {
		return nil, err
	}

	var rawList []toobitFundingRate
	if err := xjson.Unmarshal(body, &rawList); err != nil {
		return nil, fmt.Errorf("unmarshal funding rates: %w", err)
	}

	rateMap := make(map[string]*toobitFundingRate)
	for i := range rawList {
		item := &rawList[i]
		rateMap[item.Symbol] = item
	}

	results := make([]exchange.FundingRateResult, 0, len(symbols))
	for _, sym := range symbols {
		item, exists := rateMap[sym]
		if !exists {
			continue
		}

		ts, _ := item.NextFundingTime.Int64()
		rateVal, _ := strconv.ParseFloat(item.Rate, 64)
		results = append(results, exchange.FundingRateResult{
			Symbol:     sym,
			Rate:       rateVal,
			SettleTime: ts,
		})
	}

	return results, nil
}

// GetPotentialFundingSymbols scans symbols meeting volume and whitelist/blacklist criteria.
func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	tickers, err := c.GetTickers(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("toobit list tickers: %w", err)
	}

	whitelistMap := make(map[string]bool)
	for _, sym := range whitelist {
		whitelistMap[sym] = true
	}

	blacklistMap := make(map[string]bool)
	for _, sym := range blacklist {
		blacklistMap[sym] = true
	}

	var filteredSymbols []string
	volMap := make(map[string]float64)
	priceMap := make(map[string]float64)

	for _, t := range tickers {
		stdSym := t.Symbol
		if blacklistMap[stdSym] {
			continue
		}
		if len(whitelistMap) > 0 && !whitelistMap[stdSym] {
			continue
		}
		if t.AmountUSDT24 < minVol24h {
			continue
		}
		if maxVol24h > 0 && t.AmountUSDT24 > maxVol24h {
			continue
		}

		filteredSymbols = append(filteredSymbols, t.Symbol)
		volMap[stdSym] = t.AmountUSDT24
		priceMap[stdSym] = t.LastPrice
	}

	if len(filteredSymbols) == 0 {
		return nil, nil
	}

	rates, err := c.GetFundingRates(ctx, filteredSymbols)
	if err != nil {
		return nil, fmt.Errorf("toobit list funding rates: %w", err)
	}

	var results []exchange.PotentialFundingResult
	for _, r := range rates {
		stdSym := r.Symbol
		results = append(results, exchange.PotentialFundingResult{
			Symbol:     r.Symbol,
			Rate:       r.Rate,
			SettleTime: r.SettleTime,
			Volume24h:  volMap[stdSym],
			Price:      priceMap[stdSym],
		})
	}

	return results, nil
}

// GetTopGainer returns tickers sorted by 24h price change percentage descending.
func (c *Client) GetTopGainer(ctx context.Context, req exchange.TopGainerRequest) ([]exchange.TopGainerResult, error) {
	body, err := c.base.Request(ctx, http.MethodGet, "/quote/v1/contract/ticker/24hr", nil, false)
	if err != nil {
		return nil, fmt.Errorf("toobit get top gainer tickers: %w", err)
	}

	var rawList []toobitTicker
	if err := xjson.Unmarshal(body, &rawList); err != nil {
		return nil, fmt.Errorf("unmarshal toobit top gainer tickers: %w", err)
	}

	nowMs := int64(0)
	if c.base.Clock() != nil {
		nowMs = c.base.Clock().Now().UnixMilli()
	}

	results := make([]exchange.TopGainerResult, 0, len(rawList))
	for i := range rawList {
		item := &rawList[i]
		if !isToobitValidTicker(item.S, item.T, nowMs) {
			continue
		}
		last, _ := strconv.ParseFloat(item.C, 64)
		bid, _ := strconv.ParseFloat(item.B, 64)
		ask, _ := strconv.ParseFloat(item.A, 64)
		qv, _ := strconv.ParseFloat(item.Qv, 64)
		pcp, _ := strconv.ParseFloat(item.Pcp, 64)

		if last <= 0 {
			continue
		}

		spreadPct := 0.0
		if bid > 0 && ask > 0 {
			spreadPct = ((ask - bid) / bid) * 100.0
		}

		results = append(results, exchange.TopGainerResult{
			Symbol:        item.S,
			LastPrice:     last,
			Bid1:          bid,
			Ask1:          ask,
			Volume24hUSDT: qv,
			Gain24hPct:    pcp,
			SpreadPct:     spreadPct,
			Timestamp:     item.T,
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

// GetDepth retrieves the current full L2 depth snapshot for a symbol via REST API.
func (c *Client) GetDepth(ctx context.Context, symbol string) (*domain.OrderBook, error) {
	params := map[string]string{
		symbolKey: symbol,
		limitKey:  "100",
	}
	body, err := c.base.Request(ctx, http.MethodGet, "/quote/v1/depth", params, false)
	if err != nil {
		return nil, fmt.Errorf("toobit get depth for %s: %w", symbol, err)
	}

	var data toobitDepthResponse
	if err := xjson.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("toobit parse depth for %s: %w", symbol, err)
	}

	bids := make([]domain.OrderBookEntry, 0, len(data.Bids))
	for _, b := range data.Bids {
		if len(b) >= 2 {
			p, v := xjson.ToFloat64(b[0]), xjson.ToFloat64(b[1])
			if p > 0 && v > 0 {
				bids = append(bids, domain.OrderBookEntry{Price: p, Volume: v})
			}
		}
	}

	asks := make([]domain.OrderBookEntry, 0, len(data.Asks))
	for _, a := range data.Asks {
		if len(a) >= 2 {
			p, v := xjson.ToFloat64(a[0]), xjson.ToFloat64(a[1])
			if p > 0 && v > 0 {
				asks = append(asks, domain.OrderBookEntry{Price: p, Volume: v})
			}
		}
	}

	return &domain.OrderBook{
		Symbol:  symbol,
		Version: data.Time,
		Bids:    bids,
		Asks:    asks,
	}, nil
}
