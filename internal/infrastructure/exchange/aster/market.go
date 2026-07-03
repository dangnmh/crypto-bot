package aster

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
	"crypto-bot/pkg/xjson"
)

type aster24hrTicker struct {
	Symbol      string `json:"symbol"`
	LastPrice   string `json:"lastPrice"`
	Volume      string `json:"volume"`
	QuoteVolume string `json:"quoteVolume"`
	CloseTime   int64  `json:"closeTime"`
}

type asterBookTicker struct {
	Symbol   string `json:"symbol"`
	BidPrice string `json:"bidPrice"`
	AskPrice string `json:"askPrice"`
	Time     int64  `json:"time"`
}

type asterFilter struct {
	FilterType string `json:"filterType"`
	MinPrice   string `json:"minPrice,omitempty"`
	MaxPrice   string `json:"maxPrice,omitempty"`
	TickSize   string `json:"tickSize,omitempty"`
	MinQty     string `json:"minQty,omitempty"`
	MaxQty     string `json:"maxQty,omitempty"`
	StepSize   string `json:"stepSize,omitempty"`
}

type asterSymbol struct {
	Symbol            string        `json:"symbol"`
	BaseAsset         string        `json:"baseAsset"`
	QuoteAsset        string        `json:"quoteAsset"`
	MarginAsset       string        `json:"marginAsset"`
	PricePrecision    int           `json:"pricePrecision"`
	QuantityPrecision int           `json:"quantityPrecision"`
	Filters           []asterFilter `json:"filters"`
}

type asterExchangeInfo struct {
	Symbols []asterSymbol `json:"symbols"`
}

type asterPremiumIndex struct {
	Symbol          string `json:"symbol"`
	LastFundingRate string `json:"lastFundingRate"`
	NextFundingTime int64  `json:"nextFundingTime"`
}

type asterLeverageBracket struct {
	Symbol   string `json:"symbol"`
	Brackets []struct {
		Bracket          int     `json:"bracket"`
		InitialLeverage  int     `json:"initialLeverage"`
		NotionalCap      float64 `json:"notionalCap"`
		NotionalFloor    float64 `json:"notionalFloor"`
		MaintMarginRatio float64 `json:"maintMarginRatio"`
		Cum              float64 `json:"cum"`
	} `json:"brackets"`
}

// Raw methods.
func rawGetTickerList[T any](ctx context.Context, c *Client, path, symbol string) ([]T, error) {
	params := make(map[string]string)
	if symbol != "" {
		params[paramSymbol] = symbol
	}
	body, err := c.request(ctx, http.MethodGet, path, params, false)
	if err != nil {
		return nil, err
	}
	if symbol != "" {
		var single T
		if err := xjson.Unmarshal(body, &single); err != nil {
			return nil, err
		}
		return []T{single}, nil
	}
	var list []T
	if err := xjson.Unmarshal(body, &list); err != nil {
		return nil, err
	}
	return list, nil
}

func (c *Client) rawGet24hrTickers(ctx context.Context, symbol string) ([]aster24hrTicker, error) {
	return rawGetTickerList[aster24hrTicker](ctx, c, "/fapi/v3/ticker/24hr", symbol)
}

func (c *Client) rawGetBookTickers(ctx context.Context, symbol string) ([]asterBookTicker, error) {
	return rawGetTickerList[asterBookTicker](ctx, c, "/fapi/v3/ticker/bookTicker", symbol)
}

