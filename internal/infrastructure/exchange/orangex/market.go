package orangex

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
	"crypto-bot/pkg/xjson"
)

type orangexContract struct {
	TickerID                 string       `json:"ticker_id"`
	BaseCurrency             string       `json:"base_currency"`
	TargetCurrency           string       `json:"target_currency"`
	LastPrice                xjson.Number `json:"last_price"`
	BaseVolume               xjson.Number `json:"base_volume"`
	TargetVolume             xjson.Number `json:"target_volume"`
	Bid                      xjson.Number `json:"bid"`
	Ask                      xjson.Number `json:"ask"`
	High                     xjson.Number `json:"high"`
	Low                      xjson.Number `json:"low"`
	ProductType              string       `json:"product_type"`
	OpenInterest             xjson.Number `json:"open_interest"`
	IndexPrice               xjson.Number `json:"index_price"`
	IndexName                string       `json:"index_name"`
	IndexCurrency            string       `json:"index_currency"`
	StartTimestamp           int64        `json:"start_timestamp"`
	EndTimestamp             int64        `json:"end_timestamp"`
	FundingRate              xjson.Number `json:"funding_rate"`
	NextFundingRate          xjson.Number `json:"next_funding_rate"`
	NextFundingRateTimestamp int64        `json:"next_funding_rate_timestamp"`
	ContractType             string       `json:"contract_type"`
	ContractPrice            xjson.Number `json:"contract_price"`
	ContractPriceCurrency    string       `json:"contract_price_currency"`
}

type orangexResponse struct {
	JsonRpc string            `json:"jsonrpc"`
	Result  []orangexContract `json:"result"`
}

func (c *Client) request(ctx context.Context, path string) ([]byte, error) {
	reqURL, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP GET %s: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

func toStandardSymbol(sym string) string {
	s := strings.ToUpper(sym)
	s = strings.TrimSuffix(s, "-PERPETUAL")
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
	body, err := c.request(ctx, "/public/coin_gecko_contracts")
	if err != nil {
		return nil, fmt.Errorf("orangex list coin gecko contracts: %w", err)
	}

	var resp orangexResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal orangex contracts: %w", err)
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
	for i := range resp.Result {
		item := &resp.Result[i]
		if res, ok := matchAndFilter(item, whitelistMap, blacklistMap, minVol24h, maxVol24h); ok {
			results = append(results, res)
		}
	}

	return results, nil
}

func matchAndFilter(
	item *orangexContract,
	whitelistMap, blacklistMap map[string]bool,
	minVol24h, maxVol24h float64,
) (exchange.PotentialFundingResult, bool) {
	if !strings.EqualFold(item.ProductType, "perpetual") {
		return exchange.PotentialFundingResult{}, false
	}
	if !strings.EqualFold(item.TargetCurrency, "USDT") && !strings.EqualFold(item.TargetCurrency, "USDC") {
		return exchange.PotentialFundingResult{}, false
	}

	stdSym := toStandardSymbol(item.TickerID)
	if blacklistMap[stdSym] {
		return exchange.PotentialFundingResult{}, false
	}
	if len(whitelistMap) > 0 && !whitelistMap[stdSym] {
		return exchange.PotentialFundingResult{}, false
	}

	price := xjson.ToFloat64(item.LastPrice)
	vol24h := xjson.ToFloat64(item.TargetVolume)
	if vol24h == 0 {
		baseVol := xjson.ToFloat64(item.BaseVolume)
		vol24h = baseVol * price
	}

	if minVol24h > 0 && vol24h < minVol24h {
		return exchange.PotentialFundingResult{}, false
	}
	if maxVol24h > 0 && vol24h > maxVol24h {
		return exchange.PotentialFundingResult{}, false
	}

	rate := xjson.ToFloat64(item.FundingRate)

	return exchange.PotentialFundingResult{
		Symbol:     item.TickerID,
		Rate:       rate,
		SettleTime: item.NextFundingRateTimestamp,
		Volume24h:  vol24h,
		Price:      price,
	}, true
}

type orangexTicker struct {
	InstrumentName string       `json:"instrument_name"`
	BestBidPrice   xjson.Number `json:"best_bid_price"`
	BestAskPrice   xjson.Number `json:"best_ask_price"`
	LastPrice      xjson.Number `json:"last_price"`
	Volume24h      xjson.Number `json:"volume_24h"`
}

type orangexInstrument struct {
	InstrumentName string       `json:"instrument_name"`
	BaseCurrency   string       `json:"base_currency"`
	QuoteCurrency  string       `json:"quote_currency"`
	TickSize       xjson.Number `json:"tick_size"`
	MinTradeAmount xjson.Number `json:"min_trade_amount"`
	ContractSize   xjson.Number `json:"contract_size"`
	FundingRate    xjson.Number `json:"funding_rate"`
	NextFunding    int64        `json:"next_funding_rate_timestamp"`
}

func (c *Client) rawGetTickers(ctx context.Context, instrument string) ([]orangexTicker, error) {
	params := map[string]string{}
	if instrument != "" {
		params["instrument_name"] = instrument
	}
	resp, err := c.postRPC(ctx, "/public/tickers", "/public/tickers", params, false)
	if err != nil {
		return nil, err
	}
	var envelope orangexRPCResponse[[]orangexTicker]
	if err := xjson.Unmarshal(resp, &envelope); err != nil {
		return nil, err
	}
	if envelope.Error != nil {
		return nil, envelope.Error
	}
	return envelope.Result, nil
}

func (c *Client) rawGetInstruments(ctx context.Context, kind string) ([]orangexInstrument, error) {
	params := map[string]string{
		"kind": kind,
	}
	resp, err := c.postRPC(ctx, "/public/get_instruments", "/public/get_instruments", params, false)
	if err != nil {
		return nil, err
	}
	var envelope orangexRPCResponse[[]orangexInstrument]
	if err := xjson.Unmarshal(resp, &envelope); err != nil {
		return nil, err
	}
	if envelope.Error != nil {
		return nil, envelope.Error
	}
	return envelope.Result, nil
}

func (c *Client) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	if symbol != "" {
		res, err := c.rawGetTickers(ctx, symbol)
		if err != nil {
			return nil, err
		}
		var out []exchange.Ticker
		for _, t := range res {
			lastVal := xjson.ToFloat64(t.LastPrice)
			volVal := xjson.ToFloat64(t.Volume24h)
			out = append(out, exchange.Ticker{
				Symbol:       t.InstrumentName,
				Bid1:         xjson.ToFloat64(t.BestBidPrice),
				Ask1:         xjson.ToFloat64(t.BestAskPrice),
				LastPrice:    lastVal,
				Volume24:     volVal,
				AmountUSDT24: volVal * lastVal,
			})
		}
		return out, nil
	}

	// If symbol is empty, use the batch coin_gecko_contracts API to get last prices in one call, avoiding rate limits.
	body, err := c.request(ctx, "/public/coin_gecko_contracts")
	if err != nil {
		return nil, fmt.Errorf("orangex get coin gecko contracts: %w", err)
	}

	var resp orangexResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal orangex contracts: %w", err)
	}

	var out []exchange.Ticker
	for i := range resp.Result {
		item := &resp.Result[i]
		if !strings.EqualFold(item.ProductType, "perpetual") {
			continue
		}
		price := xjson.ToFloat64(item.LastPrice)
		baseVol := xjson.ToFloat64(item.BaseVolume)
		quoteVol := xjson.ToFloat64(item.TargetVolume)
		if quoteVol == 0 && baseVol > 0 {
			quoteVol = baseVol * price
		}
		if baseVol == 0 && quoteVol > 0 && price > 0 {
			baseVol = quoteVol / price
		}

		bidVal := xjson.ToFloat64(item.Bid)
		if bidVal == 0 {
			bidVal = price
		}
		askVal := xjson.ToFloat64(item.Ask)
		if askVal == 0 {
			askVal = price
		}

		out = append(out, exchange.Ticker{
			Symbol:       item.TickerID,
			Bid1:         bidVal,
			Ask1:         askVal,
			LastPrice:    price,
			Volume24:     baseVol,
			AmountUSDT24: quoteVol,
		})
	}
	return out, nil
}

