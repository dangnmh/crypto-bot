package application

import (
	"context"
	"math"
	"time"

	shared "crypto-bot/internal/domain"

	"crypto-bot/internal/bots/funding_reversion/application/events"
	"crypto-bot/internal/bots/funding_reversion/domain"
	"crypto-bot/pkg/decmath"

	"github.com/ThreeDotsLabs/watermill/message"
)

// subscribeScan handles cycle.start → checks FR → publishes CandidateFound or Abort.
func (o *CycleOrchestrator) subscribeScan(ctx context.Context) {
	o.consumeTopic(ctx, events.TopicCycleStart, func(_ *message.Message) {
		o.handleScan(ctx)
	})
}

func (o *CycleOrchestrator) handleScan(ctx context.Context) {
	td, err := o.deps.TickerStore.GetTicker(ctx, o.cfg.Symbol)
	if err != nil {
		o.deps.Log.Warn("🟡 No ticker", "error", err)
		o.abort("scan", "no ticker data")
		return
	}

	if math.Abs(td.FundingRate) < o.cfg.MinFundingRate {
		o.deps.Log.Info("😴 FR below threshold", "fr", td.FundingRate*100)
		o.abort("scan", "FR below threshold")
		return
	}

	o.candidate = o.buildCandidate(td)
	if !o.enrich(ctx, &o.candidate) {
		o.abort("scan", "enrichment failed")
		return
	}

	o.deps.Log.Info("🔍 Qualified",
		"side", o.candidate.Side.String(),
		"fr", o.candidate.FundingRate*100,
	)

	o.publishOrLog(events.TopicCandidateFound, events.CandidateFoundEvent{
		Symbol:      o.candidate.Symbol,
		FundingRate: o.candidate.FundingRate,
		Side:        int(o.candidate.Side),
		CloseSide:   int(o.candidate.CloseSide),
		LastPrice:   o.candidate.LastPrice,
	})
}

// subscribeArm handles cycle.candidate.found → WS subscribe, calc IOC, safety check.
func (o *CycleOrchestrator) subscribeArm(ctx context.Context) {
	o.consumeTopic(ctx, events.TopicCandidateFound, func(_ *message.Message) {
		o.handleArm(ctx)
	})
}

func (o *CycleOrchestrator) handleArm(ctx context.Context) {
	c := &o.candidate

	if c.Config.FundingReversion.DynamicPricing.Enabled {
		o.initKlines(ctx)
	}

	if err := o.subs.SubscribeAll(); err != nil {
		o.deps.Log.Error("🔴 Failed to subscribe WS channels", "error", err)
		o.abort("arm", "WS subscribe failed")
		return
	}

	o.sleep(ctx, 2*time.Second)
	if err := o.refreshPrice(ctx, c); err != nil {
		o.deps.Log.Warn("🟡 Refresh price failed", "error", err)
		o.subs.UnsubscribeAll()
		o.abort("arm", "refresh price failed")
		return
	}

	if c.Config.FundingReversion.DynamicPricing.Enabled {
		klines := o.deps.KlineStore.GetKlines(ctx, o.cfg.Symbol)
		c.ATR = domain.CalculateATR(klines, 14)
		c.PrepareDynamicPricing()
		o.deps.Log.Info("📈 Dynamic Pricing", "ATR", c.ATR, "TP", c.Config.FundingReversion.TakeProfitPct, "SL", c.Config.FundingReversion.StopLossPct)
	}

	ioc, err := c.CalculateIOCPrice(nil)
	if err != nil {
		o.deps.Log.Warn("🟡 IOC calc failed", "error", err)
		o.subs.UnsubscribeAll()
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
		o.deps.Log.Warn("🔴 Safety FAIL", "reason", c.SafetyResult.RejectReason)
		o.subs.UnsubscribeAll()
		o.abort("arm", c.SafetyResult.RejectReason)
		return
	}

	o.deps.Log.Info("🎯 Ready",
		"side", c.Side.String(),
		"fr", c.FundingRate*100,
		"ioc", ioc,
		"vol", c.Volume,
	)

	o.publishOrLog(events.TopicArmed, events.ArmedEvent{
		Symbol: c.Symbol,
	})
}
