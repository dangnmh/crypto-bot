package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
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

func main() {
	fmt.Println("🔍 Scanning MEXC Futures market for top funding rates...")

	// Create a new exchange client. No API keys needed for public market data.
	client := exchange.NewClient("https://contract.mexc.com", "", "", config.LoggingConfig{})

	// Give a timeout context
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Fetch Tickers for Volume
	tickers, err := client.GetTickers(ctx, "")
	if err != nil {
		fmt.Printf("🔴 Failed to fetch tickers: %v\n", err)
		os.Exit(1)
	}

	volMap := make(map[string]float64)
	for _, t := range tickers {
		volMap[t.Symbol] = t.Amount24
	}

	// 2. Fetch Funding Rates for NextSettleTime
	body, err := client.GetCtx(ctx, "/api/v1/contract/funding_rate", nil)
	if err != nil {
		fmt.Printf("🔴 Failed to fetch funding rates: %v\n", err)
		os.Exit(1)
	}

	var frResp APIResponse
	if err := json.Unmarshal(body, &frResp); err != nil {
		fmt.Printf("🔴 Failed to parse funding rates: %v\n", err)
		os.Exit(1)
	}

	var validRates []FundingRateDetail
	for _, r := range frResp.Data {
		vol := volMap[r.Symbol]
		// Filter out illiquid or zero funding rate
		if r.FundingRate == 0 || vol < 100000 {
			continue
		}
		validRates = append(validRates, r)
	}

	// Sort by absolute funding rate descending
	sort.Slice(validRates, func(i, j int) bool {
		return math.Abs(validRates[i].FundingRate) > math.Abs(validRates[j].FundingRate)
	})

	fmt.Printf("✅ Scanned %d active pairs. Top 20 opportunities:\n\n", len(validRates))

	// Print a formatted table
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', tabwriter.AlignRight|tabwriter.Debug)
	_, _ = fmt.Fprintln(w, "SYMBOL\t FUNDING RATE (%)\t NEXT SETTLE IN\t TRADE DIRECTION\t 24H VOL (USDT)\t")
	_, _ = fmt.Fprintln(w, "------\t ----------------\t --------------\t ---------------\t --------------\t")

	now := time.Now()
	displayCount := 20
	if len(validRates) < displayCount {
		displayCount = len(validRates)
	}

	for i := 0; i < displayCount; i++ {
		r := validRates[i]
		vol := volMap[r.Symbol]

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
		volStr := formatVolume(vol)

		_, _ = fmt.Fprintf(w, "%s\t %s\t %s\t %s\t %s\t\n",
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
