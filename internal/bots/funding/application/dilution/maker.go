package dilution

import (
	"context"
	"fmt"
	"math"
	"time"

	fundingconfig "crypto-bot/internal/bots/funding/config"
	shared "crypto-bot/internal/domain"
	infraapp "crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/trading/ordermanager"
	"crypto-bot/pkg/decmath"
	"crypto-bot/pkg/tradecalc"

	cache "github.com/patrickmn/go-cache"
)

// DilutionMaker generates PostOnly maker quotes (Limit Buy/Sell at BBO) to safely dilute volume.
type DilutionMaker struct {
	engine         EngineProviderGetter
	contractsCache *cache.Cache
}

// NewDilutionMaker creates a new DilutionMaker.
func NewDilutionMaker(engine EngineProviderGetter) (*DilutionMaker, error) {
	if engine == nil {
		return nil, fmt.Errorf("missing required dependency EngineProviderGetter for DilutionMaker")
	}
	return &DilutionMaker{
		engine:         engine,
		contractsCache: cache.New(24*time.Hour, 1*time.Hour),
	}, nil
}

// GenerateQuotes creates 1 or 2 PostOnly maker quotes based on current position breakdown and market depth.
func (m *DilutionMaker) GenerateQuotes(
	ctx context.Context,
	exchangeName string,
	cfg fundingconfig.ExchangeDilutionCfg,
	pos PositionSummary,
) ([]*DilutionSpec, error) {
	marketInfo := m.resolveMarketInfo(ctx, exchangeName, cfg.Symbol)
	if marketInfo.BestBid <= 0 || marketInfo.BestAsk <= 0 {
		return nil, fmt.Errorf("invalid BBO price for %s:%s (bid=%.4f, ask=%.4f)", exchangeName, cfg.Symbol, marketInfo.BestBid, marketInfo.BestAsk)
	}

	spreadOffset := float64(cfg.SpreadOffsetTicks) * marketInfo.PriceUnit
	buyPrice := marketInfo.BestBid - spreadOffset
	sellPrice := marketInfo.BestAsk + spreadOffset

	leverage := max(cfg.Leverage, 1)
	orderNotional := cfg.OrderNotionalUSD()
	marginUSDT := orderNotional / float64(leverage)

	params := quoteParams{
		exchangeName: exchangeName,
		cfg:          cfg,
		pos:          pos,
		marketInfo:   marketInfo,
		buyPrice:     buyPrice,
		sellPrice:    sellPrice,
		marginUSDT:   marginUSDT,
		leverage:     leverage,
	}

	return m.buildQuoteSpecs(params)
}

type quoteParams struct {
	exchangeName string
	cfg          fundingconfig.ExchangeDilutionCfg
	pos          PositionSummary
	marketInfo   MarketInfo
	buyPrice     float64
	sellPrice    float64
	marginUSDT   float64
	leverage     int
}

func (m *DilutionMaker) buildQuoteSpecs(p quoteParams) ([]*DilutionSpec, error) {
	if p.pos.LongVol > 0 && p.pos.ShortVol > 0 {
		return m.buildDualExitQuotes(p), nil
	}

	halfNotional := p.cfg.OrderNotionalUSD() * 0.5
	if p.pos.LongVol > 0 || p.pos.NetUSD >= halfNotional {
		return m.buildLongExitQuote(p), nil
	}

	if p.pos.ShortVol > 0 || p.pos.NetUSD <= -halfNotional {
		return m.buildShortExitQuote(p), nil
	}

	isCeilingReached := p.cfg.MaxPositionUSD > 0 && (p.pos.GrossUSD >= p.cfg.MaxPositionUSD || math.Abs(p.pos.NetUSD) >= p.cfg.MaxPositionUSD)
	if isCeilingReached {
		return nil, nil
	}

	return m.buildFlatQuotes(p), nil
}

func (m *DilutionMaker) buildDualExitQuotes(p quoteParams) []*DilutionSpec {
	var specs []*DilutionSpec
	if p.sellPrice > 0 {
		if vol := formatVolume(p.pos.LongVol, p.marketInfo); vol > 0 {
			specs = append(specs, m.makeSpec(p, shared.SideCloseLong, p.sellPrice, vol))
		}
	}
	if p.buyPrice > 0 {
		if vol := formatVolume(p.pos.ShortVol, p.marketInfo); vol > 0 {
			specs = append(specs, m.makeSpec(p, shared.SideCloseShort, p.buyPrice, vol))
		}
	}
	return specs
}

func (m *DilutionMaker) buildLongExitQuote(p quoteParams) []*DilutionSpec {
	if p.sellPrice <= 0 {
		return nil
	}
	closeVol := p.pos.LongVol
	if closeVol <= 0 {
		closeVol = computeOrderVolume(p.cfg.OrderNotionalUSD(), p.sellPrice, p.marketInfo.ContractSize, p.marketInfo)
	} else {
		closeVol = formatVolume(closeVol, p.marketInfo)
	}
	if closeVol <= 0 {
		return nil
	}
	return []*DilutionSpec{m.makeSpec(p, shared.SideCloseLong, p.sellPrice, closeVol)}
}

