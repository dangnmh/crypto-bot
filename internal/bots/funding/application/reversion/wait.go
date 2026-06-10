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

	target := settleTime.Add(-5 * time.Second)
	if err := r.waitUntilFuture(ctx, armedEvt.Symbol, target); err != nil {
		r.abortAfter(ctx, armedEvt.BaseReversionEvent, armedEvt.Symbol, ReversionReason("wait period failed: "+err.Error()))
		return err
	}

	evt := WaitCompleteEvent{
		BaseReversionEvent: nextReversionBase(armedEvt.BaseReversionEvent, armedEvt.Symbol, r.deps.Clock.Now()),
		Candidate:          armedEvt.Candidate,
	}

	return r.publishEvent(ctx, TopicReversionWaitComplete, evt)
}
