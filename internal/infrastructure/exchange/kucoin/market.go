package kucoin

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
	"crypto-bot/pkg/xjson"
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
	LastTradePrice          float64 `json:"lastTradePrice"`
	MarkPrice               float64 `json:"markPrice"`
	IndexPrice              float64 `json:"indexPrice"`
	PriceChgPct             float64 `json:"priceChgPct"`
	ChangeRate24h           float64 `json:"changeRate24h"`
	ChangePrice24h          float64 `json:"changePrice24h"`
}

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
	Symbol             string `json:"symbol"`
	BestBidPrice       string `json:"bestBidPrice"`
	BestAskPrice       string `json:"bestAskPrice"`
	LastPrice          string `json:"lastPrice"`
	Price              string `json:"price"`
	Volume             string `json:"volume"`
	Vol                string `json:"vol"`
	PriceChangePercent string `json:"priceChangePercent"`
	PriceChgPct        string `json:"priceChgPct"`
	ChangeRate         string `json:"changeRate"`
	ChangePrice        string `json:"changePrice"`
	Ts                 int64  `json:"ts"`
}

// Private raw methods invoking the KuCoin REST API.

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

	tickers, err := c.getRawTickers(ctx, kucoinTickersRequest{})
	if err != nil {
		return nil, err
	}

	priceMap := make(map[string]float64)
	for i := range tickers {
		t := &tickers[i]
		last := decmath.ParseFloat(t.LastPrice)
		if last == 0 {
			last = decmath.ParseFloat(t.Price)
		}
		priceMap[t.Symbol] = last
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
			Price:      priceMap[inst.Symbol],
		})
	}

	return results, nil
}

func extractKuCoinLastPrice(inst *kucoinContract, t *kucoinTicker, hasTicker bool) float64 {
	last := inst.LastTradePrice
	if hasTicker {
		tLast := decmath.ParseFloat(t.LastPrice)
		if tLast == 0 {
			tLast = decmath.ParseFloat(t.Price)
		}
		if tLast > 0 {
			last = tLast
		}
	}
	if last <= 0 {
		last = inst.MarkPrice
		if last <= 0 {
			last = inst.IndexPrice
		}
	}
	return last
}

func extractKuCoinGainPct(inst *kucoinContract, t *kucoinTicker, hasTicker bool) float64 {
	gainPct := inst.PriceChgPct
	if gainPct == 0 {
		gainPct = inst.ChangeRate24h
	}
	if hasTicker && gainPct == 0 {
		tGain := decmath.ParseFloat(t.PriceChangePercent)
		if tGain == 0 {
			tGain = decmath.ParseFloat(t.PriceChgPct)
		}
		if tGain == 0 {
			tGain = decmath.ParseFloat(t.ChangeRate)
		}
		gainPct = tGain
	}
	if gainPct != 0 && math.Abs(gainPct) < 2.0 {
		gainPct *= 100.0
	}
	return gainPct
}

func buildKuCoinTopGainer(inst *kucoinContract, tickerMap map[string]kucoinTicker) (exchange.TopGainerResult, bool) {
	if inst.Status != statusOpen {
		return exchange.TopGainerResult{}, false
	}

	t, hasTicker := tickerMap[inst.Symbol]
	last := extractKuCoinLastPrice(inst, &t, hasTicker)
	if last <= 0 {
		return exchange.TopGainerResult{}, false
	}

	bid := 0.0
	ask := 0.0
	ts := time.Now().UnixMilli()
	if hasTicker {
		bid = decmath.ParseFloat(t.BestBidPrice)
		ask = decmath.ParseFloat(t.BestAskPrice)
		if t.Ts > 0 {
			ts = t.Ts
		}
	}

	volUSDT := inst.TurnoverOf24h
	if volUSDT == 0 && inst.VolumeOf24h > 0 {
		volUSDT = inst.VolumeOf24h * last
	}

	spreadPct := 0.0
	if bid > 0 && ask > 0 {
		spreadPct = ((ask - bid) / bid) * 100.0
	}

	return exchange.TopGainerResult{
		Symbol:        inst.Symbol,
		LastPrice:     last,
		Bid1:          bid,
		Ask1:          ask,
		Volume24hUSDT: volUSDT,
		Gain24hPct:    extractKuCoinGainPct(inst, &t, hasTicker),
		SpreadPct:     spreadPct,
		Timestamp:     ts,
	}, true
}

