package pionex

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
	"crypto-bot/pkg/xjson"
)

type pionexIndexItem struct {
	Symbol          string       `json:"symbol"`
	IndexPrice      xjson.Number `json:"indexPrice"`
	MarkPrice       xjson.Number `json:"markPrice"`
	NextFundingRate xjson.Number `json:"nextFundingRate"`
	NextFundingTime int64        `json:"nextFundingTime"`
	UpdateTime      int64        `json:"updateTime"`
}

type pionexIndexesResponse struct {
	Result bool              `json:"result"`
	Data   pionexIndexesData `json:"data"`
}

type pionexIndexesData struct {
	Indexes []pionexIndexItem `json:"indexes"`
}

type pionexTickerItem struct {
	Symbol string       `json:"symbol"`
	Close  xjson.Number `json:"close"`
	Volume xjson.Number `json:"volume"`
	Amount xjson.Number `json:"amount"`
}

type pionexTickersResponse struct {
	Result bool              `json:"result"`
	Data   pionexTickersData `json:"data"`
}

type pionexTickersData struct {
	Tickers []pionexTickerItem `json:"tickers"`
}

type pionexBookTickerItem struct {
	Symbol    string       `json:"symbol"`
	BidPrice  xjson.Number `json:"bidPrice"`
	BidSize   xjson.Number `json:"bidSize"`
	AskPrice  xjson.Number `json:"askPrice"`
	AskSize   xjson.Number `json:"askSize"`
	Timestamp int64        `json:"timestamp"`
}

type pionexBookTickersResponse struct {
	Result bool                  `json:"result"`
	Data   pionexBookTickersData `json:"data"`
}

type pionexBookTickersData struct {
	Tickers []pionexBookTickerItem `json:"tickers"`
}

type pionexSymbolItem struct {
	Symbol         string       `json:"symbol"`
	Name           string       `json:"name"`
	Type           string       `json:"type"`
	BaseCurrency   string       `json:"baseCurrency"`
	QuoteCurrency  string       `json:"quoteCurrency"`
	BasePrecision  xjson.Number `json:"basePrecision"`
	QuotePrecision xjson.Number `json:"quotePrecision"`
	BaseStep       xjson.Number `json:"baseStep"`
	QuoteStep      xjson.Number `json:"quoteStep"`
	MinSizeLimit   xjson.Number `json:"minSizeLimit"`
	MaxSizeLimit   xjson.Number `json:"maxSizeLimit"`
	Status         string       `json:"status"`
}

type pionexSymbolsResponse struct {
	Result bool              `json:"result"`
	Data   pionexSymbolsData `json:"data"`
}

type pionexSymbolsData struct {
	Symbols []pionexSymbolItem `json:"symbols"`
}

// GetTickers retrieves real-time ticker data.
func (c *Client) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	var query map[string]string
	if symbol != "" {
		query = map[string]string{
			symbolKey: symbol,
		}
	} else {
		query = map[string]string{
			typeKey: typePerp,
		}
	}

	tickersBody, err := c.rawRequestPublic(ctx, "GET", "/api/v1/market/tickers", query)
	if err != nil {
		return nil, fmt.Errorf("pionex tickers error: %w", err)
	}

	var tickersResp pionexTickersResponse
	if err := xjson.Unmarshal(tickersBody, &tickersResp); err != nil {
		return nil, fmt.Errorf("unmarshal pionex tickers: %w", err)
	}
	if !tickersResp.Result {
		return nil, fmt.Errorf("pionex tickers API failed")
	}

	var bookQuery map[string]string
	if symbol != "" {
		bookQuery = map[string]string{
			symbolKey: symbol,
		}
	}
	bookBody, err := c.rawRequestPublic(ctx, "GET", "/api/v1/market/bookTickers", bookQuery)
	if err != nil {
		return nil, fmt.Errorf("pionex book tickers error: %w", err)
	}

	var bookResp pionexBookTickersResponse
	if err := xjson.Unmarshal(bookBody, &bookResp); err != nil {
		return nil, fmt.Errorf("unmarshal pionex book tickers: %w", err)
	}
	if !bookResp.Result {
		return nil, fmt.Errorf("pionex book tickers API failed")
	}

	bookMap := make(map[string]*pionexBookTickerItem)
	for i := range bookResp.Data.Tickers {
		item := &bookResp.Data.Tickers[i]
		bookMap[item.Symbol] = item
	}

	var out []exchange.Ticker
	for _, t := range tickersResp.Data.Tickers {
		// Only support perps or specific symbol
		if symbol == "" && !strings.HasSuffix(t.Symbol, "_PERP") {
			continue
		}

		lastVal := xjson.ToFloat64(t.Close)
		volVal := xjson.ToFloat64(t.Volume)
		amtVal := xjson.ToFloat64(t.Amount)
		if amtVal == 0 {
			amtVal = volVal * lastVal
		}

		var bid, ask float64
		var ts int64
		if b, ok := bookMap[t.Symbol]; ok {
			bid = xjson.ToFloat64(b.BidPrice)
			ask = xjson.ToFloat64(b.AskPrice)
			ts = b.Timestamp
		}

		out = append(out, exchange.Ticker{
			Symbol:       t.Symbol,
			LastPrice:    lastVal,
			Bid1:         bid,
			Ask1:         ask,
			Volume24:     volVal,
			AmountUSDT24: amtVal,
			Timestamp:    ts,
		})
	}

	return out, nil
}

