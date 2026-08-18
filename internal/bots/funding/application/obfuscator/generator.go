package obfuscator

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	fundingconfig "crypto-bot/internal/bots/funding/config"
	shared "crypto-bot/internal/domain"
	infraapp "crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/trading/ordermanager"
	"crypto-bot/pkg/tradecalc"

	cache "github.com/patrickmn/go-cache"
)

// OrderGenerator generates obfuscation specs based on profitable trade records and orderbook depth imbalance.
type OrderGenerator struct {
	engine         EngineProviderGetter
	contractsCache *cache.Cache
}

// NewOrderGenerator creates a new OrderGenerator instance with Engine injected and 24h contract spec cache.
func NewOrderGenerator(engine EngineProviderGetter) (*OrderGenerator, error) {
	if engine == nil {
		return nil, fmt.Errorf("missing required dependency EngineProviderGetter for OrderGenerator")
	}
	return &OrderGenerator{
		engine:         engine,
		contractsCache: cache.New(24*time.Hour, 1*time.Hour),
	}, nil
}

// GenerateSpec constructs an ObfuscationSpec for a symbol based on remaining loss budget or target sizing.
func (g *OrderGenerator) GenerateSpec(
	ctx context.Context,
	cfg fundingconfig.ExchangeObfuscationCfg,
	exchangeName, symbol string,
	targetLossUSD float64,
	originReqID string,
) (*ObfuscationSpec, error) {
	scaledNotional := cfg.OrderNotionalUSD()
	leverage := cfg.Leverage

	marketInfo := g.resolveMarketInfo(ctx, exchangeName, symbol, shared.SideOpenLong)
	side := marketInfo.Side
	refPrice := marketInfo.RefPrice
	contractSize := marketInfo.ContractSize

	iocPrice := computeIOCPrice(side, refPrice, cfg, marketInfo)
	volume := computeOrderVolume(scaledNotional, refPrice, contractSize, marketInfo)
	tpPrice, slPrice := computeTPSLPrices(side, refPrice, cfg, marketInfo)
	holdDuration := computeHoldDuration(cfg)

	return &ObfuscationSpec{
		OriginReqID:     originReqID,
		Exchange:        exchangeName,
		Symbol:          symbol,
		Side:            side,
		NotionalUSDT:    scaledNotional,
		MarginUSDT:      cfg.MarginUSDT,
		Leverage:        leverage,
		Price:           iocPrice,
		Volume:          volume,
		ContractSize:    contractSize,
		TakeProfitPrice: tpPrice,
		StopLossPrice:   slPrice,
		TakeProfitPct:   cfg.TakeProfitPct,
		StopLossPct:     cfg.StopLossPct,
		HoldDuration:    holdDuration,
		OrderType:       ordermanager.OrderTypeIOC,
		Vol24hUSDT:      marketInfo.Vol24hUSDT,
		FundingRate:     marketInfo.FundingRate,
	}, nil
}

// GenerateSpecForSymbol is an alias to GenerateSpec.
func (g *OrderGenerator) GenerateSpecForSymbol(
	ctx context.Context,
	cfg fundingconfig.ExchangeObfuscationCfg,
	exchangeName, symbol string,
	targetLossUSD float64,
	originReqID string,
) (*ObfuscationSpec, error) {
	return g.GenerateSpec(ctx, cfg, exchangeName, symbol, targetLossUSD, originReqID)
}

func (g *OrderGenerator) resolveMarketInfo(ctx context.Context, exchangeName, symbol string, prevSide shared.Side) MarketInfo {
	info := MarketInfo{
		ContractSize: 1.0,
		PriceUnit:    0.01,
		Side:         fallbackSide(prevSide),
	}

	prov, err := g.engine.GetProvider(exchangeName)
	if err != nil || prov == nil || prov.Client == nil {
		return info
	}

	info = g.applyContractSpec(ctx, prov, exchangeName, symbol, info)
	info = g.applyTicker(ctx, prov, symbol, info)
	info = g.applyFundingRate(ctx, prov, symbol, info)
	info = g.applyDepthMomentum(ctx, prov, symbol, prevSide, info)

	info.RefPrice = tradecalc.ExecutionRefPrice(tradecalc.Side(info.Side), info.LastPrice, info.BestBid, info.BestAsk)
	return info
}

