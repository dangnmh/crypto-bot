package xt

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type xtContract struct {
	ID                       int64  `json:"id"`
	Symbol                   string `json:"symbol"`
	TickerID                 string `json:"ticker_id"`
	BaseCurrency             string `json:"base_currency"`
	TargetCurrency           string `json:"target_currency"`
	LastPrice                string `json:"last_price"`
	BaseVolume               string `json:"base_volume"`
	TargetVolume             string `json:"target_volume"`
	Bid                      string `json:"bid"`
	Ask                      string `json:"ask"`
	ProductType              string `json:"product_type"`
	FundingRate              string `json:"funding_rate"`
	NextFundingRate          string `json:"next_funding_rate"`
	NextFundingRateTimestamp int64  `json:"next_funding_rate_timestamp"`
}

type xtSymbolInfo struct {
	Symbol            string `json:"symbol"`
	BaseCoin          string `json:"baseCoin"`
	QuoteCoin         string `json:"quoteCoin"`
	ContractSize      string `json:"contractSize"`
	PricePrecision    int    `json:"pricePrecision"`
	QuantityPrecision int    `json:"quantityPrecision"`
	MinQty            string `json:"minQty"`
	State             int    `json:"state"`
}

type xtSymbolListResponse struct {
	ReturnCode int64          `json:"returnCode"`
	MsgInfo    string         `json:"msgInfo"`
	Result     []xtSymbolInfo `json:"result"`
}

func toStandardSymbol(sym string) string {
	s := strings.ToUpper(sym)
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "_", "")
	return s
}

func cleanXTSymbol(symbol string) string {
	sym := strings.ToLower(symbol)
	if !strings.Contains(sym, "_") {
		if before, ok := strings.CutSuffix(sym, "usdt"); ok {
			sym = before + "_usdt"
		} else if before, ok := strings.CutSuffix(sym, "usdc"); ok {
			sym = before + "_usdc"
		}
	}
	return sym
}

// GetTickers satisfies the Client interface.
func (c *Client) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	body, err := c.request(ctx, "GET", "/future/market/v1/public/cg/contracts", nil, nil, false)
	if err != nil {
		return nil, fmt.Errorf("xt GetTickers contracts: %w", err)
	}

	var rawContracts []xtContract
	if err := xjson.Unmarshal(body, &rawContracts); err != nil {
		return nil, fmt.Errorf("unmarshal contracts: %w", err)
	}

	var targetStd string
	if symbol != "" {
		targetStd = toStandardSymbol(symbol)
	}

	var results []exchange.Ticker
	for i := range rawContracts {
		item := &rawContracts[i]
		if !strings.EqualFold(item.ProductType, "PERPETUAL") {
			continue
		}
		if !strings.EqualFold(item.TargetCurrency, "USDT") && !strings.EqualFold(item.TargetCurrency, "USDC") {
			continue
		}

		std := toStandardSymbol(item.TickerID)
		if targetStd != "" && std != targetStd {
			continue
		}

		lastPrice, _ := strconv.ParseFloat(item.LastPrice, 64)
		bid, _ := strconv.ParseFloat(item.Bid, 64)
		ask, _ := strconv.ParseFloat(item.Ask, 64)
		vol, _ := strconv.ParseFloat(item.TargetVolume, 64)

		results = append(results, exchange.Ticker{
			Symbol:       std,
			LastPrice:    lastPrice,
			Bid1:         bid,
			Ask1:         ask,
			Volume24:     vol,
			AmountUSDT24: vol,
			Timestamp:    item.NextFundingRateTimestamp,
		})
	}

	return results, nil
}

// GetContractDetails satisfies the Client interface.
func (c *Client) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	body, err := c.request(ctx, "GET", "/future/market/v1/public/symbol/list", nil, nil, false)
	if err != nil {
		return nil, fmt.Errorf("xt GetContractDetails: %w", err)
	}

	var resp xtSymbolListResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal symbol list: %w", err)
	}

	var results []exchange.ContractDetail
	for i := range resp.Result {
		item := &resp.Result[i]
		if item.State != 0 {
			continue
		}
		targetUpper := strings.ToUpper(item.QuoteCoin)
		if targetUpper != "USDT" && targetUpper != "USDC" {
			continue
		}

		std := toStandardSymbol(item.Symbol)
		contractSize, _ := strconv.ParseFloat(item.ContractSize, 64)
		minQty, _ := strconv.ParseFloat(item.MinQty, 64)
		priceUnit := math.Pow10(-item.PricePrecision)

		results = append(results, exchange.ContractDetail{
			Symbol:        std,
			DisplayName:   item.Symbol,
			DisplayNameEn: item.Symbol,
			BaseCoin:      strings.ToUpper(item.BaseCoin),
			QuoteCoin:     targetUpper,
			SettleCoin:    targetUpper,
			ContractSize:  contractSize,
			MinLeverage:   1,
			MaxLeverage:   100,
			PriceScale:    item.PricePrecision,
			VolScale:      item.QuantityPrecision,
			AmountScale:   item.QuantityPrecision,
			MinVol:        int(minQty),
			PriceUnit:     priceUnit,
		})
	}

	return results, nil
}

// GetFundingRates satisfies the Client interface.
func (c *Client) GetFundingRates(ctx context.Context, symbols []string) ([]exchange.FundingRateResult, error) {
	body, err := c.request(ctx, "GET", "/future/market/v1/public/cg/contracts", nil, nil, false)
	if err != nil {
		return nil, fmt.Errorf("xt GetFundingRates contracts: %w", err)
	}

	var rawContracts []xtContract
	if err := xjson.Unmarshal(body, &rawContracts); err != nil {
		return nil, fmt.Errorf("unmarshal contracts: %w", err)
	}

	filter := make(map[string]bool)
	for _, s := range symbols {
		filter[toStandardSymbol(s)] = true
	}

	var results []exchange.FundingRateResult
	for i := range rawContracts {
		item := &rawContracts[i]
		if !strings.EqualFold(item.ProductType, "PERPETUAL") {
			continue
		}
		std := toStandardSymbol(item.TickerID)
		if len(filter) > 0 && !filter[std] {
			continue
		}

		rate, _ := strconv.ParseFloat(item.FundingRate, 64)

		results = append(results, exchange.FundingRateResult{
			Symbol:     std,
			Rate:       rate,
			SettleTime: item.NextFundingRateTimestamp,
		})
	}

	return results, nil
}

// GetPotentialFundingSymbols satisfies the ScannerClient interface in tools.
func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	body, err := c.request(ctx, "GET", "/future/market/v1/public/cg/contracts", nil, nil, false)
	if err != nil {
		return nil, fmt.Errorf("xt list contracts: %w", err)
	}

	var rawContracts []xtContract
	if err := xjson.Unmarshal(body, &rawContracts); err != nil {
		return nil, fmt.Errorf("unmarshal xt contracts response: %w", err)
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
	for i := range rawContracts {
		item := &rawContracts[i]
		if res, ok := c.filterContract(item, whitelistMap, blacklistMap, minVol24h, maxVol24h); ok {
			results = append(results, res)
		}
	}

	return results, nil
}

func (c *Client) filterContract(
	item *xtContract,
	whitelistMap, blacklistMap map[string]bool,
	minVol24h, maxVol24h float64,
) (exchange.PotentialFundingResult, bool) {
	if !strings.EqualFold(item.ProductType, "PERPETUAL") {
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

func (c *Client) GetTopGainer(_ context.Context, _ exchange.TopGainerRequest) ([]exchange.TopGainerResult, error) {
	return nil, nil
}