func (c *Client) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	res, err := c.rawGetInstruments(ctx, "perpetual")
	if err != nil {
		return nil, err
	}
	var out []exchange.ContractDetail
	for _, inst := range res {
		contractSize := xjson.ToFloat64(inst.ContractSize)
		if contractSize <= 0 {
			contractSize = 1.0
		}
		priceUnit := xjson.ToFloat64(inst.TickSize)
		minVolVal := xjson.ToFloat64(inst.MinTradeAmount)

		out = append(out, exchange.ContractDetail{
			Symbol:        inst.InstrumentName,
			DisplayName:   inst.InstrumentName,
			DisplayNameEn: inst.InstrumentName,
			BaseCoin:      inst.BaseCurrency,
			QuoteCoin:     inst.QuoteCurrency,
			SettleCoin:    inst.QuoteCurrency,
			ContractSize:  contractSize,
			MinLeverage:   1,
			MaxLeverage:   100,
			PriceUnit:     priceUnit,
			MinVol:        int(minVolVal),
			VolUnit:       int(minVolVal),
			PriceScale:    decmath.DecimalPlaces(inst.TickSize.String()),
			VolScale:      decmath.DecimalPlaces(inst.MinTradeAmount.String()),
			State:         1,
		})
	}
	return out, nil
}

func (c *Client) GetFundingRates(ctx context.Context, symbols []string) ([]exchange.FundingRateResult, error) {
	body, err := c.request(ctx, "/public/coin_gecko_contracts")
	if err != nil {
		return nil, fmt.Errorf("orangex get funding rates: %w", err)
	}

	var resp orangexResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal funding rates: %w", err)
	}

	var out []exchange.FundingRateResult
	symMap := make(map[string]bool)
	for _, s := range symbols {
		symMap[s] = true
	}

	for i := range resp.Result {
		item := &resp.Result[i]
		if !strings.EqualFold(item.ProductType, "perpetual") {
			continue
		}
		if len(symbols) > 0 && !symMap[item.TickerID] {
			continue
		}
		rate := xjson.ToFloat64(item.FundingRate)
		out = append(out, exchange.FundingRateResult{
			Symbol:     item.TickerID,
			Rate:       rate,
			SettleTime: item.NextFundingRateTimestamp,
		})
	}
	return out, nil
}