// GetContractDetails retrieves contract details.
type pionexRiskRow struct {
	RowNum      int    `json:"rowNum"`
	MaxLeverage string `json:"maxLeverage"`
}

type pionexRiskSymbolItem struct {
	Symbol string          `json:"symbol"`
	Rows   []pionexRiskRow `json:"rows"`
}

type pionexRiskTableResponse struct {
	Result bool                `json:"result"`
	Data   pionexRiskTableData `json:"data"`
}

type pionexRiskTableData struct {
	Symbols []pionexRiskSymbolItem `json:"symbols"`
}

func parseRiskTable(riskBody []byte) (map[string]int, error) {
	riskMap := make(map[string]int)
	var riskResp pionexRiskTableResponse
	if err := xjson.Unmarshal(riskBody, &riskResp); err != nil {
		return nil, err
	}
	if !riskResp.Result {
		return nil, fmt.Errorf("riskTable API result false")
	}
	for _, sym := range riskResp.Data.Symbols {
		for _, row := range sym.Rows {
			if row.RowNum == 1 {
				if lev, err := strconv.Atoi(row.MaxLeverage); err == nil {
					riskMap[sym.Symbol] = lev
				}
				break
			}
		}
	}
	return riskMap, nil
}

// GetContractDetails retrieves contract details.
func (c *Client) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	query := map[string]string{
		typeKey: typePerp,
	}
	body, err := c.rawRequestPublic(ctx, "GET", "/api/v1/common/symbols", query)
	if err != nil {
		return nil, fmt.Errorf("pionex symbols error: %w", err)
	}

	var resp pionexSymbolsResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal pionex symbols: %w", err)
	}
	if !resp.Result {
		return nil, fmt.Errorf("pionex symbols API failed")
	}

	riskMap := make(map[string]int)
	riskBody, err := c.GetRiskTableRaw(ctx, nil)
	if err == nil {
		if m, err := parseRiskTable(riskBody); err == nil {
			riskMap = m
		}
	}

	var out []exchange.ContractDetail
	for i := range resp.Data.Symbols {
		s := &resp.Data.Symbols[i]
		if !strings.EqualFold(s.Type, "PERP") {
			continue
		}

		priceUnit := xjson.ToFloat64(s.QuoteStep)
		minVolVal := xjson.ToFloat64(s.MinSizeLimit)
		baseStepVal := xjson.ToFloat64(s.BaseStep)

		maxLeverage := 100
		if lev, ok := riskMap[s.Symbol]; ok {
			maxLeverage = lev
		}

		out = append(out, exchange.ContractDetail{
			Symbol:        s.Symbol,
			DisplayName:   s.Name,
			DisplayNameEn: s.Name,
			BaseCoin:      s.BaseCurrency,
			QuoteCoin:     s.QuoteCurrency,
			SettleCoin:    s.QuoteCurrency,
			ContractSize:  1.0,
			MinLeverage:   1,
			MaxLeverage:   maxLeverage,
			PriceUnit:     priceUnit,
			MinVol:        int(minVolVal),
			VolUnit:       int(baseStepVal),
			PriceScale:    decmath.DecimalPlaces(s.QuoteStep.String()),
			VolScale:      decmath.DecimalPlaces(s.BaseStep.String()),
			State:         1,
		})
	}

	return out, nil
}

// GetFundingRates retrieves funding rates.
func (c *Client) GetFundingRates(ctx context.Context, symbols []string) ([]exchange.FundingRateResult, error) {
	body, err := c.rawRequestPublic(ctx, "GET", "/api/v1/market/indexes", nil)
	if err != nil {
		return nil, fmt.Errorf("pionex indexes error: %w", err)
	}

	var resp pionexIndexesResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal pionex indexes: %w", err)
	}
	if !resp.Result {
		return nil, fmt.Errorf("pionex indexes API failed")
	}

	symMap := make(map[string]bool)
	for _, s := range symbols {
		symMap[s] = true
	}

	var out []exchange.FundingRateResult
	for i := range resp.Data.Indexes {
		idx := &resp.Data.Indexes[i]
		if !strings.HasSuffix(idx.Symbol, "_PERP") {
			continue
		}
		if len(symbols) > 0 && !symMap[idx.Symbol] {
			continue
		}

		rate := xjson.ToFloat64(idx.NextFundingRate)
		out = append(out, exchange.FundingRateResult{
			Symbol:     idx.Symbol,
			Rate:       rate,
			SettleTime: idx.NextFundingTime,
		})
	}

	return out, nil
}

