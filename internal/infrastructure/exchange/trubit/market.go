package trubit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

const paramSymbols = "symbols"

type trubitRefData struct {
	Symbol string `json:"symbol"`
	Type   string `json:"type"`
}

type trubitRefResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Result  []trubitRefData `json:"result"`
}

type trubitStats struct {
	Symbol    string       `json:"symbol"`
	LastPrice xjson.Number `json:"lastPrice"`
	Volume    xjson.Number `json:"volume"`
}

type trubitStatsResponse struct {
	Code    int           `json:"code"`
	Message string        `json:"message"`
	Result  []trubitStats `json:"result"`
}

type trubitFunding struct {
	Symbol    string       `json:"symbol"`
	Rate      xjson.Number `json:"rate"`
	Timestamp int64        `json:"timestamp"`
}

type trubitFundingResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Result  []trubitFunding `json:"result"`
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
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "_", "")
	return s
}

func getNext8HourInterval() int64 {
	now := time.Now().UTC()
	hour := now.Hour()
	var nextHour int
	switch {
	case hour < 8:
		nextHour = 8
	case hour < 16:
		nextHour = 16
	default:
		nextHour = 24
	}
	nextSettle := time.Date(now.Year(), now.Month(), now.Day(), nextHour, 0, 0, 0, time.UTC)
	return nextSettle.UnixMilli()
}

func chunkSymbols(symbols []string, size int) [][]string {
	var chunks [][]string
	for i := 0; i < len(symbols); i += size {
		end := min(i+size, len(symbols))
		chunks = append(chunks, symbols[i:end])
	}
	return chunks
}

// GetPotentialFundingSymbols satisfies the ScannerClient interface.
func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist, blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	// 1. Fetch reference data
	refBody, err := c.request(ctx, "/market/api/v1/basic/refData", nil)
	if err != nil {
		return nil, fmt.Errorf("trubit get refData: %w", err)
	}

	var refResp trubitRefResponse
	if err := json.Unmarshal(refBody, &refResp); err != nil {
		return nil, fmt.Errorf("unmarshal trubit refData: %w", err)
	}

	if refResp.Code != 0 {
		return nil, fmt.Errorf("trubit api error: %s", refResp.Message)
	}

	targetSymbols := c.filterTrubitSymbols(&refResp, whitelist, blacklist)
	if len(targetSymbols) == 0 {
		return nil, nil
	}

	// 2. Fetch stats and funding in chunks of 20 symbols
	marketMap := c.fetchTrubitMarketData(ctx, targetSymbols)

	var results []exchange.PotentialFundingResult
	for stdSym, data := range marketMap {
		if minVol24h > 0 && data.vol24h < minVol24h {
			continue
		}
		if maxVol24h > 0 && data.vol24h > maxVol24h {
			continue
		}

		results = append(results, exchange.PotentialFundingResult{
			Symbol:     stdSym,
			Rate:       data.rate,
			SettleTime: getNext8HourInterval(),
			Volume24h:  data.vol24h,
			Price:      data.lastPrice,
		})
	}

	return results, nil
}

func (c *Client) filterTrubitSymbols(refResp *trubitRefResponse, whitelist, blacklist []string) []string {
	whitelistMap := make(map[string]bool)
	for _, sym := range whitelist {
		whitelistMap[toStandardSymbol(sym)] = true
	}

	blacklistMap := make(map[string]bool)
	for _, sym := range blacklist {
		blacklistMap[toStandardSymbol(sym)] = true
	}

	var targetSymbols []string
	for _, ref := range refResp.Result {
		if ref.Type != "PERP" {
			continue
		}

		stdSym := toStandardSymbol(ref.Symbol)
		if blacklistMap[stdSym] {
			continue
		}
		if len(whitelistMap) > 0 && !whitelistMap[stdSym] {
			continue
		}

		targetSymbols = append(targetSymbols, ref.Symbol)
	}
	return targetSymbols
}

type symbolMarketData struct {
	lastPrice float64
	vol24h    float64
	rate      float64
	timestamp int64
}

func (c *Client) fetchTrubitMarketData(ctx context.Context, targetSymbols []string) map[string]*symbolMarketData {
	chunks := chunkSymbols(targetSymbols, 20)
	marketMap := make(map[string]*symbolMarketData)

	for _, chunk := range chunks {
		csv := strings.Join(chunk, ",")

		// Fetch Stats
		statsBody, err := c.request(ctx, "/market/api/v1/kLine/tradeStatistics", map[string]string{paramSymbols: csv})
		if err != nil {
			c.logger.Error("failed to fetch tradeStatistics for trubit chunk", "error", err, "symbols", csv)
			continue
		}
		var statsResp trubitStatsResponse
		if err := json.Unmarshal(statsBody, &statsResp); err != nil {
			c.logger.Error("failed to unmarshal tradeStatistics for trubit chunk", "error", err, "symbols", csv)
			continue
		}

		for _, item := range statsResp.Result {
			stdSym := toStandardSymbol(item.Symbol)
			price, _ := item.LastPrice.Float64()
			vol, _ := item.Volume.Float64()

			data, exists := marketMap[stdSym]
			if !exists {
				data = &symbolMarketData{}
				marketMap[stdSym] = data
			}
			data.lastPrice = price
			data.vol24h = vol
		}

		// Fetch Funding
		fundBody, err := c.request(ctx, "/market/api/v1/kLine/fundingRate", map[string]string{paramSymbols: csv})
		if err != nil {
			c.logger.Error("failed to fetch fundingRate for trubit chunk", "error", err, "symbols", csv)
			continue
		}
		var fundResp trubitFundingResponse
		if err := json.Unmarshal(fundBody, &fundResp); err != nil {
			c.logger.Error("failed to unmarshal fundingRate for trubit chunk", "error", err, "symbols", csv)
			continue
		}

		for _, item := range fundResp.Result {
			stdSym := toStandardSymbol(item.Symbol)
			rate, _ := item.Rate.Float64()

			data, exists := marketMap[stdSym]
			if !exists {
				data = &symbolMarketData{}
				marketMap[stdSym] = data
			}
			data.rate = rate
			data.timestamp = item.Timestamp
		}
	}
	return marketMap
}
