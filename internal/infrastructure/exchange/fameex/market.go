package fameex

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type fameexTickerItem struct {
	TickerID                string `json:"ticker_id"`
	BaseCurrency            string `json:"base_currency"`
	QuoteCurrency           string `json:"quote_currency"`
	LastPrice               string `json:"last_price"`
	BaseVolume              string `json:"base_volume"`
	QuoteVolume             string `json:"quote_volume"`
	Bid                     string `json:"bid"`
	Ask                     string `json:"ask"`
	High                    string `json:"high"`
	Low                     string `json:"low"`
	ProductType             string `json:"product_type"`
	OpenInterest            string `json:"open_interest"`
	OpenInterestUSD         string `json:"open_interest_usd"`
	IndexPrice              string `json:"index_price"`
	Basis                   string `json:"basis"`
	FundingRate             string `json:"funding_rate"`
	NextFundingRateTimestam int64  `json:"next_funding_rate_timestam"`
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
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "_", "")
	return s
}

//nolint:gocognit,cyclop // Scanner methods are naturally complex
func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	body, err := c.request(ctx, "/swap-api/v2/tickers")
	if err != nil {
		return nil, fmt.Errorf("fameex list tickers: %w", err)
	}

	var tickers []fameexTickerItem
	if err := xjson.Unmarshal(body, &tickers); err != nil {
		return nil, fmt.Errorf("unmarshal fameex tickers: %w", err)
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
		item := &tickers[i]

		// Only Perpetual contracts
		if !strings.EqualFold(item.ProductType, "Perpetual") {
			continue
		}

		// Only settle on USDT/USDC
		if !strings.EqualFold(item.QuoteCurrency, "USDT") && !strings.EqualFold(item.QuoteCurrency, "USDC") {
			continue
		}

		stdSym := toStandardSymbol(item.BaseCurrency + item.QuoteCurrency)
		origSym := toStandardSymbol(item.TickerID)

		// Filter blacklist/whitelist by both formats to be extremely safe
		if blacklistMap[stdSym] || blacklistMap[origSym] {
			continue
		}
		if len(whitelistMap) > 0 && !whitelistMap[stdSym] && !whitelistMap[origSym] {
			continue
		}

		vol24h, _ := strconv.ParseFloat(item.QuoteVolume, 64)
		if minVol24h > 0 && vol24h < minVol24h {
			continue
		}
		if maxVol24h > 0 && vol24h > maxVol24h {
			continue
		}

		price, _ := strconv.ParseFloat(item.LastPrice, 64)
		rate, _ := strconv.ParseFloat(item.FundingRate, 64)

		results = append(results, exchange.PotentialFundingResult{
			Symbol:     stdSym,
			Rate:       rate,
			SettleTime: item.NextFundingRateTimestam,
			Volume24h:  vol24h,
			Price:      price,
		})
	}

	return results, nil
}
