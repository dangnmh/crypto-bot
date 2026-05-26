package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	sysconfig "crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange/bybit"
	"crypto-bot/internal/infrastructure/exchange/gate"
	"crypto-bot/internal/infrastructure/exchange/mexc"
	"crypto-bot/pkg/httpclient"
)

type FundingRateDetail struct {
	Symbol         string  `json:"symbol"`
	FundingRate    float64 `json:"fundingRate"`
	NextSettleTime int64   `json:"nextSettleTime"`
}

type APIResponse struct {
	Success bool                `json:"success"`
	Data    []FundingRateDetail `json:"data"`
}

type Opportunity struct {
	Exchange       string
	Symbol         string
	FundingRate    float64
	NextSettleTime int64
	Volume24h      float64
}

func main() {
	fmt.Println("🔍 Scanning MEXC, Gate.io & Bybit Futures markets for top funding rates...")

	// Create exchange clients. No API keys needed for public market data.
	httpPool := httpclient.NewPool(httpclient.DefaultPoolConfig())
	mexcClient := mexc.NewClient(httpPool, "https://contract.mexc.com", "", "", sysconfig.LoggingConfig{})
	gateClient := gate.NewClient(httpPool, "https://api.gateio.ws/api/v4", "", "", sysconfig.LoggingConfig{})
	bybitClient := bybit.NewClient(httpPool, "https://api.bybit.com", "", "", "standard", sysconfig.LoggingConfig{})

	// Give a timeout context
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var opportunities []Opportunity

	// ── 1. Fetch MEXC Data ────────────────────────────────────────────────
	mexcOpps, err := fetchMEXCOpportunities(ctx, mexcClient)
	if err != nil {
		fmt.Printf("🔴 Failed to fetch MEXC data: %v\n", err)
	} else {
		opportunities = append(opportunities, mexcOpps...)
	}

	// ── 2. Fetch Gate.io Data ─────────────────────────────────────────────
	gateOpps, err := fetchGateOpportunities(ctx, gateClient)
	if err != nil {
		fmt.Printf("🔴 Failed to fetch Gate.io data: %v\n", err)
	} else {
		opportunities = append(opportunities, gateOpps...)
	}

	// ── 3. Fetch Bybit Data ───────────────────────────────────────────────
	bybitOpps, err := fetchBybitOpportunities(ctx, bybitClient)
	if err != nil {
		fmt.Printf("🔴 Failed to fetch Bybit data: %v\n", err)
	} else {
		opportunities = append(opportunities, bybitOpps...)
	}

	// Sort by absolute funding rate descending
	sort.Slice(opportunities, func(i, j int) bool {
		return math.Abs(opportunities[i].FundingRate) > math.Abs(opportunities[j].FundingRate)
	})

	fmt.Printf("✅ Scanned %d active pairs. Top 20 opportunities:\n\n", len(opportunities))

	// Print a formatted table
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', tabwriter.AlignRight|tabwriter.Debug)
	_, _ = fmt.Fprintln(w, "EXCHANGE\t SYMBOL\t FUNDING RATE (%)\t NEXT SETTLE IN\t TRADE DIRECTION\t 24H VOL (USDT)\t")
	_, _ = fmt.Fprintln(w, "--------\t ------\t ----------------\t --------------\t ---------------\t --------------\t")

	now := time.Now()
	displayCount := 20
	if len(opportunities) < displayCount {
		displayCount = len(opportunities)
	}

	for i := 0; i < displayCount; i++ {
		r := opportunities[i]

		// 1. Calculate Countdown
		settleTime := time.UnixMilli(r.NextSettleTime)
		countdown := settleTime.Sub(now).Round(time.Second)
		if countdown < 0 {
			countdown = 0
		}

		// 2. Formatting Funding Rate with sign and color conceptually
		frPct := r.FundingRate * 100
		frStr := fmt.Sprintf("%.4f%%", frPct)
		if frPct > 0 {
			frStr = "+" + frStr
		}

		// 3. Trade Direction based on Reversion strategy
		direction := "LONG"
		if r.FundingRate < 0 {
			direction = "SHORT"
		}

		// 4. Volume formatted
		volStr := formatVolume(r.Volume24h)

		_, _ = fmt.Fprintf(w, "%s\t %s\t %s\t %s\t %s\t %s\t\n",
			strings.ToUpper(r.Exchange),
			r.Symbol,
			frStr,
			countdown.String(), // This will now properly format like "1h30m45s"
			direction,
			volStr,
		)
	}
	_ = w.Flush()
	fmt.Println("\n💡 Tip: Direction indicates what to open to ride the post-settlement reversion pump/dump.")
}

