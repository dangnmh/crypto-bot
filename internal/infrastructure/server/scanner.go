package server

import (
	"context"
	"maps"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/exchange"

	"github.com/gin-gonic/gin"
)

type Opportunity struct {
	Exchange       string  `json:"exchange"`
	Symbol         string  `json:"symbol"`
	FundingRate    float64 `json:"fundingRate"`
	NextSettleTime int64   `json:"nextSettleTime"`
	Volume24h      float64 `json:"volume24h"`
	Price          float64 `json:"price"`
}

type SymbolGroup struct {
	StandardSymbol string        `json:"standardSymbol"`
	Score          float64       `json:"score"`
	ScoreRate      float64       `json:"scoreRate"`
	Opportunities  []Opportunity `json:"opportunities"`
}

var scannerBlacklist = []string{
	"BTC_USDT", "BTCUSDT", "BTC-USDT", "BTC-USDT-SWAP", "BTC",
	"ETH_USDT", "ETHUSDT", "ETH-USDT", "ETH-USDT-SWAP", "ETH",
	"SOL_USDT", "SOLUSDT", "SOL-USDT", "SOL-USDT-SWAP", "SOL",
	"BNB_USDT", "BNBUSDT", "BNB-USDT", "BNB-USDT-SWAP", "BNB",
	"XRP_USDT", "XRPUSDT", "XRP-USDT", "XRP-USDT-SWAP", "XRP",
	"ADA_USDT", "ADAUSDT", "ADA-USDT", "ADA-USDT-SWAP", "ADA",
	"DOT_USDT", "DOTUSDT", "DOT-USDT", "DOT-USDT-SWAP", "DOT",
	"DOGE_USDT", "DOGEUSDT", "DOGE-USDT", "DOGE-USDT-SWAP", "DOGE",
	"LTC_USDT", "LTCUSDT", "LTC-USDT", "LTC-USDT-SWAP", "LTC",
	"TRX_USDT", "TRXUSDT", "TRX-USDT", "TRX-USDT-SWAP", "TRX",
}

func (s *APIServer) handleFundingScanner(c *gin.Context) {
	exchangesParam := c.Query("exchange")
	var targetExchanges []string
	if exchangesParam != "" {
		for raw := range strings.SplitSeq(exchangesParam, ",") {
			trimmed := strings.ToLower(strings.TrimSpace(raw))
			if trimmed != "" {
				targetExchanges = append(targetExchanges, trimmed)
			}
		}
	}

	minRate := 0.3
	if rateStr := c.Query("min_rate"); rateStr != "" {
		if parsed, err := strconv.ParseFloat(rateStr, 64); err == nil {
			minRate = parsed
		}
	}

	minVol := 1000000.0
	if volStr := c.Query("min_vol"); volStr != "" {
		if parsed, err := strconv.ParseFloat(volStr, 64); err == nil {
			minVol = parsed
		}
	}

	groups, err := s.runScanner(c.Request.Context(), targetExchanges, minRate, minVol)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{errKey: err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"groups":  groups,
	})
}

func (s *APIServer) runScanner(ctx context.Context, targetExchanges []string, minRate, minVol float64) ([]SymbolGroup, error) {
	providers := s.filterProviders(targetExchanges)
	if len(providers) == 0 {
		return []SymbolGroup{}, nil
	}

	opportunities := s.fetchOpportunities(ctx, providers, minRate, minVol)
	groups := s.groupAndSortOpportunities(opportunities)
	return groups, nil
}

func (s *APIServer) filterProviders(targetExchanges []string) map[string]*app.ExchangeProvider {
	providers := make(map[string]*app.ExchangeProvider)
	if len(targetExchanges) > 0 {
		for _, name := range targetExchanges {
			if prov, exists := s.engine.Providers[name]; exists {
				providers[name] = prov
			}
		}
	} else {
		maps.Copy(providers, s.engine.Providers)
	}
	return providers
}

func (s *APIServer) fetchOpportunities(ctx context.Context, providers map[string]*app.ExchangeProvider, minRate, minVol float64) []Opportunity {
	var opportunities []Opportunity
	var mu sync.Mutex
	var wg sync.WaitGroup

	scanCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	for name, prov := range providers {
		wg.Add(1)
		go func(exchangeName string, client exchange.Client) {
			defer wg.Done()
			results, err := getPotentialFundingSymbols(scanCtx, client, minVol, scannerBlacklist)
			if err != nil {
				return
			}

			var localOpps []Opportunity
			for _, r := range results {
				if r.Rate == 0 {
					continue
				}
				if minRate > 0 && math.Abs(r.Rate)*100 < minRate {
					continue
				}

				nextSettle := r.SettleTime
				if nextSettle == 0 {
					nextSettle = getGateNextSettleTime().UnixMilli()
				}

				localOpps = append(localOpps, Opportunity{
					Exchange:       exchangeName,
					Symbol:         r.Symbol,
					FundingRate:    r.Rate,
					NextSettleTime: nextSettle,
					Volume24h:      r.Volume24h,
					Price:          r.Price,
				})
			}

			mu.Lock()
			opportunities = append(opportunities, localOpps...)
			mu.Unlock()
		}(name, prov.Client)
	}

	wg.Wait()
	return opportunities
}