func (m *DilutionMaker) buildShortExitQuote(p quoteParams) []*DilutionSpec {
	if p.buyPrice <= 0 {
		return nil
	}
	closeVol := p.pos.ShortVol
	if closeVol <= 0 {
		closeVol = computeOrderVolume(p.cfg.OrderNotionalUSD(), p.buyPrice, p.marketInfo.ContractSize, p.marketInfo)
	} else {
		closeVol = formatVolume(closeVol, p.marketInfo)
	}
	if closeVol <= 0 {
		return nil
	}
	return []*DilutionSpec{m.makeSpec(p, shared.SideCloseShort, p.buyPrice, closeVol)}
}

func (m *DilutionMaker) buildFlatQuotes(p quoteParams) []*DilutionSpec {
	var specs []*DilutionSpec
	if p.buyPrice > 0 {
		if vol := computeOrderVolume(p.cfg.OrderNotionalUSD(), p.buyPrice, p.marketInfo.ContractSize, p.marketInfo); vol > 0 {
			specs = append(specs, m.makeSpec(p, shared.SideOpenLong, p.buyPrice, vol))
		}
	}
	if p.sellPrice > 0 {
		if vol := computeOrderVolume(p.cfg.OrderNotionalUSD(), p.sellPrice, p.marketInfo.ContractSize, p.marketInfo); vol > 0 {
			specs = append(specs, m.makeSpec(p, shared.SideOpenShort, p.sellPrice, vol))
		}
	}
	return specs
}

func (m *DilutionMaker) makeSpec(p quoteParams, side shared.Side, price, vol float64) *DilutionSpec {
	unfilledTimeout := time.Duration(p.cfg.UnfilledCancelTimeout)

	var tpPrice, slPrice float64
	if (side == shared.SideOpenLong || side == shared.SideOpenShort) && price > 0 {
		if p.cfg.TakeProfitPct > 0 {
			tpPrice = tradecalc.CalculateStaticTakeProfitPrice(
				tradecalc.Side(side),
				price,
				p.cfg.TakeProfitPct/100.0,
				p.marketInfo.PriceUnit,
				p.marketInfo.PriceScale,
			)
		}
		if p.cfg.StopLossPct > 0 {
			slPrice = tradecalc.CalculateStopLossPrice(
				tradecalc.Side(side),
				price,
				p.cfg.StopLossPct/100.0,
				p.marketInfo.PriceUnit,
				p.marketInfo.PriceScale,
			)
		}
	}

	return &DilutionSpec{
		Exchange:              p.exchangeName,
		Symbol:                p.cfg.Symbol,
		Side:                  side,
		NotionalUSDT:          p.cfg.OrderNotionalUSD(),
		MarginUSDT:            p.marginUSDT,
		Leverage:              p.leverage,
		Price:                 price,
		Volume:                vol,
		ContractSize:          p.marketInfo.ContractSize,
		PositionCloseTimeout:  time.Duration(p.cfg.PositionCloseTimeout),
		UnfilledCancelTimeout: unfilledTimeout,
		TakeProfitPrice:       tpPrice,
		StopLossPrice:         slPrice,
		OrderType:             ordermanager.OrderTypePostOnly,
		Vol24hUSDT:            p.marketInfo.Vol24hUSDT,
	}
}

func (m *DilutionMaker) resolveMarketInfo(ctx context.Context, exchangeName, symbol string) MarketInfo {
	info := MarketInfo{
		ContractSize: 1.0,
		PriceUnit:    0.01,
	}

	prov, err := m.engine.GetProvider(exchangeName)
	if err != nil || prov == nil || prov.Client == nil {
		return info
	}

	info = m.applyContractSpec(ctx, prov, exchangeName, symbol, info)
	info = m.applyTicker(ctx, prov, symbol, info)

	return info
}

func (m *DilutionMaker) applyTicker(ctx context.Context, prov *infraapp.ExchangeProvider, symbol string, info MarketInfo) MarketInfo {
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

func (m *DilutionMaker) applyContractSpec(ctx context.Context, prov *infraapp.ExchangeProvider, exchangeName, symbol string, info MarketInfo) MarketInfo {
	detail, err := m.getContractDetail(ctx, prov, exchangeName, symbol)
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

func (m *DilutionMaker) getContractDetail(ctx context.Context, prov *infraapp.ExchangeProvider, exchangeName, symbol string) (*exchange.ContractDetail, error) {
	cacheKey := exchangeName + ":" + symbol
	if val, found := m.contractsCache.Get(cacheKey); found {
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
		m.contractsCache.Set(exchangeName+":"+d.Symbol, &d, cache.DefaultExpiration)
		if d.Symbol == symbol {
			matched = &d
		}
	}

	return matched, nil
}

func computeOrderVolume(notional, price, contractSize float64, info MarketInfo) float64 {
	if price <= 0 {
		return notional
	}
	vol := tradecalc.CalculateVolumeForNotional(
		notional,
		price,
		contractSize,
		float64(info.MinVol),
		info.VolScale,
	)
	if vol <= 0 {
		return notional
	}
	return vol
}

func formatVolume(vol float64, info MarketInfo) float64 {
	if vol <= 0 {
		return 0
	}
	if info.VolScale >= 0 {
		return decmath.FloorToScale(vol, info.VolScale)
	}
	return vol
}