func (c *Client) rawGetLeverageBrackets(ctx context.Context) ([]asterLeverageBracket, error) {
	body, err := c.request(ctx, http.MethodGet, "/fapi/v3/leverageBracket", nil, true)
	if err != nil {
		return nil, err
	}
	var resp []asterLeverageBracket
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// GetTickers implements MarketDataProvider.
func (c *Client) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	stats, err := c.rawGet24hrTickers(ctx, symbol)
	if err != nil {
		return nil, err
	}
	books, err := c.rawGetBookTickers(ctx, symbol)
	if err != nil {
		return nil, err
	}

	bookMap := make(map[string]asterBookTicker)
	for i := range books {
		bookMap[books[i].Symbol] = books[i]
	}

	tickers := make([]exchange.Ticker, 0, len(stats))
	for i := range stats {
		stat := &stats[i]
		book, hasBook := bookMap[stat.Symbol]

		last, _ := strconv.ParseFloat(stat.LastPrice, 64)
		vol, _ := strconv.ParseFloat(stat.Volume, 64)
		amt, _ := strconv.ParseFloat(stat.QuoteVolume, 64)

		bid := last
		ask := last
		ts := stat.CloseTime

		if hasBook {
			bid, _ = strconv.ParseFloat(book.BidPrice, 64)
			ask, _ = strconv.ParseFloat(book.AskPrice, 64)
			ts = book.Time
		}

		tickers = append(tickers, exchange.Ticker{
			Symbol:       strings.ToUpper(stat.Symbol),
			LastPrice:    last,
			Bid1:         bid,
			Ask1:         ask,
			Volume24:     vol,
			AmountUSDT24: amt,
			Timestamp:    ts,
		})
	}
	return tickers, nil
}

// GetContractDetails implements MarketDataProvider.
func (c *Client) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	body, err := c.request(ctx, http.MethodGet, "/fapi/v3/exchangeInfo", nil, false)
	if err != nil {
		return nil, err
	}
	var info asterExchangeInfo
	if err := xjson.Unmarshal(body, &info); err != nil {
		return nil, err
	}

	maxLevMap := make(map[string]int)
	if c.apiKey != "" && c.apiSecret != "" {
		if brackets, err := c.rawGetLeverageBrackets(ctx); err == nil {
			for i := range brackets {
				b := &brackets[i]
				if len(b.Brackets) > 0 {
					maxLevMap[strings.ToUpper(b.Symbol)] = b.Brackets[0].InitialLeverage
				}
			}
		} else {
			c.logger.WarnContext(ctx, "failed to fetch aster leverage brackets", "error", err)
		}
	}

	details := make([]exchange.ContractDetail, 0, len(info.Symbols))
	for i := range info.Symbols {
		sym := &info.Symbols[i]

		priceUnit := 0.0
		minVol := 0.0
		volUnit := 1.0

		for _, f := range sym.Filters {
			switch f.FilterType {
			case "PRICE_FILTER":
				priceUnit = decmath.ParseFloat(f.TickSize)
			case "LOT_SIZE":
				minVol = decmath.ParseFloat(f.MinQty)
				volUnit = decmath.ParseFloat(f.StepSize)
			}
		}

		maxLeverage := 100
		if val, ok := maxLevMap[strings.ToUpper(sym.Symbol)]; ok {
			maxLeverage = val
		}

		details = append(details, exchange.ContractDetail{
			Symbol:        strings.ToUpper(sym.Symbol),
			DisplayName:   sym.Symbol,
			DisplayNameEn: sym.Symbol,
			BaseCoin:      strings.ToUpper(sym.BaseAsset),
			QuoteCoin:     strings.ToUpper(sym.QuoteAsset),
			SettleCoin:    strings.ToUpper(sym.MarginAsset),
			ContractSize:  1.0,
			MinLeverage:   1,
			MaxLeverage:   maxLeverage,
			PriceScale:    sym.PricePrecision,
			VolScale:      sym.QuantityPrecision,
			PriceUnit:     priceUnit,
			VolUnit:       int(volUnit),
			MinVol:        int(minVol),
			State:         1,
		})
	}
	return details, nil
}

