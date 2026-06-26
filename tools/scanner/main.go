package main

import (
	"context"
	"flag"
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
	"crypto-bot/internal/infrastructure/exchange/batonex"
	"crypto-bot/internal/infrastructure/exchange/binance"
	"crypto-bot/internal/infrastructure/exchange/bitmart"
	"crypto-bot/internal/infrastructure/exchange/bitunix"
	"crypto-bot/internal/infrastructure/exchange/bybit"
	"crypto-bot/internal/infrastructure/exchange/coinw"
	"crypto-bot/internal/infrastructure/exchange/deepcoin"
	"crypto-bot/internal/infrastructure/exchange/gate"
	"crypto-bot/internal/infrastructure/exchange/krakenfutures"
	"crypto-bot/internal/infrastructure/exchange/kucoin"
	"crypto-bot/internal/infrastructure/exchange/mexc"
	"crypto-bot/internal/infrastructure/exchange/okx"
	"crypto-bot/internal/infrastructure/exchange/toobit"
	"crypto-bot/internal/infrastructure/exchange/weex"
	"crypto-bot/pkg/httpclient"
	"crypto-bot/pkg/logger"
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
	Price          float64
}

type ScannerClient interface {
	GetPotentialFundingSymbols(ctx context.Context, minVol24h, maxVol24h float64, whitelist []string, blacklist []string) ([]exchange.PotentialFundingResult, error)
}

