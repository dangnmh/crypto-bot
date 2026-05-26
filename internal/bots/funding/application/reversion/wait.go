package reversion

import (
	"context"
	"log/slog"
	"time"
)

func (r *StatelessRunner) handleWait(ctx context.Context, armedEvt ArmedEvent) error {
	r.log.Info("handleWait SettleTime", slog.Time("settle", armedEvt.SettleTime))
	settleTime := armedEvt.SettleTime

	if settleTime.IsZero() {
		evt := WaitCompleteEvent{
			BaseReversionEvent: nextReversionBase(armedEvt.BaseReversionEvent, armedEvt.Symbol, r.deps.Clock.Now()),
			Candidate:          armedEvt.Candidate,
		}
		return r.publishEvent(ctx, TopicReversionWaitComplete, evt)
	}

	target := settleTime.Add(-2 * time.Second)
	if !r.WaitUntil(ctx, armedEvt.Symbol, target) {
		r.abortAfter(ctx, armedEvt.BaseReversionEvent, armedEvt.Symbol, "wait period context canceled")
		return context.Canceled
	}

	evt := WaitCompleteEvent{
		BaseReversionEvent: nextReversionBase(armedEvt.BaseReversionEvent, armedEvt.Symbol, r.deps.Clock.Now()),
		SettleTime:         settleTime,
		Candidate:          armedEvt.Candidate,
	}

	return r.publishEvent(ctx, TopicReversionWaitComplete, evt)
}
