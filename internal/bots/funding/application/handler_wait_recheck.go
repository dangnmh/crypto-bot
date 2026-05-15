package application

import (
	"context"
	"log/slog"
	"math"
	"time"

	"crypto-bot/internal/bots/funding/application/events"

	"github.com/ThreeDotsLabs/watermill/message"
)

// subscribeWait handles funding.reversion.armed → sleep until T-2s → publish wait_complete.
func (o *CycleOrchestrator) subscribeWait(ctx context.Context, settle time.Time) {
	o.consumeTopic(ctx, events.TopicReversionArmed, func(_ *message.Message) {
		o.handleWait(ctx, settle)
	})
}

func (o *CycleOrchestrator) handleWait(ctx context.Context, settle time.Time) {
	o.waitUntil(ctx, settle.Add(-2*time.Second))

	o.publishOrLog(events.TopicReversionWaitComplete, events.WaitCompleteEvent{
		Flow:   events.FlowReversion,
		Symbol: o.cfg.Symbol,
		Settle: settle,
	})
}

// subscribeRecheck handles funding.reversion.wait_complete → verify FR → publish confirmed or abort.
func (o *CycleOrchestrator) subscribeRecheck(ctx context.Context) {
	o.consumeTopic(ctx, events.TopicReversionWaitComplete, func(_ *message.Message) {
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
			slog.Float64("old", c.FundingRate*100),
			slog.Float64("new", td.FundingRate*100),
		)
		o.abort("recheck", "FR sign flip")
		return
	}

	if math.Abs(td.FundingRate) < o.cfg.MinFundingRate {
		o.deps.Log.Warn("🟡 FR dropped below threshold",
			slog.Float64("fr", td.FundingRate*100),
			slog.Float64("min", o.cfg.MinFundingRate*100),
		)
		o.abort("recheck", "FR below threshold")
		return
	}

	o.deps.Log.Info("🟢 FR OK", slog.Float64("fr", td.FundingRate*100))

	// Capture recheck FR for cycle record.
	o.recorder.FRAtRecheck = td.FundingRate

	o.publishOrLog(events.TopicReversionConfirmed, events.ConfirmedEvent{
		Flow:        events.FlowReversion,
		Symbol:      c.Symbol,
		FundingRate: td.FundingRate,
		Side:        c.Side,
		CloseSide:   c.CloseSide,
	})
}
