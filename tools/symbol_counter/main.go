package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"
	"text/tabwriter"
	"time"

	sysconfig "crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/batonex"
	"crypto-bot/internal/infrastructure/exchange/binance"
	"crypto-bot/internal/infrastructure/exchange/bingx"
	"crypto-bot/internal/infrastructure/exchange/bitget"
	"crypto-bot/internal/infrastructure/exchange/bitmart"
	"crypto-bot/internal/infrastructure/exchange/bitunix"
	"crypto-bot/internal/infrastructure/exchange/bybit"
	"crypto-bot/internal/infrastructure/exchange/coinex"
	"crypto-bot/internal/infrastructure/exchange/coinw"
	"crypto-bot/internal/infrastructure/exchange/deepcoin"
	"crypto-bot/internal/infrastructure/exchange/deribit"
	"crypto-bot/internal/infrastructure/exchange/gate"
	"crypto-bot/internal/infrastructure/exchange/htx"
	"crypto-bot/internal/infrastructure/exchange/hyperliquid"
	"crypto-bot/internal/infrastructure/exchange/krakenfutures"
	"crypto-bot/internal/infrastructure/exchange/kucoin"
	"crypto-bot/internal/infrastructure/exchange/lbank"
	"crypto-bot/internal/infrastructure/exchange/mexc"
	"crypto-bot/internal/infrastructure/exchange/okx"
	"crypto-bot/internal/infrastructure/exchange/orangex"
	"crypto-bot/internal/infrastructure/exchange/pionex"
	"crypto-bot/internal/infrastructure/exchange/toobit"
	"crypto-bot/internal/infrastructure/exchange/weex"
	"crypto-bot/internal/infrastructure/exchange/xt"
	"crypto-bot/internal/infrastructure/exchange/zoomex"
	"crypto-bot/pkg/httpclient"
)

type Result struct {
	Exchange string
	Count    int
	Method   string
	Err      error
}

func fetchSymbolCount(ctx context.Context, client any) (int, string, error) {
	// Try GetContractDetails
	if lister, ok := client.(interface {
		GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error)
	}); ok {
		details, err := lister.GetContractDetails(ctx)
		if err == nil {
			return len(details), "GetContractDetails", nil
		}
	}

	// Fallback to GetPotentialFundingSymbols
	if lister, ok := client.(interface {
		GetPotentialFundingSymbols(ctx context.Context, minVol24h, maxVol24h float64, whitelist []string, blacklist []string) ([]exchange.PotentialFundingResult, error)
	}); ok {
		results, err := lister.GetPotentialFundingSymbols(ctx, 0, 0, nil, nil)
		if err == nil {
			return len(results), "GetPotentialFundingSymbols", nil
		}
		return 0, "", err
	}

	return 0, "", fmt.Errorf("no supported symbol retrieval method")
}

func main() {
	httpPool := httpclient.NewPool(httpclient.DefaultPoolConfig())
	logCfg := sysconfig.LoggingConfig{
		HTTP: false,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	clients := map[string]any{
		"binance":       binance.NewClient(httpPool, "https://fapi.binance.com", "", "", logCfg),
		"bybit":         bybit.NewClient(httpPool, "https://api.bybit.com", "", "", "standard", logCfg),
		"okx":           okx.NewClient(httpPool, "https://www.okx.com", "", "", "", logCfg),
		"mexc":          mexc.NewClient(httpPool, "https://contract.mexc.com", "", "", logCfg),
		"gate":          gate.NewClient(httpPool, "https://api.gateio.ws/api/v4", "", "", logCfg),
		"bingx":         bingx.NewClient(httpPool, "https://open-api.bingx.com", "", "", logCfg),
		"bitget":        bitget.NewClient(httpPool, "https://api.bitget.com", "", "", "", logCfg),
		"kucoin":        kucoin.NewClient(httpPool, "https://api-futures.kucoin.com", "", "", "", logCfg),
		"hyperliquid":   hyperliquid.NewClient(ctx, httpPool, "https://api.hyperliquid.xyz", "", "", logCfg),
		"deepcoin":      deepcoin.NewClient(httpPool, "https://api.deepcoin.com", "", "", "", logCfg),
		"toobit":        toobit.NewClient(httpPool, "https://api.toobit.com", "", "", logCfg),
		"weex":          weex.NewClient(httpPool, "https://api-contract.weex.com", "", "", "", logCfg),
		"batonex":       batonex.NewClient(httpPool, "https://api.batonex.com", logCfg),
		"zoomex":        zoomex.NewClient(httpPool, "https://openapi.zoomex.com", logCfg),
		"bitmart":       bitmart.NewClient(httpPool, "https://api-cloud-v2.bitmart.com", "", "", "", logCfg),
		"coinw":         coinw.NewClient(httpPool, "https://api.coinw.com", logCfg),
		"krakenfutures": krakenfutures.NewClient(httpPool, "https://futures.kraken.com", logCfg),
		"bitunix":       bitunix.NewClient(httpPool, "https://fapi.bitunix.com", "", "", logCfg),
		"xt":            xt.NewClient(httpPool, "https://fapi.xt.com", logCfg),
		"htx":           htx.NewClient(httpPool, "https://api.hbdm.com", logCfg),
		"lbank":         lbank.NewClient(httpPool, "https://lbkperp.lbank.com", logCfg),
		"orangex":       orangex.NewClient(httpPool, "https://api.orangex.com/api/v1", logCfg),
		"pionex":        pionex.NewClient(httpPool, "https://api.pionex.com", logCfg),
		"deribit":       deribit.NewClient(httpPool, "https://www.deribit.com", logCfg),
		"coinex":        coinex.NewClient(httpPool, "https://api.coinex.com/v2", logCfg),
	}

	var results []Result
	var mu sync.Mutex
	var wg sync.WaitGroup

	for name, client := range clients {
		wg.Add(1)
		go func(exName string, cl any) {
			defer wg.Done()
			count, method, err := fetchSymbolCount(ctx, cl)
			mu.Lock()
			results = append(results, Result{
				Exchange: exName,
				Count:    count,
				Method:   method,
				Err:      err,
			})
			mu.Unlock()
		}(name, client)
	}

	wg.Wait()

	// Sort results by symbol count descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Count > results[j].Count
	})

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "EXCHANGE\tACTIVE SYMBOLS\tMETHOD\tSTATUS")
	fmt.Fprintln(w, "--------\t--------------\t------\t------")

	total := 0
	for _, r := range results {
		status := "SUCCESS"
		if r.Err != nil {
			status = fmt.Sprintf("FAILED: %v", r.Err)
		}
		fmt.Fprintf(w, "%s\t%d\t%s\t%s\n", r.Exchange, r.Count, r.Method, status)
		total += r.Count
	}
	fmt.Fprintln(w, "--------\t--------------\t------\t------")
	fmt.Fprintf(w, "TOTAL\t%d\t\t\n", total)
	w.Flush()
}
