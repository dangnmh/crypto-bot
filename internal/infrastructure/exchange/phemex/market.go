package phemex

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type phemexProduct struct {
	Symbol string `json:"symbol"`
	Type   string `json:"type"` // e.g. "Perpetual"
	Status string `json:"status"`
}

type phemexProductsResponse struct {
	Code int64  `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Products []phemexProduct `json:"products"`
	} `json:"data"`
}

type phemexTicker struct {
	Symbol          string       `json:"symbol"`
	LastEp          xjson.Number `json:"lastEp"`          // price scaled by 10^8
	VolumeEv        xjson.Number `json:"volumeEv"`        // volume scaled by 10^8 or base
	FundingRateEr   xjson.Number `json:"fundingRateEr"`   // funding rate scaled by 10^8
	PredFundingRate xjson.Number `json:"predFundingRate"` // predicted funding rate
}

type phemexTickerResponse struct {
	Error  any            `json:"error"`
	Id     int64          `json:"id"`
	Result []phemexTicker `json:"result"`
}

func (c *Client) request(ctx context.Context, path string, query map[string]string) ([]byte, error) {
	reqURL, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}

	if len(query) > 0 {
		q := reqURL.Query()
		for k, v := range query {
			q.Set(k, v)
		}
		reqURL.RawQuery = q.Encode()
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
	s = strings.ReplaceAll(s, "-PERP", "")
	s = strings.ReplaceAll(s, "-SWAP", "")
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "_", "")
	return s
}

func (c *Client) fetchPerpetualSymbols(ctx context.Context) (map[string]bool, error) {
	prodBody, err := c.request(ctx, "/public/products", nil)
	if err != nil {
		return nil, fmt.Errorf("phemex get products: %w", err)
	}

	var prodResp phemexProductsResponse
	if err := xjson.Unmarshal(prodBody, &prodResp); err != nil {
		return nil, fmt.Errorf("unmarshal phemex products: %w", err)
	}

	perpSymbols := make(map[string]bool)
	for i := range prodResp.Data.Products {
		item := &prodResp.Data.Products[i]
		if strings.EqualFold(item.Type, "Perpetual") && strings.EqualFold(item.Status, "Listed") {
			perpSymbols[item.Symbol] = true
		}
	}
	return perpSymbols, nil
}

func (c *Client) fetchTickers(ctx context.Context) ([]phemexTicker, error) {
	tickerBody, err := c.request(ctx, "/md/v2/ticker/24hr/all", nil)
	if err != nil {
		return nil, fmt.Errorf("phemex get tickers: %w", err)
	}

	var tickResp phemexTickerResponse
	if err := xjson.Unmarshal(tickerBody, &tickResp); err != nil {
		return nil, fmt.Errorf("unmarshal phemex tickers: %w", err)
	}

	if tickResp.Error != nil {
		return nil, fmt.Errorf("phemex ticker api error: %v", tickResp.Error)
	}
	return tickResp.Result, nil
}

func (c *Client) processTicker(
	ticker *phemexTicker,
	perpSymbols map[string]bool,
	minVol24h, maxVol24h float64,
	whitelistMap map[string]bool,
	blacklistMap map[string]bool,
) (exchange.PotentialFundingResult, bool) {
	// Filter only perpetual symbols
	if !perpSymbols[ticker.Symbol] {
		return exchange.PotentialFundingResult{}, false
	}

	stdSym := toStandardSymbol(ticker.Symbol)
	if blacklistMap[stdSym] {
		return exchange.PotentialFundingResult{}, false
	}
	if len(whitelistMap) > 0 && !whitelistMap[stdSym] {
		return exchange.PotentialFundingResult{}, false
	}

	lastEp, _ := ticker.LastEp.Int64()
	price := float64(lastEp) / 1e8

	volumeEv, _ := ticker.VolumeEv.Int64()
	baseVolume := float64(volumeEv) / 1e8
	vol24h := baseVolume * price // volume in quote currency (USD/USDT)

	if minVol24h > 0 && vol24h < minVol24h {
		return exchange.PotentialFundingResult{}, false
	}
	if maxVol24h > 0 && vol24h > maxVol24h {
		return exchange.PotentialFundingResult{}, false
	}

	fundingRateEr, _ := ticker.FundingRateEr.Int64()
	rate := float64(fundingRateEr) / 1e8

	// Calculate next settle time (usually every 8 hours at 00:00, 08:00, 16:00 UTC)
	now := time.Now().UTC()
	nowUnix := now.Unix()
	period := int64(8 * 3600)
	nextSettleUnix := ((nowUnix / period) + 1) * period
	settleTime := nextSettleUnix * 1000 // in milliseconds

	return exchange.PotentialFundingResult{
		Symbol:     stdSym,
		Rate:       rate,
		SettleTime: settleTime,
		Volume24h:  vol24h,
		Price:      price,
	}, true
}

func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	perpSymbols, err := c.fetchPerpetualSymbols(ctx)
	if err != nil {
		return nil, err
	}

	tickers, err := c.fetchTickers(ctx)
	if err != nil {
		return nil, err
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
		res, ok := c.processTicker(&tickers[i], perpSymbols, minVol24h, maxVol24h, whitelistMap, blacklistMap)
		if ok {
			results = append(results, res)
		}
	}

	return results, nil
}