func fetchMEXCOpportunities(ctx context.Context, client *mexc.Client) ([]Opportunity, error) {
	tickers, err := client.GetTickers(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("fetch tickers: %w", err)
	}

	volMap := make(map[string]float64)
	for _, t := range tickers {
		volMap[t.Symbol] = t.Amount24
	}

	body, err := client.GetCtx(ctx, "/api/v1/contract/funding_rate", nil)
	if err != nil {
		return nil, fmt.Errorf("fetch funding rates: %w", err)
	}

	var frResp APIResponse
	if err := json.Unmarshal(body, &frResp); err != nil {
		return nil, fmt.Errorf("parse funding rates: %w", err)
	}

	var opportunities []Opportunity
	for _, r := range frResp.Data {
		vol := volMap[r.Symbol]
		if r.FundingRate == 0 || vol < 100000 {
			continue
		}
		opportunities = append(opportunities, Opportunity{
			Exchange:       "mexc",
			Symbol:         r.Symbol,
			FundingRate:    r.FundingRate,
			NextSettleTime: r.NextSettleTime,
			Volume24h:      vol,
		})
	}
	return opportunities, nil
}

func fetchGateOpportunities(ctx context.Context, client *gate.Client) ([]Opportunity, error) {
	tickers, err := client.GetTickers(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("fetch tickers: %w", err)
	}

	gateNextSettle := getGateNextSettleTime().UnixMilli()
	var opportunities []Opportunity
	for _, t := range tickers {
		if t.FundingRate == 0 || t.Amount24 < 100000 {
			continue
		}
		opportunities = append(opportunities, Opportunity{
			Exchange:       "gate",
			Symbol:         t.Symbol,
			FundingRate:    t.FundingRate,
			NextSettleTime: gateNextSettle,
			Volume24h:      t.Amount24,
		})
	}
	return opportunities, nil
}

func fetchBybitOpportunities(ctx context.Context, client *bybit.Client) ([]Opportunity, error) {
	tickers, err := client.GetTickers(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("fetch tickers: %w", err)
	}

	var opportunities []Opportunity
	for _, t := range tickers {
		if t.FundingRate == 0 || t.Amount24 < 100000 {
			continue
		}
		opportunities = append(opportunities, Opportunity{
			Exchange:       "bybit",
			Symbol:         t.Symbol,
			FundingRate:    t.FundingRate,
			NextSettleTime: t.NextSettleTime,
			Volume24h:      t.Amount24,
		})
	}
	return opportunities, nil
}

func getGateNextSettleTime() time.Time {
	now := time.Now().UTC()
	h := now.Hour()
	var nextHour int
	var addDays int
	if h < 8 {
		nextHour = 8
	} else if h < 16 {
		nextHour = 16
	} else {
		nextHour = 0
		addDays = 1
	}

	settle := time.Date(now.Year(), now.Month(), now.Day(), nextHour, 0, 0, 0, time.UTC)
	if addDays > 0 {
		settle = settle.AddDate(0, 0, addDays)
	}
	return settle
}

// formatVolume formats large numbers to K, M, B for readability.
func formatVolume(vol float64) string {
	if vol >= 1_000_000_000 {
		return fmt.Sprintf("%.2fB", vol/1_000_000_000)
	}
	if vol >= 1_000_000 {
		return fmt.Sprintf("%.2fM", vol/1_000_000)
	}
	if vol >= 1_000 {
		return fmt.Sprintf("%.2fK", vol/1_000)
	}
	return fmt.Sprintf("%.2f", vol)
}