func (g *OrderGenerator) applyTicker(ctx context.Context, prov *infraapp.ExchangeProvider, symbol string, info MarketInfo) MarketInfo {
	mdp, ok := prov.Client.(exchange.MarketDataProvider)
	if !ok {
		return info
	}

	tickers, err := mdp.GetTickers(ctx, symbol)
	if err != nil || len(tickers) == 0 {
		return info
	}

	var matched *exchange.Ticker
	for i := range tickers {
		if tickers[i].Symbol == symbol {
			matched = &tickers[i]
			break
		}
	}
	if matched == nil {
		matched = &tickers[0]
	}

	if matched.Bid1 > 0 {
		info.BestBid = matched.Bid1
	}
	if matched.Ask1 > 0 {
		info.BestAsk = matched.Ask1
	}
	if matched.LastPrice > 0 {
		info.LastPrice = matched.LastPrice
	}
	if matched.AmountUSDT24 > 0 {
		info.Vol24hUSDT = matched.AmountUSDT24
	} else if matched.Volume24 > 0 && info.LastPrice > 0 {
		info.Vol24hUSDT = matched.Volume24 * info.LastPrice
	}
	return info
}

func (g *OrderGenerator) applyFundingRate(ctx context.Context, prov *infraapp.ExchangeProvider, symbol string, info MarketInfo) MarketInfo {
	mdp, ok := prov.Client.(exchange.MarketDataProvider)
	if !ok {
		return info
	}

	rates, err := mdp.GetFundingRates(ctx, []string{symbol})
	if err != nil || len(rates) == 0 {
		return info
	}

	for i := range rates {
		if rates[i].Symbol == symbol {
			info.FundingRate = rates[i].Rate
			break
		}
	}
	return info
}

func (g *OrderGenerator) applyContractSpec(ctx context.Context, prov *infraapp.ExchangeProvider, exchangeName, symbol string, info MarketInfo) MarketInfo {
	detail, err := g.getContractDetail(ctx, prov, exchangeName, symbol)
	if err != nil || detail == nil {
		return info
	}
	if detail.ContractSize > 0 {
		info.ContractSize = detail.ContractSize
	}
	info.MinVol = detail.MinVol
	info.VolScale = detail.VolScale
	if detail.PriceUnit > 0 {
		info.PriceUnit = detail.PriceUnit
	}
	info.PriceScale = detail.PriceScale
	return info
}

func (g *OrderGenerator) applyDepthMomentum(ctx context.Context, prov *infraapp.ExchangeProvider, symbol string, prevSide shared.Side, info MarketInfo) MarketInfo {
	dp, ok := prov.Client.(exchange.DepthProvider)
	if !ok {
		return info
	}
	ob, err := dp.GetDepth(ctx, symbol)
	if err != nil || ob == nil {
		return info
	}

	var totalBid, totalAsk float64
	for i := range ob.Bids {
		totalBid += ob.Bids[i].Volume
	}
	for i := range ob.Asks {
		totalAsk += ob.Asks[i].Volume
	}

	switch {
	case totalBid > totalAsk:
		info.Side = shared.SideOpenLong
	case totalAsk > totalBid:
		info.Side = shared.SideOpenShort
	default:
		info.Side = fallbackSide(prevSide)
	}

	if len(ob.Bids) > 0 {
		info.BestBid = ob.Bids[0].Price
	}
	if len(ob.Asks) > 0 {
		info.BestAsk = ob.Asks[0].Price
	}
	return info
}

