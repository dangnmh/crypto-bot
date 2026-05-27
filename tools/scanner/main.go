package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	sysconfig "crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/binance"
	"crypto-bot/internal/infrastructure/exchange/bingx"
	"crypto-bot/internal/infrastructure/exchange/bitget"
	"crypto-bot/internal/infrastructure/exchange/bybit"
	"crypto-bot/internal/infrastructure/exchange/gate"
	"crypto-bot/internal/infrastructure/exchange/hyperliquid"
	"crypto-bot/internal/infrastructure/exchange/kucoin"
	"crypto-bot/internal/infrastructure/exchange/mexc"
	"crypto-bot/internal/infrastructure/exchange/okx"
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
	fmt.Println("🔍 Scanning MEXC, Gate.io, Bybit, Binance, OKX, Hyperliquid, Bitget, BingX & KuCoin Futures markets for top funding rates...")

	// Create exchange clients. No API keys needed for public market data.
	httpPool := httpclient.NewPool(httpclient.DefaultPoolConfig())
	mexcClient := mexc.NewClient(httpPool, "https://contract.mexc.com", "", "", sysconfig.LoggingConfig{})
	gateClient := gate.NewClient(httpPool, "https://api.gateio.ws/api/v4", "", "", sysconfig.LoggingConfig{})
	bybitClient := bybit.NewClient(httpPool, "https://api.bybit.com", "", "", "standard", sysconfig.LoggingConfig{})
	okxClient := okx.NewClient(httpPool, "https://www.okx.com", "", "", "", sysconfig.LoggingConfig{})
	hlClient := hyperliquid.NewClient(context.Background(), httpPool, "https://api.hyperliquid.xyz", "", "", sysconfig.LoggingConfig{})
	bitgetClient := bitget.NewClient(httpPool, "https://api.bitget.com", "", "", "", sysconfig.LoggingConfig{})
	bingxClient := bingx.NewClient(httpPool, "https://open-api.bingx.com", "", "", sysconfig.LoggingConfig{})
	kucoinClient := kucoin.NewClient(httpPool, "https://api-futures.kucoin.com", "", "", "", sysconfig.LoggingConfig{})

	binanceClient := binance.NewClient(httpPool, "https://fapi.binance.com", "", "", sysconfig.LoggingConfig{})

	// Give a timeout context (30 seconds for extra safety)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	clients := map[string]exchange.Client{
		"mexc":        mexcClient,
		"gate":        gateClient,
		"bybit":       bybitClient,
		"okx":         okxClient,
		"hyperliquid": hlClient,
		"bitget":      bitgetClient,
		"bingx":       bingxClient,
		"kucoin":      kucoinClient,
		"binance":     binanceClient,
	}

	var opportunities []Opportunity
	var mu sync.Mutex
	var wg sync.WaitGroup

	for name, client := range clients {
		wg.Add(1)
		go func(exchangeName string, c exchange.Client) {
			defer wg.Done()
			tickers, err := c.GetTickers(ctx, "")
			if err != nil {
				fmt.Printf("🔴 Failed to fetch %s data: %v\n", strings.ToUpper(exchangeName), err)
				return
			}

			var localOpps []Opportunity
			for _, t := range tickers {
				vol := t.Amount24
				if vol == 0 {
					vol = t.Volume24
				}

				if vol < 100000 {
					continue
				}

				if t.FundingRate == 0 {
					continue
				}

				nextSettle := t.NextSettleTime
				if nextSettle == 0 {
					nextSettle = getGateNextSettleTime().UnixMilli()
				}

				localOpps = append(localOpps, Opportunity{
					Exchange:       exchangeName,
					Symbol:         t.Symbol,
					FundingRate:    t.FundingRate,
					NextSettleTime: nextSettle,
					Volume24h:      vol,
				})
			}

			mu.Lock()
			opportunities = append(opportunities, localOpps...)
			mu.Unlock()
		}(name, client)
	}

	wg.Wait()

	printOpportunities(opportunities)
}

func printOpportunities(opportunities []Opportunity) {
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