// GetTopGainer returns tickers sorted by 24h price change percentage descending.
func (c *Client) GetTopGainer(ctx context.Context, req exchange.TopGainerRequest) ([]exchange.TopGainerResult, error) {
	contracts, err := c.getRawContractDetails(ctx, kucoinContractsRequest{})
	if err != nil {
		return nil, fmt.Errorf("kucoin get top gainer contracts: %w", err)
	}

	tickers, err := c.getRawTickers(ctx, kucoinTickersRequest{})
	if err != nil {
		return nil, fmt.Errorf("kucoin get top gainer tickers: %w", err)
	}

	tickerMap := make(map[string]kucoinTicker, len(tickers))
	for i := range tickers {
		tickerMap[tickers[i].Symbol] = tickers[i]
	}

	results := make([]exchange.TopGainerResult, 0, len(contracts))
	for i := range contracts {
		if res, ok := buildKuCoinTopGainer(&contracts[i], tickerMap); ok {
			results = append(results, res)
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Gain24hPct > results[j].Gain24hPct
	})

	if req.Limit > 0 && req.Limit < len(results) {
		results = results[:req.Limit]
	}

	return results, nil
}

type kucoinDepthRawData struct {
	Sequence int64            `json:"sequence"`
	Symbol   string           `json:"symbol"`
	Bids     [][]xjson.Number `json:"bids"`
	Asks     [][]xjson.Number `json:"asks"`
	Ts       int64            `json:"ts"`
}

func parseKucoinDepthEntries(rawBids, rawAsks [][]xjson.Number) ([]domain.OrderBookEntry, []domain.OrderBookEntry) {
	bids := make([]domain.OrderBookEntry, 0, len(rawBids))
	for _, b := range rawBids {
		if len(b) >= 2 {
			p, v := xjson.ToFloat64(b[0]), xjson.ToFloat64(b[1])
			if p > 0 && v > 0 {
				bids = append(bids, domain.OrderBookEntry{Price: p, Volume: v})
			}
		}
	}

	asks := make([]domain.OrderBookEntry, 0, len(rawAsks))
	for _, a := range rawAsks {
		if len(a) >= 2 {
			p, v := xjson.ToFloat64(a[0]), xjson.ToFloat64(a[1])
			if p > 0 && v > 0 {
				asks = append(asks, domain.OrderBookEntry{Price: p, Volume: v})
			}
		}
	}

	return bids, asks
}

// GetDepth implements exchange.DepthProvider.
// It retrieves the current full Level 2 depth snapshot for a symbol via KuCoin REST API.
func (c *Client) GetDepth(ctx context.Context, symbol string) (*domain.OrderBook, error) {
	params := map[string]string{
		paramSymbol: symbol,
	}
	body, err := c.RawRequest(ctx, http.MethodGet, pathDepthSnapshot, params, nil)
	if err != nil {
		return nil, fmt.Errorf("kucoin get depth for %s: %w", symbol, err)
	}

	data, err := ParseResponse[kucoinDepthRawData](body, "depth_snapshot")
	if err != nil {
		return nil, fmt.Errorf("kucoin parse depth snapshot for %s: %w", symbol, err)
	}

	bids, asks := parseKucoinDepthEntries(data.Bids, data.Asks)

	return &domain.OrderBook{
		Symbol:  symbol,
		Version: data.Sequence,
		Bids:    bids,
		Asks:    asks,
	}, nil
}

// GetDepthCommits implements exchange.DepthCommitsProvider.
// It retrieves part orderbook depth data via /api/v1/level2/depth{size} (e.g. depth20 or depth100).
func (c *Client) GetDepthCommits(ctx context.Context, symbol string, limit int) ([]exchange.DepthCommit, error) {
	path := pathDepth100
	if limit > 0 && limit <= 20 {
		path = pathDepth20
	}

	params := map[string]string{
		paramSymbol: symbol,
	}
	body, err := c.RawRequest(ctx, http.MethodGet, path, params, nil)
	if err != nil {
		return nil, fmt.Errorf("kucoin get depth commits for %s: %w", symbol, err)
	}

	data, err := ParseResponse[kucoinDepthRawData](body, "depth_commits")
	if err != nil {
		return nil, fmt.Errorf("kucoin parse depth commits for %s: %w", symbol, err)
	}

	bids, asks := parseKucoinDepthEntries(data.Bids, data.Asks)

	return []exchange.DepthCommit{
		{
			Version: data.Sequence,
			Bids:    bids,
			Asks:    asks,
		},
	}, nil
}