func (s *APIServer) groupAndSortOpportunities(opportunities []Opportunity) []SymbolGroup {
	groupsMap := make(map[string][]Opportunity)
	for _, opp := range opportunities {
		stdSym := standardizeSymbol(opp.Symbol)
		groupsMap[stdSym] = append(groupsMap[stdSym], opp)
	}

	groups := make([]SymbolGroup, 0, len(groupsMap))
	for stdSym, opps := range groupsMap {
		maxAbsFR := 0.0
		var bestRate float64
		for _, o := range opps {
			absFR := math.Abs(o.FundingRate)
			if absFR > maxAbsFR {
				maxAbsFR = absFR
				bestRate = o.FundingRate
			}
		}

		sort.Slice(opps, func(i, j int) bool {
			return opps[i].Volume24h > opps[j].Volume24h
		})

		groups = append(groups, SymbolGroup{
			StandardSymbol: stdSym,
			Score:          maxAbsFR,
			ScoreRate:      bestRate,
			Opportunities:  opps,
		})
	}

	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Score > groups[j].Score
	})

	return groups
}

func standardizeSymbol(symbol string) string {
	s := strings.ToUpper(symbol)
	s = strings.TrimSuffix(s, "-SWAP")
	s = strings.TrimSuffix(s, "SWAP")
	s = strings.ReplaceAll(s, "_", "")
	s = strings.ReplaceAll(s, "-", "")
	if strings.HasSuffix(s, "USDTM") {
		s = strings.TrimSuffix(s, "M")
	}
	if before, ok := strings.CutSuffix(s, "USD"); ok {
		s = before + "USDT"
	}
	if before, ok := strings.CutSuffix(s, "USDC"); ok {
		s = before + "USDT"
	}
	if !strings.HasSuffix(s, "USDT") {
		s += "USDT"
	}
	base := strings.TrimSuffix(s, "USDT")
	return base + "_USDT"
}

func getGateNextSettleTime() time.Time {
	now := time.Now().UTC()
	h := now.Hour()
	var nextHour int
	var addDays int
	switch {
	case h < 8:
		nextHour = 8
	case h < 16:
		nextHour = 16
	default:
		nextHour = 0
		addDays = 1
	}

	settle := time.Date(now.Year(), now.Month(), now.Day(), nextHour, 0, 0, 0, time.UTC)
	if addDays > 0 {
		settle = settle.AddDate(0, 0, addDays)
	}
	return settle
}

type potentialFundingResult struct {
	Symbol     string
	Rate       float64
	SettleTime int64
	Volume24h  float64
	Price      float64
}

func getPotentialFundingSymbols(
	ctx context.Context,
	client exchange.Client,
	minVol24h float64,
	blacklist []string,
) ([]potentialFundingResult, error) {
	tickers, err := client.GetTickers(ctx, "")
	if err != nil {
		return nil, err
	}

	blacklistMap := make(map[string]bool)
	for _, sym := range blacklist {
		blacklistMap[sym] = true
	}

	var filteredSymbols []string
	volMap := make(map[string]float64)
	priceMap := make(map[string]float64)

	for _, t := range tickers {
		if blacklistMap[t.Symbol] {
			continue
		}
		vol := t.AmountUSDT24
		if vol < minVol24h {
			continue
		}

		filteredSymbols = append(filteredSymbols, t.Symbol)
		volMap[t.Symbol] = vol
		priceMap[t.Symbol] = t.LastPrice
	}

	if len(filteredSymbols) == 0 {
		return nil, nil
	}

	rates, err := client.GetFundingRates(ctx, filteredSymbols)
	if err != nil {
		return nil, err
	}

	results := make([]potentialFundingResult, 0, len(rates))
	for _, r := range rates {
		results = append(results, potentialFundingResult{
			Symbol:     r.Symbol,
			Rate:       r.Rate,
			SettleTime: r.SettleTime,
			Volume24h:  volMap[r.Symbol],
			Price:      priceMap[r.Symbol],
		})
	}

	return results, nil
}
