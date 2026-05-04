package application

import (
	"context"
	"fmt"
	"time"

	shared "crypto-bot/internal/domain"

	"crypto-bot/internal/bots/funding_reversion/config"
	"crypto-bot/internal/bots/funding_reversion/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
)

// ──────────────────────────────────────────────────────────────────────
// symbolWorker helpers
// ──────────────────────────────────────────────────────────────────────.

func (w *symbolWorker) buildCandidate(td *store.TickerData) domain.Candidate {
	intent := domain.TradeIntent{
		Symbol:      td.Symbol,
		FundingRate: td.FundingRate,
	}
	if td.FundingRate > 0 {
		intent.Side, intent.CloseSide, intent.RefPriceType = shared.SideOpenLong, shared.SideCloseLong, "bestAsk"
	} else {
		intent.Side, intent.CloseSide, intent.RefPriceType = shared.SideOpenShort, shared.SideCloseShort, "bestBid"
	}

	return domain.Candidate{
		Config:      toTradeConfig(w.cfg),
		TradeIntent: intent,
		MarketData: domain.MarketData{
			LastPrice: td.LastPrice,
			BestBid:   td.BestBid,
			BestAsk:   td.BestAsk,
			Volume24:  td.Volume24,
			Amount24:  td.Amount24,
		},
		Phase: "SCANNING",
	}
}

func (w *symbolWorker) enrich(c *domain.Candidate) bool {
	cd, err := w.contractStore.GetContract(c.Symbol)
	if err != nil {
		w.log.Warn("🟡 No contract data — skip")
		return false
	}
	c.ContractSpec = domain.ContractSpec{
		PriceUnit:    cd.PriceUnit,
		VolUnit:      cd.VolUnit,
		MinVol:       cd.MinVol,
		PriceScale:   cd.PriceScale,
		VolScale:     cd.VolScale,
		ContractSize: cd.ContractSize,
		TakerFeeRate: cd.TakerFeeRate,
		MakerFeeRate: cd.MakerFeeRate,
	}
	return true
}

func (w *symbolWorker) refreshPrice(c *domain.Candidate) {
	if pd, err := w.priceStore.GetPrice(c.Symbol, 5*time.Second); err == nil {
		c.BestBid, c.BestAsk, c.LastPrice = pd.BestBid, pd.BestAsk, pd.LastPrice
	}
}

// initKlines fetches initial 1-minute klines via REST if we don't have enough data.
func (w *symbolWorker) initKlines(ctx context.Context) {
	klines := w.klineStore.GetKlines(w.cfg.Symbol)
	if len(klines) >= 14 {
		return
	}

	w.log.Info("📊 Fetching initial 1-minute klines via REST")
	apiKlines, err := w.client.GetKlines(ctx, w.cfg.Symbol, exchange.IntervalMin1, 0, 0)
	if err != nil {
		w.log.Warn("🟡 Failed to fetch initial klines", "error", err)
		return
	}

	if len(apiKlines) > 20 {
		apiKlines = apiKlines[len(apiKlines)-20:]
	}
	w.klineStore.InitKlines(w.cfg.Symbol, 20, apiKlines)
}

func (w *symbolWorker) nextSettleTime() (time.Time, error) {
	if w.cfg.SimulateSettle != "" {
		sim, err := time.Parse(time.RFC3339, w.cfg.SimulateSettle)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid simulateSettle datetime %q: %w", w.cfg.SimulateSettle, err)
		}
		if sim.After(time.Now().Add(time.Minute)) {
			return sim, nil
		}
	}

	st, err := w.fundingStore.GetSettleTime(w.cfg.Symbol)
	if err != nil {
		return time.Time{}, fmt.Errorf("settle time: %w", err)
	}
	return st, nil
}

func (w *symbolWorker) waitUntil(ctx context.Context, target time.Time) {
	if d := w.ts.Until(target); d > 0 {
		w.log.Debug("⏱️ wait", "target", target, "wait", d)
		select {
		case <-ctx.Done():
		case <-time.After(d):
		}
	}
}

func (w *symbolWorker) sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

// ──────────────────────────────────────────────────────────────────────
// Config → Domain translation
// ──────────────────────────────────────────────────────────────────────.

