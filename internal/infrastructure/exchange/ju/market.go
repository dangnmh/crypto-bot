package ju

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type juContract struct {
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

func (c *Client) request(ctx context.Context, path string) ([]byte, error) {
	if c.limiter != nil {
		if err := c.limiter.Acquire(ctx, path); err != nil {
			return nil, fmt.Errorf("rate limit acquire: %w", err)
		}
	}

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
	s = strings.TrimSuffix(s, "PERPETUAL")
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
	body, err := c.request(ctx, "/v1/future-u/market/public/cg/contracts")
	if err != nil {
		return nil, fmt.Errorf("ju list cg contracts: %w", err)
	}

	var rawMap map[string]*juContract
	if err := xjson.Unmarshal(body, &rawMap); err != nil {
		// try wrapped response
		var wrapped struct {
			Code int                    `json:"code"`
			Data map[string]*juContract `json:"data"`
		}
		if err2 := xjson.Unmarshal(body, &wrapped); err2 != nil {
			return nil, fmt.Errorf("unmarshal ju contracts: %w (fallback: %s)", err, err2.Error())
		}
		rawMap = wrapped.Data
	}

	results := c.filterContracts(rawMap, minVol24h, maxVol24h, whitelist, blacklist)
	return results, nil
}

func (c *Client) filterContracts(
	rawMap map[string]*juContract,
	minVol24h, maxVol24h float64,
	whitelist, blacklist []string,
) []exchange.PotentialFundingResult {
	whitelistMap := make(map[string]bool)
	for _, sym := range whitelist {
		whitelistMap[toStandardSymbol(sym)] = true
	}

	blacklistMap := make(map[string]bool)
	for _, sym := range blacklist {
		blacklistMap[toStandardSymbol(sym)] = true
	}

	var results []exchange.PotentialFundingResult
	for tickerID, contract := range rawMap {
		stdSym := toStandardSymbol(contract.TickerID)
		if stdSym == "" {
			stdSym = toStandardSymbol(tickerID)
		}

		if blacklistMap[stdSym] {
			continue
		}
		if len(whitelistMap) > 0 && !whitelistMap[stdSym] {
			continue
		}

		vol24h, _ := contract.TargetVolume.Float64() // TargetVolume is USDT volume usually in cg format
		if vol24h == 0 {
			// fallback to base volume * last price
			baseVol, _ := contract.BaseVolume.Float64()
			lastPrice, _ := contract.LastPrice.Float64()
			vol24h = baseVol * lastPrice
		}

		if minVol24h > 0 && vol24h < minVol24h {
			continue
		}
		if maxVol24h > 0 && vol24h > maxVol24h {
			continue
		}

		price, _ := contract.LastPrice.Float64()
		rate, _ := contract.FundingRate.Float64()

		results = append(results, exchange.PotentialFundingResult{
			Symbol:     stdSym,
			Rate:       rate,
			SettleTime: contract.NextFundingRateTimestamp,
			Volume24h:  vol24h,
			Price:      price,
		})
	}

	return results
}
