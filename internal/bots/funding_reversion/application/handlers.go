package application

import (
	"context"
	"math"
	"time"

	shared "crypto-bot/internal/domain"

	"crypto-bot/internal/bots/funding_reversion/domain"
)

// ──────────────────────────────────────────────────────────────────────
// Phase handlers — each returns bool (success → next state, fail → abort)
// ──────────────────────────────────────────────────────────────────────.

// onScan checks the funding rate against the configured threshold.
func (w *symbolWorker) onScan(cs *CycleState) bool {
	td, err := w.tickerStore.GetTicker(w.cfg.Symbol)
	if err != nil {
		w.log.Warn("🟡 No ticker", "error", err)
		return false
	}

	if math.Abs(td.FundingRate) < w.cfg.MinFundingRate {
		w.log.Info("😴 FR below threshold", "fr", td.FundingRate*100)
		return false
	}

	cs.Candidate = w.buildCandidate(td)
	if !w.enrich(&cs.Candidate) {
		return false
	}

	w.log.Info("🔍 Qualified",
		"side", cs.Candidate.Side.String(),
		"fr", cs.Candidate.FundingRate*100,
	)
	return true
}

// onArm subscribes to WS, runs safety checks, and calculates IOC price + volume.
func (w *symbolWorker) onArm(ctx context.Context, cs *CycleState) bool {
	c := &cs.Candidate

	if c.Config.DynamicPricing.Enabled {
		w.initKlines(ctx)
	}

	w.subs.SubscribeAll()
	w.sleep(ctx, 2*time.Second)
	w.refreshPrice(c)

	if c.Config.DynamicPricing.Enabled {
		exKlines := w.klineStore.GetKlines(w.cfg.Symbol)
		klines := make([]shared.Kline, len(exKlines))
		for i, k := range exKlines {
			klines[i] = shared.Kline{Timestamp: k.Timestamp, Open: k.Open, Close: k.Close, High: k.High, Low: k.Low, Volume: k.Volume, Amount: k.Amount}
		}
		c.ATR = domain.CalculateATR(klines, 14)
		c.PrepareDynamicPricing()
		w.log.Info("📈 Dynamic Pricing", "ATR", c.ATR, "TP", c.Config.TakeProfitPct, "SL", c.Config.StopLossPct)
	}

	// c.CalculateIOCPrice(nil) evaluated here just to ensure parameters are valid and price is calculable.
	ioc, err := c.CalculateIOCPrice(nil)
	if err != nil {
		w.log.Warn("🟡 IOC calc failed", "error", err)
		w.subs.UnsubscribeAll()
		return false
	}
	c.Volume = c.CalculateVolume()

	// Record actual slippage % for safety evaluation and logging
	if ioc > 0 {
		var refPrice float64
		if c.Side == shared.SideOpenLong {
			refPrice = c.BestAsk
		} else {
			refPrice = c.BestBid
		}
		if refPrice > 0 {
			c.Slippage = math.Abs(ioc-refPrice) / refPrice * 100.0
		}
	}

	c.SafetyResult = c.EvaluateSafety(w.global.System.Safety.MaxImpactRatio)
	if !c.SafetyResult.Passed {
		w.log.Warn("🔴 Safety FAIL", "reason", c.SafetyResult.RejectReason)
		w.subs.UnsubscribeAll()
		return false
	}

	w.log.Info("🎯 Ready",
		"side", c.Side.String(),
		"fr", c.FundingRate*100,
		"ioc", ioc,
		"vol", c.Volume,
	)
	return true
}

// onWait sleeps until T-2s (server-synced).
func (w *symbolWorker) onWait(ctx context.Context, cs *CycleState) {
	w.waitUntil(ctx, cs.Settle.Add(-2*time.Second))
}

// onRecheck verifies the funding rate hasn't flipped sign at T-2s.
func (w *symbolWorker) onRecheck(cs *CycleState) bool {
	c := &cs.Candidate
	td, err := w.tickerStore.GetTicker(c.Symbol)
	if err != nil {
		w.log.Warn("🟡 No ticker for recheck")
		return false
	}

	if (td.FundingRate > 0) != (c.FundingRate > 0) {
		w.log.Error("🔴 FR sign flip!",
			"old", c.FundingRate*100,
			"new", td.FundingRate*100,
		)
		return false
	}

	if math.Abs(td.FundingRate) < w.cfg.MinFundingRate {
		w.log.Warn("🟡 FR dropped below threshold",
			"fr", td.FundingRate*100,
			"min", w.cfg.MinFundingRate*100,
		)
		return false
	}

	w.log.Info("🟢 FR OK", "fr", td.FundingRate*100)
	return true
}

// onFireIOC snapshots the peak price and submits the Sniper IOC order.
func (w *symbolWorker) onFireIOC(ctx context.Context, cs *CycleState) {
	c := &cs.Candidate
	settle := cs.Settle

	latencyMs := w.ts.LatencyMs()
	oneWayMs := latencyMs / 2
	bufferTime := time.Duration(w.global.System.Safety.BufferTime)

	fireOffset := time.Duration(oneWayMs)*time.Millisecond + bufferTime

	w.log.Info("⏱️ Firing configuration", "latency_rtt", latencyMs, "one_way", oneWayMs, "buffer", bufferTime, "total_offset", fireOffset)

	// Snapshot price before chaos begins
	snapshotOffset := 50 * time.Millisecond
	if fireOffset > snapshotOffset {
		snapshotOffset = fireOffset
	}
	w.waitUntil(ctx, settle.Add(-snapshotOffset))

	w.refreshPrice(c)

	// Refresh volume with latest price before OB sweep uses it
	c.Volume = c.CalculateVolume()

	var ob *shared.OrderBook
	if c.Config.DynamicPricing.Enabled && c.Config.DynamicPricing.SlippageMode == "OB_IMBALANCE" {
		if exOb, _ := w.depthStore.GetDepth(w.cfg.Symbol); exOb != nil {
			ob = exchangeOBToDomain(exOb)
		}
	}

	// Wait for precise fire moment, then shoot
	w.waitUntil(ctx, settle.Add(-fireOffset))
	res := FireIOC(ctx, w.client, c, w.ts, w.log, ob)
	cs.Results = append(cs.Results, res)

	if res.IsSuccess() {
		resCopy := res
		w.trailing.SetupFillCallback(res.OrderID, &resCopy)
	}
}

// onFireTrap submits the Limit Trap order after settlement to catch the wick.
func (w *symbolWorker) onFireTrap(ctx context.Context, cs *CycleState) {
	c := &cs.Candidate
	settle := cs.Settle
	trapOffset := time.Duration(w.global.System.Safety.TrapAfterSettle)

	w.waitUntil(ctx, settle.Add(trapOffset))
	res := FireLimitTrap(ctx, w.client, c, w.ts, w.log)
	cs.Results = append(cs.Results, res)

	if res.IsSuccess() {
		resCopy := res
		w.trailing.SetupFillCallback(res.OrderID, &resCopy)
	}
}
