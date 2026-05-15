package application

import (
	"context"
	"log/slog"
	"math"
	"time"

	shared "crypto-bot/internal/domain"

	"crypto-bot/internal/bots/funding/application/events"
	"crypto-bot/internal/bots/funding/domain"
	"crypto-bot/pkg/decmath"

	"github.com/ThreeDotsLabs/watermill/message"
)

// subscribeScan handles funding.scan.start → checks FR → publishes flow candidates or abort.
func (o *CycleOrchestrator) subscribeScan(ctx context.Context) {
	o.consumeTopic(ctx, events.TopicScanStart, func(_ *message.Message) {
		o.handleScan(ctx)
	})
}

func (o *CycleOrchestrator) handleScan(ctx context.Context) {
	td, err := o.deps.TickerStore.GetTicker(ctx, o.cfg.Symbol)
	if err != nil {
		o.deps.Log.Warn("🟡 No ticker", slog.Any("error", err))
		o.abort("scan", "no ticker data")
		return
	}

	if math.Abs(td.FundingRate) < o.cfg.MinFundingRate {
		o.deps.Log.Info("😴 FR below threshold", slog.Float64("fr", td.FundingRate*100))
		o.abort("scan", "FR below threshold")
		return
	}

	o.candidate = o.buildCandidate(td)
	if !o.enrich(ctx, &o.candidate) {
		o.abort("scan", "enrichment failed")
		return
	}

	// Capture scan data for cycle record.
	o.recorder.FRAtScan = td.FundingRate
	o.recorder.Side = o.candidate.Side
	spread := calcSpreadPct(o.candidate.BestBid, o.candidate.BestAsk)
	o.recorder.AddSnapshot(domain.MarketSnapshot{
		Phase:     domain.PhaseScan,
		LastPrice: o.candidate.LastPrice,
		BestBid:   o.candidate.BestBid,
		BestAsk:   o.candidate.BestAsk,
		Spread:    spread,
	})

	o.deps.Log.Info("🔍 Qualified",
		slog.String("side", o.candidate.Side.String()),
		slog.Float64("fr", o.candidate.FundingRate*100),
	)

	scanEvent := events.CandidateFoundEvent{
		Symbol:      o.candidate.Symbol,
		FundingRate: o.candidate.FundingRate,
		Side:        o.candidate.Side,
		CloseSide:   o.candidate.CloseSide,
		LastPrice:   o.candidate.LastPrice,
	}
	o.publishOrLog(events.TopicScanCandidateFound, scanEvent)

	reversionEvent := scanEvent
	reversionEvent.Flow = events.FlowReversion
	o.publishOrLog(events.TopicReversionCandidate, reversionEvent)
	if o.cfg.IsHedgeTrapEnabled() {
		trapEvent := scanEvent
		trapEvent.Flow = events.FlowTrap
		trapEvent.Side = o.candidate.Side.Opposite()
		trapEvent.CloseSide = shared.CloseSideFor(trapEvent.Side)
		o.publishOrLog(events.TopicTrapCandidate, trapEvent)
	}
}

// subscribeArm handles funding.reversion.candidate → WS subscribe, calc IOC, safety check.
func (o *CycleOrchestrator) subscribeArm(ctx context.Context) {
	o.consumeTopic(ctx, events.TopicReversionCandidate, func(_ *message.Message) {
		o.handleArm(ctx)
	})
}

func (o *CycleOrchestrator) handleArm(ctx context.Context) {
	c := &o.candidate

	if c.Config.FundingReversion.DynamicPricing.Enabled {
		o.initKlines(ctx)
	}

	if err := o.subs.SubscribeAll(ctx); err != nil {
		o.deps.Log.Error("🔴 Failed to subscribe WS channels", slog.Any("error", err))
		o.abort("arm", "WS subscribe failed")
		return
	}

	o.sleep(ctx, 2*time.Second)
	if err := o.refreshPrice(ctx, c); err != nil {
		o.deps.Log.Warn("🟡 Refresh price failed", slog.Any("error", err))
		o.subs.UnsubscribeAll(ctx)
		o.abort("arm", "refresh price failed")
		return
	}

	if c.Config.FundingReversion.DynamicPricing.Enabled {
		klines := o.deps.KlineStore.GetKlines(ctx, o.cfg.Symbol)
		c.ATR = domain.CalculateATR(klines, 14)
		c.PrepareDynamicPricing()
		o.deps.Log.Info("📈 Dynamic Pricing",
			slog.Float64("ATR", c.ATR),
			slog.Float64("TP", c.Config.FundingReversion.TakeProfitPct),
			slog.Float64("SL", c.Config.FundingReversion.StopLossPct),
		)
	}

	ioc, err := c.CalculateIOCPrice(nil)
	if err != nil {
		o.deps.Log.Warn("🟡 IOC calc failed", slog.Any("error", err))
		o.subs.UnsubscribeAll(ctx)
		o.abort("arm", "IOC calc failed")
		return
	}
	c.Volume = c.CalculateVolume()

	if ioc > 0 {
		var refPrice float64
		if c.Side == shared.SideOpenLong {
			refPrice = c.BestAsk
		} else {
			refPrice = c.BestBid
		}
		if refPrice > 0 {
			c.Slippage = decmath.Mul(decmath.Div(math.Abs(decmath.Sub(ioc, refPrice)), refPrice), 100.0)
		}
	}

	c.SafetyResult = c.EvaluateSafety(o.global.System.Safety.MaxImpactRatio)
	if !c.SafetyResult.Passed {
		o.recorder.SafetyPassed = false
		o.recorder.SafetyRejectReason = c.SafetyResult.RejectReason
		o.deps.Log.Warn("🔴 Safety FAIL", slog.String("reason", c.SafetyResult.RejectReason))
		o.subs.UnsubscribeAll(ctx)
		o.abort("arm", c.SafetyResult.RejectReason)
		return
	}
	o.recorder.SafetyPassed = true
	o.recorder.TrapEnabled = o.cfg.IsHedgeTrapEnabled()

	// Capture dynamic pricing data for comparison.
	if c.Config.FundingReversion.DynamicPricing.Enabled {
		o.recorder.DynamicEnabled = true
		o.recorder.DynamicTPPct = c.Config.FundingReversion.TakeProfitPct
		o.recorder.DynamicSLPct = c.Config.FundingReversion.StopLossPct
		o.recorder.ATRValue = c.ATR
	}

	pd, err := o.deps.PriceStore.GetPrice(ctx, o.cfg.Symbol, 5*time.Second)
	if err == nil {
		spread := calcSpreadPct(pd.BestBid, pd.BestAsk)
		o.recorder.AddSnapshot(domain.MarketSnapshot{
			Phase:     domain.PhaseArm,
			LastPrice: pd.LastPrice,
			BestBid:   pd.BestBid,
			BestAsk:   pd.BestAsk,
			Spread:    spread,
		})
	}

	o.deps.Log.Info("🎯 Ready",
		slog.String("side", c.Side.String()),
		slog.Float64("fr", c.FundingRate*100),
		slog.Float64("ioc", ioc),
		slog.Float64("vol", c.Volume),
	)

	o.publishOrLog(events.TopicReversionArmed, events.ArmedEvent{
		Flow:   events.FlowReversion,
		Symbol: c.Symbol,
	})
}
