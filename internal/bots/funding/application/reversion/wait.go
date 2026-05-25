package reversion

import (
	"context"
	"time"
)

func (s *Strategy) handleWait(ctx context.Context, armedEvt ArmedEvent) error {
	s.mu.Lock()
	settleTime := s.settleTime
	s.mu.Unlock()

	if settleTime.IsZero() {
		// If settleTime is zero, skip wait
		evt := WaitCompleteEvent{
			Flow:      FlowReversion,
			Symbol:    armedEvt.Symbol,
			Timestamp: s.deps.Clock.Now(),
		}
		return s.publishEvent(ctx, TopicReversionWaitComplete, evt)
	}

	target := settleTime.Add(-2 * time.Second)
	if !s.WaitUntil(ctx, target) {
		s.abort(ctx, "wait period context canceled")
		return context.Canceled
	}

	evt := WaitCompleteEvent{
		Flow:       FlowReversion,
		Symbol:     armedEvt.Symbol,
		SettleTime: settleTime,
		Timestamp:  s.deps.Clock.Now(),
	}

	return s.publishEvent(ctx, TopicReversionWaitComplete, evt)
}