// GetFundingRates implements MarketDataProvider.
func (c *Client) GetFundingRates(ctx context.Context, symbols []string) ([]exchange.FundingRateResult, error) {
	body, err := c.request(ctx, http.MethodGet, "/fapi/v3/premiumIndex", nil, false)
	if err != nil {
		return nil, err
	}

	var list []asterPremiumIndex
	if err := xjson.Unmarshal(body, &list); err != nil {
		return nil, err
	}

	rateMap := make(map[string]float64)
	timeMap := make(map[string]int64)
	for i := range list {
		rate, _ := strconv.ParseFloat(list[i].LastFundingRate, 64)
		rateMap[strings.ToUpper(list[i].Symbol)] = rate
		timeMap[strings.ToUpper(list[i].Symbol)] = list[i].NextFundingTime
	}

	results := make([]exchange.FundingRateResult, 0, len(symbols))
	for _, s := range symbols {
		upper := strings.ToUpper(s)
		if rate, has := rateMap[upper]; has {
			results = append(results, exchange.FundingRateResult{
				Symbol:     upper,
				Rate:       rate,
				SettleTime: timeMap[upper],
			})
		}
	}
	return results, nil
}

func toStandardSymbol(sym string) string {
	s := strings.ToUpper(sym)
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "_", "")
	return s
}

// GetPotentialFundingSymbols satisfies the ScannerClient interface.
func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	tickerBody, err := c.request(ctx, http.MethodGet, "/fapi/v3/ticker/24hr", nil, false)
	if err != nil {
		return nil, err
	}

	var tickers []aster24hrTicker
	if err := xjson.Unmarshal(tickerBody, &tickers); err != nil {
		return nil, err
	}

	indexBody, err := c.request(ctx, http.MethodGet, "/fapi/v3/premiumIndex", nil, false)
	if err != nil {
		return nil, err
	}

	var indexes []asterPremiumIndex
	if err := xjson.Unmarshal(indexBody, &indexes); err != nil {
		return nil, err
	}

	indexMap := make(map[string]asterPremiumIndex)
	for i := range indexes {
		indexMap[indexes[i].Symbol] = indexes[i]
	}

	whitelistMap := make(map[string]bool)
	for _, sym := range whitelist {
		whitelistMap[toStandardSymbol(sym)] = true
	}

	blacklistMap := make(map[string]bool)
	for _, sym := range blacklist {
		blacklistMap[toStandardSymbol(sym)] = true
	}

	var results []exchange.PotentialFundingResult
	for i := range tickers {
		ticker := &tickers[i]
		if res, ok := matchAndFilter(ticker, indexMap, whitelistMap, blacklistMap, minVol24h, maxVol24h); ok {
			results = append(results, res)
		}
	}

	return results, nil
}

func matchAndFilter(
	ticker *aster24hrTicker,
	indexMap map[string]asterPremiumIndex,
	whitelistMap, blacklistMap map[string]bool,
	minVol24h, maxVol24h float64,
) (exchange.PotentialFundingResult, bool) {
	stdSym := toStandardSymbol(ticker.Symbol)
	if blacklistMap[stdSym] {
		return exchange.PotentialFundingResult{}, false
	}
	if len(whitelistMap) > 0 && !whitelistMap[stdSym] {
		return exchange.PotentialFundingResult{}, false
	}

	vol24h, _ := strconv.ParseFloat(ticker.QuoteVolume, 64)
	if minVol24h > 0 && vol24h < minVol24h {
		return exchange.PotentialFundingResult{}, false
	}
	if maxVol24h > 0 && vol24h > maxVol24h {
		return exchange.PotentialFundingResult{}, false
	}

	idxItem, ok := indexMap[ticker.Symbol]
	if !ok {
		return exchange.PotentialFundingResult{}, false
	}

	price, _ := strconv.ParseFloat(ticker.LastPrice, 64)
	rate, _ := strconv.ParseFloat(idxItem.LastFundingRate, 64)

	return exchange.PotentialFundingResult{
		Symbol:     stdSym,
		Rate:       rate,
		SettleTime: idxItem.NextFundingTime,
		Volume24h:  vol24h,
		Price:      price,
	}, true
}
