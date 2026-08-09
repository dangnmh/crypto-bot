package jupiter

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"crypto-bot/internal/infrastructure/exchange"
)

type jupiterMarketStats struct {
	Price  string `json:"price"`
	Volume string `json:"volume"`
}

type jupiterPoolInfo struct {
	LongBorrowRatePercent string `json:"longBorrowRatePercent"`
}

const (
	MintSol = "So11111111111111111111111111111111111111112"
	MintBtc = "3NZ9JMVBmGAqocybic2c7LQCJScmgsAZ6vQqTDzcqmJh"
	MintEth = "7vfCXTUXx5WJV5JADk17DUJ4ksgau7utNKj4b963voxs"

	SymbolSolPerp = "SOL-PERP"
	SymbolBtcPerp = "BTC-PERP"
	SymbolEthPerp = "ETH-PERP"

	BaseSol = "SOL"
	BaseBtc = "BTC"
	BaseEth = "ETH"
)

type jupiterMarketDef struct {
	Mint   string
	Symbol string
	Base   string
}

var jupiterMarkets = []jupiterMarketDef{
	{Mint: MintSol, Symbol: SymbolSolPerp, Base: BaseSol},
	{Mint: MintBtc, Symbol: SymbolBtcPerp, Base: BaseBtc},
	{Mint: MintEth, Symbol: SymbolEthPerp, Base: BaseEth},
}

// GetPotentialFundingSymbols fetches all perpetual contracts, their tickers, and estimated funding rates.
func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	whitelistMap := make(map[string]bool)
	for _, sym := range whitelist {
		whitelistMap[strings.ToUpper(sym)] = true
	}
	blacklistMap := make(map[string]bool)
	for _, sym := range blacklist {
		blacklistMap[strings.ToUpper(sym)] = true
	}

	var targeted []jupiterMarketDef
	for _, m := range jupiterMarkets {
		symbolUpper := strings.ToUpper(m.Symbol)
		baseUpper := strings.ToUpper(m.Base)
		if isAllowedSymbol(symbolUpper, baseUpper, whitelistMap, blacklistMap) {
			targeted = append(targeted, m)
		}
	}

	if len(targeted) == 0 {
		return nil, nil
	}

	type fetchResult struct {
		res exchange.PotentialFundingResult
		err error
	}

	resultsChan := make(chan fetchResult, len(targeted))
	var wg sync.WaitGroup

	for _, m := range targeted {
		wg.Add(1)
		go func(item jupiterMarketDef) {
			defer wg.Done()
			res, err := c.fetchMarketData(ctx, item)
			resultsChan <- fetchResult{res: res, err: err}
		}(m)
	}

	wg.Wait()
	close(resultsChan)

	var results []exchange.PotentialFundingResult
	for r := range resultsChan {
		if r.err != nil {
			c.logger.Debug("failed to fetch jupiter market data", "error", r.err)
			continue
		}

		if r.res.Volume24h < minVol24h {
			continue
		}
		if maxVol24h > 0 && r.res.Volume24h > maxVol24h {
			continue
		}

		results = append(results, r.res)
	}

	return results, nil
}

func (c *Client) fetchMarketData(ctx context.Context, m jupiterMarketDef) (exchange.PotentialFundingResult, error) {
	query := map[string]string{"mint": m.Mint}

	// 1. Fetch market stats
	statsBytes, err := c.request(ctx, "GET", "/v1/market-stats", query)
	if err != nil {
		return exchange.PotentialFundingResult{}, fmt.Errorf("fetch stats: %w", err)
	}

	var stats jupiterMarketStats
	if err := json.Unmarshal(statsBytes, &stats); err != nil {
		return exchange.PotentialFundingResult{}, fmt.Errorf("unmarshal stats: %w", err)
	}

	// 2. Fetch pool info
	poolBytes, err := c.request(ctx, "GET", "/v1/pool-info", query)
	if err != nil {
		return exchange.PotentialFundingResult{}, fmt.Errorf("fetch pool info: %w", err)
	}

	var pool jupiterPoolInfo
	if err := json.Unmarshal(poolBytes, &pool); err != nil {
		return exchange.PotentialFundingResult{}, fmt.Errorf("unmarshal pool info: %w", err)
	}

	price, _ := strconv.ParseFloat(stats.Price, 64)
	vol24h, _ := strconv.ParseFloat(stats.Volume, 64)
	ratePercent, _ := strconv.ParseFloat(pool.LongBorrowRatePercent, 64)
	rate := ratePercent / 100.0

	return exchange.PotentialFundingResult{
		Symbol:     strings.ToUpper(m.Symbol),
		Rate:       rate,
		SettleTime: 0,
		Volume24h:  vol24h,
		Price:      price,
	}, nil
}

func isAllowedSymbol(symbolUpper, baseUpper string, whitelistMap, blacklistMap map[string]bool) bool {
	if symbolUpper == "" {
		return false
	}

	normSymbol := symbolUpper
	for _, suffix := range []string{"_PERP", "-PERP", "PERP"} {
		if before, ok := strings.CutSuffix(symbolUpper, suffix); ok {
			normSymbol = before
			break
		}
	}

	if blacklistMap[symbolUpper] || blacklistMap[normSymbol] || blacklistMap[baseUpper] {
		return false
	}
	if len(whitelistMap) > 0 && !whitelistMap[symbolUpper] && !whitelistMap[normSymbol] && !whitelistMap[baseUpper] {
		return false
	}
	return true
}

func (c *Client) GetTopGainer(_ context.Context, _ exchange.TopGainerRequest) ([]exchange.TopGainerResult, error) {
	return nil, nil
}
