package reversion

import (
	"context"
	"time"

	"crypto-bot/internal/bots/funding/application/cycle"
	"crypto-bot/internal/bots/funding/application/events"

	"github.com/ThreeDotsLabs/watermill/message"
)

func subscribeWait(ctx context.Context, rt *cycle.Runtime) {
	rt.Subscribe(ctx, events.TopicReversionArmed, func(msg *message.Message) {
		// Get settle time from the cycle start event
		envelopes := rt.JourneyEvents()
		var settleTime time.Time
		for i := range envelopes {
			if envelopes[i].Topic == events.TopicCycleStarted {
				startEvent, err := cycle.Unmarshal[events.CycleStartEvent](envelopes[i].Payload)
				if err == nil {
					settleTime = startEvent.SettleTime
					break
				}
			}
		}
		if settleTime.IsZero() {
			return
		}
		if !rt.WaitUntil(ctx, settleTime.Add(-2*time.Second)) {
			return
		}
		reqID := rt.GetReqID()
		rt.RecordAndPublishCtx(ctx, reqID, events.TopicReversionWaitComplete, events.WaitCompleteEvent{
			Flow:       events.FlowReversion,
			Symbol:     rt.Config().Symbol,
			SettleTime: settleTime,
		})
	})
}
