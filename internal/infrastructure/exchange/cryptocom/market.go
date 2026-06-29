package cryptocom

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type cryptocomTicker struct {
	I string       `json:"i"` // Instrument name
	A xjson.Number `json:"a"` // Latest trade price
	V xjson.Number `json:"v"` // 24h volume
	T int64        `json:"t"` // Timestamp
}

type cryptocomValuation struct {
	InstrumentName string       `json:"instrument_name"`
	Symbol         string       `json:"symbol"`
	I              string       `json:"i"`
	V              xjson.Number `json:"v"` // Valuation value (funding rate)
	T              int64        `json:"t"` // Timestamp
}

type cryptocomTickerResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Result  struct {
		Data []cryptocomTicker `json:"data"`
	} `json:"result"`
}

type cryptocomValuationResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Result  struct {
		Data []cryptocomValuation `json:"data"`
	} `json:"result"`
}

type cryptocomCandidate struct {
	ticker    *cryptocomTicker
	stdSym    string
	price     float64
	volume24h float64
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

func (c *Client) fetchTickers(ctx context.Context) ([]cryptocomTicker, error) {
	tickerBody, err := c.request(ctx, "/public/get-tickers", nil)
	if err != nil {
		return nil, fmt.Errorf("cryptocom get tickers: %w", err)
	}

	var tickResp cryptocomTickerResponse
	if err := xjson.Unmarshal(tickerBody, &tickResp); err != nil {
		return nil, fmt.Errorf("unmarshal cryptocom tickers: %w", err)
	}

	if tickResp.Code != 0 {
		return nil, fmt.Errorf("cryptocom ticker api error: code=%d msg=%s", tickResp.Code, tickResp.Message)
	}
	return tickResp.Result.Data, nil
}

func (c *Client) fetchValuationForSymbol(ctx context.Context, instrumentName string) (*cryptocomValuation, error) {
	valBody, err := c.request(ctx, "/public/get-valuations", map[string]string{
		"instrument_name": instrumentName,
		"valuation_type":  "funding_rate",
	})
	if err != nil {
		return nil, fmt.Errorf("cryptocom get valuations for %s: %w", instrumentName, err)
	}

	var valResp cryptocomValuationResponse
	if err := xjson.Unmarshal(valBody, &valResp); err != nil {
		return nil, fmt.Errorf("unmarshal valuations: %w", err)
	}

	if valResp.Code != 0 {
		return nil, fmt.Errorf("valuations api error: code=%d msg=%s", valResp.Code, valResp.Message)
	}

	if len(valResp.Result.Data) == 0 {
		return nil, fmt.Errorf("no valuations returned for %s", instrumentName)
	}

	return &valResp.Result.Data[0], nil
}

func (c *Client) filterCandidates(
	tickers []cryptocomTicker,
	minVol24h, maxVol24h float64,
	whitelistMap, blacklistMap map[string]bool,
) []cryptocomCandidate {
	var candidates []cryptocomCandidate
	for i := range tickers {
		ticker := &tickers[i]
		if !strings.HasSuffix(strings.ToUpper(ticker.I), "-PERP") {
			continue
		}

		stdSym := toStandardSymbol(ticker.I)
		if blacklistMap[stdSym] {
			continue
		}
		if len(whitelistMap) > 0 && !whitelistMap[stdSym] {
			continue
		}

		price, _ := ticker.A.Float64()
		baseVol, _ := ticker.V.Float64()
		vol24h := baseVol * price

		if minVol24h > 0 && vol24h < minVol24h {
			continue
		}
		if maxVol24h > 0 && vol24h > maxVol24h {
			continue
		}

		candidates = append(candidates, cryptocomCandidate{
			ticker:    ticker,
			stdSym:    stdSym,
			price:     price,
			volume24h: vol24h,
		})
	}
	return candidates
}

func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
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

	candidates := c.filterCandidates(tickers, minVol24h, maxVol24h, whitelistMap, blacklistMap)

	// Sort candidates by volume descending
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].volume24h > candidates[j].volume24h
	})

	// Limit to top 30 symbols to avoid hitting rate limits
	limit := min(len(candidates), 30)

	var results []exchange.PotentialFundingResult
	for i := range limit {
		cand := candidates[i]
		val, err := c.fetchValuationForSymbol(ctx, cand.ticker.I)
		var rate float64
		var settleTime int64

		if err == nil {
			rate, _ = val.V.Float64()
			settleTime = val.T
		} else {
			c.logger.Error("Failed to fetch valuation for Crypto.com symbol", "symbol", cand.ticker.I, "error", err)
		}

		// Calculate next settle time if not directly provided
		if settleTime == 0 {
			settleTime = cand.ticker.T + 8*3600*1000
		}

		results = append(results, exchange.PotentialFundingResult{
			Symbol:     cand.stdSym,
			Rate:       rate,
			SettleTime: settleTime,
			Volume24h:  cand.volume24h,
			Price:      cand.price,
		})
	}

	return results, nil
}
