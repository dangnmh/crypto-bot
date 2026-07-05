package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"sync"
	"text/tabwriter"
	"time"

	sysconfig "crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/aevo"
	"crypto-bot/internal/infrastructure/exchange/apex"
	"crypto-bot/internal/infrastructure/exchange/ascendex"
	"crypto-bot/internal/infrastructure/exchange/aster"
	"crypto-bot/internal/infrastructure/exchange/backpack"
	"crypto-bot/internal/infrastructure/exchange/batonex"
	"crypto-bot/internal/infrastructure/exchange/binance"
	"crypto-bot/internal/infrastructure/exchange/bingx"
	"crypto-bot/internal/infrastructure/exchange/bitfinex"
	"crypto-bot/internal/infrastructure/exchange/bitget"
	"crypto-bot/internal/infrastructure/exchange/bitmart"
	"crypto-bot/internal/infrastructure/exchange/bitmex"
	"crypto-bot/internal/infrastructure/exchange/bitunix"
	"crypto-bot/internal/infrastructure/exchange/blofin"
	"crypto-bot/internal/infrastructure/exchange/btse"
	"crypto-bot/internal/infrastructure/exchange/bybit"
	"crypto-bot/internal/infrastructure/exchange/bydfi"
	"crypto-bot/internal/infrastructure/exchange/coinex"
	"crypto-bot/internal/infrastructure/exchange/coinw"
	"crypto-bot/internal/infrastructure/exchange/cryptocom"
	"crypto-bot/internal/infrastructure/exchange/deepcoin"
	"crypto-bot/internal/infrastructure/exchange/delta"
	"crypto-bot/internal/infrastructure/exchange/deribit"
	"crypto-bot/internal/infrastructure/exchange/digifinex"
	"crypto-bot/internal/infrastructure/exchange/dydx"
	"crypto-bot/internal/infrastructure/exchange/fameex"
	"crypto-bot/internal/infrastructure/exchange/fmfw"
	"crypto-bot/internal/infrastructure/exchange/gate"
	"crypto-bot/internal/infrastructure/exchange/gemini"
	"crypto-bot/internal/infrastructure/exchange/hashkey"
	"crypto-bot/internal/infrastructure/exchange/hibt"
	"crypto-bot/internal/infrastructure/exchange/hitbtc"
	"crypto-bot/internal/infrastructure/exchange/hotcoin"
	"crypto-bot/internal/infrastructure/exchange/htx"
	"crypto-bot/internal/infrastructure/exchange/hyperliquid"
	"crypto-bot/internal/infrastructure/exchange/ju"
	"crypto-bot/internal/infrastructure/exchange/krakenfutures"
	"crypto-bot/internal/infrastructure/exchange/kucoin"
	"crypto-bot/internal/infrastructure/exchange/lbank"
	"crypto-bot/internal/infrastructure/exchange/mandala"
	"crypto-bot/internal/infrastructure/exchange/mexc"
	"crypto-bot/internal/infrastructure/exchange/okx"
	"crypto-bot/internal/infrastructure/exchange/orangex"
	"crypto-bot/internal/infrastructure/exchange/phemex"
	"crypto-bot/internal/infrastructure/exchange/pionex"
	"crypto-bot/internal/infrastructure/exchange/poloniex"
	"crypto-bot/internal/infrastructure/exchange/sunx"
	"crypto-bot/internal/infrastructure/exchange/toobit"
	"crypto-bot/internal/infrastructure/exchange/weex"
	"crypto-bot/internal/infrastructure/exchange/whitebit"
	"crypto-bot/internal/infrastructure/exchange/woox"
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
		"gemini":        gemini.NewClient(httpPool, "https://api.gemini.com", "", "", logCfg),
		"toobit":        toobit.NewClient(httpPool, "https://api.toobit.com", "", "", logCfg),
		"weex":          weex.NewClient(httpPool, "https://api-contract.weex.com", "", "", "", logCfg),
		"batonex":       batonex.NewClient(httpPool, "https://api.batonex.com", logCfg),
		"zoomex":        zoomex.NewClient(httpPool, "https://openapi.zoomex.com", logCfg),
		"bitmart":       bitmart.NewClient(httpPool, "https://api-cloud-v2.bitmart.com", "", "", "", logCfg),
		"coinw":         coinw.NewClient(httpPool, "https://api.coinw.com", logCfg),
		"krakenfutures": krakenfutures.NewClient(httpPool, "https://futures.kraken.com", logCfg),
		"bitunix":       bitunix.NewClient(httpPool, "https://fapi.bitunix.com", "", "", logCfg),
		"xt":            xt.NewClient(httpPool, "https://fapi.xt.com", "", "", logCfg),
		"htx":           htx.NewClient(httpPool, "https://api.hbdm.com", logCfg),
		"lbank":         lbank.NewClient(httpPool, "https://lbkperp.lbank.com", logCfg),
		"mandala":       mandala.NewClient(httpPool, "https://api.wallet.mandala.exchange/api/3/public", "", "", logCfg),
		"orangex":       orangex.NewClient(httpPool, "https://api.orangex.com/api/v1", "", "", logCfg),
		"pionex":        pionex.NewClient(httpPool, "https://api.pionex.com", "", "", logCfg),
		"poloniex":      poloniex.NewClient(httpPool, "https://api.poloniex.com/v3", "", "", logCfg),
		"deribit":       deribit.NewClient(httpPool, "https://www.deribit.com", logCfg),
		"delta":         delta.NewClient(httpPool, "https://api.delta.exchange/v2", "", "", logCfg),
		"coinex":        coinex.NewClient(httpPool, "https://api.coinex.com/v2", logCfg),
		"bitfinex":      bitfinex.NewClient(httpPool, "https://api-pub.bitfinex.com", logCfg),
		"whitebit":      whitebit.NewClient(httpPool, "https://whitebit.com", logCfg),
		"dydx":          dydx.NewClient(httpPool, "https://indexer.dydx.trade", logCfg),
		"aster":         aster.NewClient(httpPool, "https://fapi.asterdex.com", "", "", "", logCfg),
		"ascendex":      ascendex.NewClient(httpPool, "https://ascendex.com/api/pro/v2", "", "", logCfg),
		"backpack":      backpack.NewClient(httpPool, "https://api.backpack.exchange/api/v1", "", "", logCfg),
		"aevo":          aevo.NewClient(httpPool, "https://api.aevo.xyz", "", "", logCfg),
		"apex":          apex.NewClient(httpPool, "https://omni.apex.exchange", "", "", logCfg),
		"btse":          btse.NewClient(httpPool, "https://api.btse.com/futures/api/v2.1", "", "", logCfg),
		"bitmex":        bitmex.NewClient(httpPool, "https://www.bitmex.com", logCfg),
		"hashkey":       hashkey.NewClient(httpPool, "https://api-glb.hashkey.com", slog.Default()),
		"hibt":          hibt.NewClient(httpPool, "https://fapi.hibt0.com/open-api", logCfg),
		"hitbtc":        hitbtc.NewClient(httpPool, "https://api.hitbtc.com/api/3/public", "", "", logCfg),
		"hotcoin":       hotcoin.NewClient(httpPool, "https://api-ct.hotcoin.fit", "", "", logCfg),
		"cryptocom":     cryptocom.NewClient(httpPool, "https://deriv-api.crypto.com/v1", slog.Default()),
		"ju":            ju.NewClient(httpPool, "https://api.jucoin.com", logCfg),
		"echobit":       ju.NewClient(httpPool, "https://api.jucoin.com", logCfg),
		"sunx":          sunx.NewClient(httpPool, "https://api.sunx.io", logCfg),
		"fameex":        fameex.NewClient(httpPool, "https://futuresopenapi.fameex.com", logCfg),
		"fmfw":          fmfw.NewClient(httpPool, "https://api.fmfw.io/api/3/public", "", "", logCfg),
		"woox":          woox.NewClient(httpPool, "https://api.woox.io", slog.Default()),
		"phemex":        phemex.NewClient(httpPool, "https://api.phemex.com", slog.Default()),
		"blofin":        blofin.NewClient(httpPool, "https://openapi.blofin.com", slog.Default()),
		"digifinex":     digifinex.NewClient(httpPool, "https://openapi.digifinex.com", slog.Default()),
		"bydfi":         bydfi.NewClient(httpPool, "https://api.bydfi.com/api", slog.Default()),
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