func (g *OrderGenerator) getContractDetail(ctx context.Context, prov *infraapp.ExchangeProvider, exchangeName, symbol string) (*exchange.ContractDetail, error) {
	cacheKey := exchangeName + ":" + symbol
	if val, found := g.contractsCache.Get(cacheKey); found {
		if detail, ok := val.(*exchange.ContractDetail); ok {
			return detail, nil
		}
	}

	mdp, ok := prov.Client.(exchange.MarketDataProvider)
	if !ok {
		return nil, nil
	}

	details, err := mdp.GetContractDetails(ctx)
	if err != nil {
		return nil, err
	}

	var matched *exchange.ContractDetail
	for i := range details {
		d := details[i]
		g.contractsCache.Set(exchangeName+":"+d.Symbol, &d, cache.DefaultExpiration)
		if d.Symbol == symbol {
			matched = &d
		}
	}

	return matched, nil
}

func fallbackSide(prevSide shared.Side) shared.Side {
	if prevSide.IsLong() {
		return shared.SideOpenShort
	}
	return shared.SideOpenLong
}

func computeOrderVolume(notional, refPrice, contractSize float64, info MarketInfo) float64 {
	if refPrice <= 0 {
		return notional
	}
	vol := tradecalc.CalculateVolumeForNotional(
		notional,
		refPrice,
		contractSize,
		float64(info.MinVol),
		info.VolScale,
	)
	if vol <= 0 {
		return notional
	}
	return vol
}

func computeTPSLPrices(side shared.Side, refPrice float64, cfg fundingconfig.ExchangeObfuscationCfg, info MarketInfo) (float64, float64) {
	var tpPrice, slPrice float64
	priceUnit := info.PriceUnit
	if priceUnit <= 0 {
		priceUnit = 0.01
	}

	if cfg.TakeProfitPct > 0 && refPrice > 0 {
		tpPrice = tradecalc.CalculateStaticTakeProfitPrice(
			tradecalc.Side(side),
			refPrice,
			cfg.TakeProfitPct/100.0,
			priceUnit,
			info.PriceScale,
		)
	}
	if cfg.StopLossPct > 0 && refPrice > 0 {
		slPrice = tradecalc.CalculateStopLossPrice(
			tradecalc.Side(side),
			refPrice,
			cfg.StopLossPct/100.0,
			priceUnit,
			info.PriceScale,
		)
	}
	return tpPrice, slPrice
}

func computeHoldDuration(cfg fundingconfig.ExchangeObfuscationCfg) time.Duration {
	holdSec := cfg.MinHoldSec
	if cfg.MaxHoldSec > cfg.MinHoldSec {
		holdSec += rand.Intn(cfg.MaxHoldSec - cfg.MinHoldSec + 1)
	}
	return time.Duration(holdSec) * time.Second
}

const defaultObfuscatorSlippagePct = 0.5

func computeIOCPrice(side shared.Side, refPrice float64, cfg fundingconfig.ExchangeObfuscationCfg, info MarketInfo) float64 {
	bestBid := info.BestBid
	bestAsk := info.BestAsk
	if bestBid <= 0 && info.LastPrice > 0 {
		bestBid = info.LastPrice
	}
	if bestAsk <= 0 && info.LastPrice > 0 {
		bestAsk = info.LastPrice
	}

	maxPriceDiff := cfg.MaxPriceDiffPercent
	if maxPriceDiff <= 0 {
		maxPriceDiff = defaultObfuscatorSlippagePct
	}

	priceUnit := info.PriceUnit
	if priceUnit <= 0 {
		priceUnit = 0.01
	}

	iocPrice, err := tradecalc.CalculateIOCPrice(
		tradecalc.Side(side),
		bestBid,
		bestAsk,
		maxPriceDiff,
		priceUnit,
		info.PriceScale,
	)
	if err != nil || iocPrice <= 0 {
		return refPrice
	}
	return iocPrice
}
