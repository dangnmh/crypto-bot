package application

import (
	"context"
	"math"
	"time"

	"crypto-bot/internal/bots/funding_reversion/application/events"

	"github.com/ThreeDotsLabs/watermill/message"
)

// subscribeWait handles cycle.armed → sleep until T-2s → publish WaitComplete.
func (o *CycleOrchestrator) subscribeWait(ctx context.Context, settle time.Time) {
	o.consumeTopic(ctx, events.TopicArmed, func(_ *message.Message) {
		o.handleWait(ctx, settle)
	})
}

func (o *CycleOrchestrator) handleWait(ctx context.Context, settle time.Time) {
	o.waitUntil(ctx, settle.Add(-2*time.Second))

	o.publishOrLog(events.TopicWaitComplete, events.WaitCompleteEvent{
		Symbol: o.cfg.Symbol,
		Settle: settle,
	})
}

// subscribeRecheck handles cycle.wait.complete → verify FR → publish Confirmed or Abort.
func (o *CycleOrchestrator) subscribeRecheck(ctx context.Context) {
	o.consumeTopic(ctx, events.TopicWaitComplete, func(_ *message.Message) {
		o.handleRecheck(ctx)
	})
}

func (o *CycleOrchestrator) handleRecheck(ctx context.Context) {
	c := &o.candidate
	td, err := o.deps.TickerStore.GetTicker(ctx, c.Symbol)
	if err != nil {
		o.deps.Log.Warn("🟡 No ticker for recheck")
		o.abort("recheck", "no ticker")
		return
	}

	if (td.FundingRate > 0) != (c.FundingRate > 0) {
		o.deps.Log.Error("🔴 FR sign flip!",
			"old", c.FundingRate*100,
			"new", td.FundingRate*100,
		)
		o.abort("recheck", "FR sign flip")
		return
	}

	if math.Abs(td.FundingRate) < o.cfg.MinFundingRate {
		o.deps.Log.Warn("🟡 FR dropped below threshold",
			"fr", td.FundingRate*100,
			"min", o.cfg.MinFundingRate*100,
		)
		o.abort("recheck", "FR below threshold")
		return
	}

	o.deps.Log.Info("🟢 FR OK", "fr", td.FundingRate*100)
	o.publishOrLog(events.TopicConfirmed, events.ConfirmedEvent{
		Symbol:      c.Symbol,
		FundingRate: td.FundingRate,
		Side:        int(c.Side),
		CloseSide:   int(c.CloseSide),
	})
}