// GetPotentialFundingSymbols satisfies the ScannerClient interface.
func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	indexesBody, err := c.rawRequestPublic(ctx, "GET", "/api/v1/market/indexes", nil)
	if err != nil {
		return nil, fmt.Errorf("pionex list indexes: %w", err)
	}

	var indexesResp pionexIndexesResponse
	if err := xjson.Unmarshal(indexesBody, &indexesResp); err != nil {
		return nil, fmt.Errorf("unmarshal pionex indexes: %w", err)
	}

	if !indexesResp.Result {
		return nil, fmt.Errorf("pionex api error: failed to fetch indexes")
	}

	query := map[string]string{
		typeKey: typePerp,
	}
	tickersBody, err := c.rawRequestPublic(ctx, "GET", "/api/v1/market/tickers", query)
	if err != nil {
		return nil, fmt.Errorf("pionex list tickers: %w", err)
	}

	var tickersResp pionexTickersResponse
	if err := xjson.Unmarshal(tickersBody, &tickersResp); err != nil {
		return nil, fmt.Errorf("unmarshal pionex tickers: %w", err)
	}

	if !tickersResp.Result {
		return nil, fmt.Errorf("pionex api error: failed to fetch tickers")
	}

	tickerMap := make(map[string]*pionexTickerItem)
	for i := range tickersResp.Data.Tickers {
		item := &tickersResp.Data.Tickers[i]
		tickerMap[item.Symbol] = item
	}

	whitelistMap := make(map[string]bool)
	for _, sym := range whitelist {
		whitelistMap[strings.ToUpper(sym)] = true
	}

	blacklistMap := make(map[string]bool)
	for _, sym := range blacklist {
		blacklistMap[strings.ToUpper(sym)] = true
	}

	var results []exchange.PotentialFundingResult
	for i := range indexesResp.Data.Indexes {
		item := &indexesResp.Data.Indexes[i]
		if res, ok := matchAndFilter(item, tickerMap, whitelistMap, blacklistMap, minVol24h, maxVol24h); ok {
			results = append(results, res)
		}
	}

	return results, nil
}

func matchAndFilter(
	indexItem *pionexIndexItem,
	tickerMap map[string]*pionexTickerItem,
	whitelistMap, blacklistMap map[string]bool,
	minVol24h, maxVol24h float64,
) (exchange.PotentialFundingResult, bool) {
	// Only support USDT/USDC perpetuals
	if !strings.HasSuffix(indexItem.Symbol, "_USDT_PERP") && !strings.HasSuffix(indexItem.Symbol, "_USDC_PERP") {
		return exchange.PotentialFundingResult{}, false
	}

	sym := strings.ToUpper(indexItem.Symbol)
	if blacklistMap[sym] {
		return exchange.PotentialFundingResult{}, false
	}
	if len(whitelistMap) > 0 && !whitelistMap[sym] {
		return exchange.PotentialFundingResult{}, false
	}

	ticker := tickerMap[indexItem.Symbol]
	if ticker == nil {
		return exchange.PotentialFundingResult{}, false
	}

	vol24h, _ := strconv.ParseFloat(ticker.Amount.String(), 64)
	if minVol24h > 0 && vol24h < minVol24h {
		return exchange.PotentialFundingResult{}, false
	}
	if maxVol24h > 0 && vol24h > maxVol24h {
		return exchange.PotentialFundingResult{}, false
	}

	price, _ := strconv.ParseFloat(ticker.Close.String(), 64)
	if price == 0 {
		price, _ = strconv.ParseFloat(indexItem.MarkPrice.String(), 64)
	}

	rate, _ := strconv.ParseFloat(indexItem.NextFundingRate.String(), 64)

	return exchange.PotentialFundingResult{
		Symbol:     indexItem.Symbol,
		Rate:       rate,
		SettleTime: indexItem.NextFundingTime,
		Volume24h:  vol24h,
		Price:      price,
	}, true
}

// GetRiskTableRaw queries risk limit tiers for futures symbols.
func (c *Client) GetRiskTableRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.rawRequestPublic(ctx, "GET", "/api/v1/common/riskTable", params)
}

// GetSymbolsRaw queries all futures trading pair information.
func (c *Client) GetSymbolsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.rawRequestPublic(ctx, "GET", "/api/v1/common/symbols", params)
}

func (c *Client) GetTopGainer(_ context.Context, _ exchange.TopGainerRequest) ([]exchange.TopGainerResult, error) {
	return nil, nil
}
