package orangex

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
	"crypto-bot/pkg/xjson"
)

type orangexContract struct {
	TickerID                 string `json:"ticker_id"`
	BaseCurrency             string `json:"base_currency"`
	TargetCurrency           string `json:"target_currency"`
	LastPrice                string `json:"last_price"`
	TargetVolume             string `json:"target_volume"`
	ProductType              string `json:"product_type"`
	FundingRate              string `json:"funding_rate"`
	NextFundingRateTimestamp int64  `json:"next_funding_rate_timestamp"`
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

	vol24h, _ := strconv.ParseFloat(item.TargetVolume, 64)
	if minVol24h > 0 && vol24h < minVol24h {
		return exchange.PotentialFundingResult{}, false
	}
	if maxVol24h > 0 && vol24h > maxVol24h {
		return exchange.PotentialFundingResult{}, false
	}

	price, _ := strconv.ParseFloat(item.LastPrice, 64)
	rate, _ := strconv.ParseFloat(item.FundingRate, 64)

	return exchange.PotentialFundingResult{
		Symbol:     stdSym,
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
	res, err := c.rawGetTickers(ctx, symbol)
	if err != nil {
		return nil, err
	}
	var out []exchange.Ticker
	for _, t := range res {
		out = append(out, exchange.Ticker{
			Symbol:    t.InstrumentName,
			Bid1:      xjson.ToFloat64(t.BestBidPrice),
			Ask1:      xjson.ToFloat64(t.BestAskPrice),
			LastPrice: xjson.ToFloat64(t.LastPrice),
			Volume24:  xjson.ToFloat64(t.Volume24h),
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
		out = append(out, exchange.ContractDetail{
			Symbol:     inst.InstrumentName,
			PriceScale: decmath.DecimalPlaces(inst.TickSize.String()),
			VolScale:   decmath.DecimalPlaces(inst.MinTradeAmount.String()),
		})
	}
	return out, nil
}

func (c *Client) GetFundingRates(ctx context.Context, symbols []string) ([]exchange.FundingRateResult, error) {
	res, err := c.rawGetInstruments(ctx, "perpetual")
	if err != nil {
		return nil, err
	}
	var out []exchange.FundingRateResult
	symMap := make(map[string]bool)
	for _, s := range symbols {
		symMap[s] = true
	}
	for _, inst := range res {
		if len(symbols) > 0 && !symMap[inst.InstrumentName] {
			continue
		}
		out = append(out, exchange.FundingRateResult{
			Symbol:     inst.InstrumentName,
			Rate:       xjson.ToFloat64(inst.FundingRate),
			SettleTime: inst.NextFunding,
		})
	}
	return out, nil
}