//nolint:gocognit,cyclop // Scanner tool main entrypoint is naturally complex
func main() {
	var exchangesFlag string
	flag.StringVar(&exchangesFlag, "exchanges", "", "Comma-separated list of exchanges to scan (e.g. binance,bybit,okx). If empty, scans all.")
	var minFundingRate float64
	flag.Float64Var(&minFundingRate, "minFundingRate", 0.3, "Minimum absolute funding rate (in percent) to filter. E.g. 0.1 for 0.1%")
	var minVol float64
	flag.Float64Var(&minVol, "minVol", 1000000.0, "Minimum 24h volume (in USDT) to filter pairs. E.g. 1000000 for 1M USDT")
	flag.Parse()

	// Parse targeted exchanges if provided
	var targetExchanges map[string]bool
	if exchangesFlag != "" {
		targetExchanges = make(map[string]bool)
		parts := strings.SplitSeq(exchangesFlag, ",")
		for p := range parts {
			name := strings.ToLower(strings.TrimSpace(p))
			if name != "" {
				targetExchanges[name] = true
			}
		}
	}

	logger.InitLogger("error", "dev")
	// Create exchange clients. No API keys needed for public market data.
	httpPool := httpclient.NewPool(httpclient.DefaultPoolConfig())
	logCfg := sysconfig.LoggingConfig{
		HTTP: true,
	}

	mexcClient := mexc.NewClient(httpPool, "https://contract.mexc.com", "", "", logCfg)
	gateClient := gate.NewClient(httpPool, "https://api.gateio.ws/api/v4", "", "", logCfg)
	bybitClient := bybit.NewClient(httpPool, "https://api.bybit.com", "", "", "standard", logCfg)
	okxClient := okx.NewClient(httpPool, "https://www.okx.com", "", "", "", logCfg)
	// hlClient := hyperliquid.NewClient(context.Background(), httpPool, "https://api.hyperliquid.xyz", "", "", logCfg)
	// bitgetClient := bitget.NewClient(httpPool, "https://api.bitget.com", "", "", "", logCfg)
	// bingxClient := bingx.NewClient(httpPool, "https://open-api.bingx.com", "", "", logCfg)
	kucoinClient := kucoin.NewClient(httpPool, "https://api-futures.kucoin.com", "", "", "", logCfg)
	binanceClient := binance.NewClient(httpPool, "https://fapi.binance.com", "", "", logCfg)
	deepcoinClient := deepcoin.NewClient(httpPool, "https://api.deepcoin.com", "", "", "", logCfg)
	toobitClient := toobit.NewClient(httpPool, "https://api.toobit.com", "", "", logCfg)
	weexClient := weex.NewClient(httpPool, "https://api-contract.weex.com", logCfg)
	batonexClient := batonex.NewClient(httpPool, "https://api.batonex.com", logCfg)
	bitunixClient := bitunix.NewClient(httpPool, "https://fapi.bitunix.com", logCfg)
	// zoomexClient := zoomex.NewClient(httpPool, "https://openapi.zoomex.com", logCfg)
	bitmartClient := bitmart.NewClient(httpPool, "https://api-cloud-v2.bitmart.com", "", "", "", logCfg)
	coinwClient := coinw.NewClient(httpPool, "https://api.coinw.com", logCfg)
	kfClient := krakenfutures.NewClient(httpPool, "https://futures.kraken.com", logCfg)

	// Give a timeout context (30 seconds for extra safety)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*3)
	defer cancel()

	allClients := map[string]ScannerClient{
		"mexc":    mexcClient,
		"gate":    gateClient,
		"bybit":   bybitClient,
		"okx":     okxClient,
		"kucoin":  kucoinClient,
		"binance": binanceClient,
		// "hyperliquid": hlClient,
		// "bitget":      bitgetClient,
		// "bingx":       bingxClient,
		// "zoomex":        zoomexClient,
		"deepcoin":      deepcoinClient,
		"toobit":        toobitClient,
		"weex":          weexClient,
		"batonex":       batonexClient,
		"bitmart":       bitmartClient,
		"coinw":         coinwClient,
		"krakenfutures": kfClient,
		"bitunix":       bitunixClient,
	}

	// Filter clients based on user flag
	clients := make(map[string]ScannerClient)
	var scanList []string
	for name, client := range allClients {
		if targetExchanges == nil || targetExchanges[name] {
			clients[name] = client

			displayName := name
			switch name {
			case "mexc":
				displayName = "MEXC"
			case "gate":
				displayName = "Gate.io"
			case "bybit":
				displayName = "Bybit"
			case "okx":
				displayName = "OKX"
			case "hyperliquid":
				displayName = "Hyperliquid"
			case "bitget":
				displayName = "Bitget"
			case "bingx":
				displayName = "BingX"
			case "kucoin":
				displayName = "KuCoin"
			case "binance":
				displayName = "Binance"
			case "deepcoin":
				displayName = "Deepcoin"
			case "toobit":
				displayName = "Toobit"
			case "weex":
				displayName = "WEEX"
			case "batonex":
				displayName = "Batonex"
			case "bitunix":
				displayName = "Bitunix"
			case "zoomex":
				displayName = "Zoomex"
			case "bitmart":
				displayName = "Bitmart"
			case "coinw":
				displayName = "CoinW"
			case "krakenfutures":
				displayName = "Kraken Futures"
			}
			scanList = append(scanList, displayName)
		}
	}

	if len(clients) == 0 {
		fmt.Println("⚠️ No valid exchanges specified to scan. Exiting.")
		return
	}

	sort.Strings(scanList)
	fmt.Printf("🔍 Scanning %s Futures markets for top funding rates...\n", strings.Join(scanList, ", "))

	var opportunities []Opportunity
	var mu sync.Mutex
	var wg sync.WaitGroup
	blackList := []string{
		// BTC
		"BTC_USDT", "BTCUSDT", "BTC-USDT", "BTC-USDT-SWAP", "BTC",
		// ETH
		"ETH_USDT", "ETHUSDT", "ETH-USDT", "ETH-USDT-SWAP", "ETH",
		// SOL
		"SOL_USDT", "SOLUSDT", "SOL-USDT", "SOL-USDT-SWAP", "SOL",
		// BNB
		"BNB_USDT", "BNBUSDT", "BNB-USDT", "BNB-USDT-SWAP", "BNB",
		// XRP
		"XRP_USDT", "XRPUSDT", "XRP-USDT", "XRP-USDT-SWAP", "XRP",
		// ADA
		"ADA_USDT", "ADAUSDT", "ADA-USDT", "ADA-USDT-SWAP", "ADA",
		// DOT
		"DOT_USDT", "DOTUSDT", "DOT-USDT", "DOT-USDT-SWAP", "DOT",
		// DOGE
		"DOGE_USDT", "DOGEUSDT", "DOGE-USDT", "DOGE-USDT-SWAP", "DOGE",
		// LTC
		"LTC_USDT", "LTCUSDT", "LTC-USDT", "LTC-USDT-SWAP", "LTC",
		// TRX
		"TRX_USDT", "TRXUSDT", "TRX-USDT", "TRX-USDT-SWAP", "TRX",
	}
	for name, client := range clients {
		wg.Add(1)
		go func(exchangeName string, c ScannerClient) {
			defer wg.Done()
			results, err := c.GetPotentialFundingSymbols(ctx, minVol, 0, nil, blackList)
			if err != nil {
				fmt.Printf("🔴 Failed to fetch %s potential funding symbols: %v\n", strings.ToUpper(exchangeName), err)
				return
			}

			var localOpps []Opportunity
			for _, r := range results {
				if r.Rate == 0 {
					continue
				}

				if minFundingRate > 0 && math.Abs(r.Rate)*100 < minFundingRate {
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
		}(name, client)
	}

	wg.Wait()

	printOpportunities(opportunities)
}

type SymbolGroup struct {
	StandardSymbol string
	Score          float64
	ScoreRate      float64
	Opportunities  []Opportunity
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
		s = s + "USDT"
	}
	base := strings.TrimSuffix(s, "USDT")
	return base + "_USDT"
}

func printOpportunities(opportunities []Opportunity) {
	// Group opportunities by standardized symbol
	groupsMap := make(map[string][]Opportunity)
	for _, opp := range opportunities {
		stdSym := standardizeSymbol(opp.Symbol)
		groupsMap[stdSym] = append(groupsMap[stdSym], opp)
	}

	// Build the SymbolGroup slice
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

		// Sort opportunities inside this symbol group by Volume24h descending
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

	// Sort groups by absolute funding rate score descending
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Score > groups[j].Score
	})

	displayCount := min(len(groups), 15)

	fmt.Printf("✅ Scanned %d active pairs across exchanges. Displaying top %d opportunity groups:\n\n", len(opportunities), displayCount)

	now := time.Now()
	for i := range displayCount {
		g := groups[i]
		scorePct := g.ScoreRate * 100
		scoreSign := ""
		if scorePct > 0 {
			scoreSign = "+"
		}
		fmt.Printf("💎 GROUP %d: %s | Peak Rate: %s%.4f%%\n", i+1, g.StandardSymbol, scoreSign, scorePct)

		// Print a formatted table for opportunities within the group
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', tabwriter.AlignRight|tabwriter.Debug)
		_, _ = fmt.Fprintln(w, "   EXCHANGE\t SYMBOL\t PRICE\t FUNDING RATE (%)\t NEXT SETTLE IN\t TRADE DIRECTION\t 24H VOL (USDT)\t")
		_, _ = fmt.Fprintln(w, "   --------\t ------\t -----\t ----------------\t --------------\t ---------------\t --------------\t")

		for _, r := range g.Opportunities {
			// 1. Calculate Countdown
			settleTime := time.UnixMilli(r.NextSettleTime)
			countdown := max(settleTime.Sub(now).Round(time.Second), 0)

			// 2. Formatting Funding Rate
			frPct := r.FundingRate * 100
			frStr := fmt.Sprintf("%.4f%%", frPct)
			if frPct > 0 {
				frStr = "+" + frStr
			}

			// 3. Trade Direction
			direction := "LONG"
			if r.FundingRate < 0 {
				direction = "SHORT"
			}

			// 4. Volume formatted
			volStr := formatVolume(r.Volume24h)

			// 5. Price formatted
			priceStr := formatPrice(r.Price)

			_, _ = fmt.Fprintf(w, "   %s\t %s\t %s\t %s\t %s\t %s\t %s\t\n",
				strings.ToUpper(r.Exchange),
				r.Symbol,
				priceStr,
				frStr,
				countdown.String(),
				direction,
				volStr,
			)
		}
		_ = w.Flush()
		fmt.Println("   " + strings.Repeat("-", 85) + "\n")
	}

	fmt.Println("💡 Tip: Direction indicates what to open to ride the post-settlement reversion pump/dump.")
}

func formatPrice(price float64) string {
	if price == 0 {
		return "0.00"
	}
	if price < 0.001 {
		return fmt.Sprintf("%.8f", price)
	}
	if price < 1 {
		return fmt.Sprintf("%.6f", price)
	}
	if price < 10 {
		return fmt.Sprintf("%.4f", price)
	}
	return fmt.Sprintf("%.2f", price)
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