// toTradeConfig converts a config.SymbolConfig to a domain.TradeConfig.
// This is the anti-corruption layer between config and domain.
func toTradeConfig(sc config.SymbolConfig) domain.TradeConfig {
	return domain.TradeConfig{
		Symbol:              sc.Symbol,
		SimulateSettle:      sc.SimulateSettle,
		MaxPriceDiffPercent: sc.MaxPriceDiffPercent,
		MarginUSDT:          sc.MarginUSDT,
		Leverage:            sc.Leverage,
		TakeProfitPct:       sc.TakeProfitPct,
		StopLossPct:         sc.StopLossPct,
		EnableHedgeTrap:     sc.IsHedgeTrapEnabled(),
		TrapDepthPct:        sc.TrapDepthPct,
		TrapTakeProfitPct:   sc.TrapTakeProfitPct,
		TrapStopLossPct:     sc.TrapStopLossPct,
		TrapTrailingConfig: domain.TrailingConfig{
			Enabled:       sc.TrapTrailingConfig.Enabled,
			ActivationPct: sc.TrapTrailingConfig.ActivationPct,
			CallbackPct:   sc.TrapTrailingConfig.CallbackPct,
		},
		DynamicPricing: domain.DynamicPricingConfig{
			Enabled:                      sc.DynamicPricing.Enabled,
			SlippageMode:                 sc.DynamicPricing.SlippageMode,
			ObBufferPct:                  sc.DynamicPricing.ObBufferPct,
			ObMaxSlippagePct:             sc.DynamicPricing.ObMaxSlippagePct,
			ObStep:                       sc.DynamicPricing.ObStep,
			SpreadMultiplier:             sc.DynamicPricing.SpreadMultiplier,
			TpFundingMultiplier:          sc.DynamicPricing.TpFundingMultiplier,
			TpAtrMultiplier:              sc.DynamicPricing.TpAtrMultiplier,
			SlAtrMultiplier:              sc.DynamicPricing.SlAtrMultiplier,
			SlFundingMultiplier:          sc.DynamicPricing.SlFundingMultiplier,
			TrapDepthMultiplier:          sc.DynamicPricing.TrapDepthMultiplier,
			MinTrapDepth:                 sc.DynamicPricing.MinTrapDepth,
			MaxTrapDepth:                 sc.DynamicPricing.MaxTrapDepth,
			TrapTpMultiplier:             sc.DynamicPricing.TrapTpMultiplier,
			MinTrapTP:                    sc.DynamicPricing.MinTrapTP,
			MaxTrapTP:                    sc.DynamicPricing.MaxTrapTP,
			TrapSlMultiplier:             sc.DynamicPricing.TrapSlMultiplier,
			MinTrapSL:                    sc.DynamicPricing.MinTrapSL,
			MaxTrapSL:                    sc.DynamicPricing.MaxTrapSL,
			TrailingActivationMultiplier: sc.DynamicPricing.TrailingActivationMultiplier,
			MinActivation:                sc.DynamicPricing.MinActivation,
			MaxActivation:                sc.DynamicPricing.MaxActivation,
			TrailingCallbackMultiplier:   sc.DynamicPricing.TrailingCallbackMultiplier,
			MinCallback:                  sc.DynamicPricing.MinCallback,
			MaxCallback:                  sc.DynamicPricing.MaxCallback,
		},
		TrailingConfig: domain.TrailingConfig{
			Enabled:       sc.TrailingConfig.Enabled,
			ActivationPct: sc.TrailingConfig.ActivationPct,
			CallbackPct:   sc.TrailingConfig.CallbackPct,
		},
		ParsedOpenType:     sc.ParsedOpenType,
		ParsedPositionMode: sc.ParsedPositionMode,
	}
}

// exchangeOBToDomain converts an exchange.OrderBook to a shared domain.OrderBook.
func exchangeOBToDomain(ob *exchange.OrderBook) *shared.OrderBook {
	if ob == nil {
		return nil
	}
	asks := make([]shared.OrderBookEntry, len(ob.Asks))
	for i, a := range ob.Asks {
		asks[i] = shared.OrderBookEntry{Price: a.Price, Volume: a.Volume}
	}
	bids := make([]shared.OrderBookEntry, len(ob.Bids))
	for i, b := range ob.Bids {
		bids[i] = shared.OrderBookEntry{Price: b.Price, Volume: b.Volume}
	}
	return &shared.OrderBook{Symbol: ob.Symbol, Version: ob.Version, Asks: asks, Bids: bids}
}
