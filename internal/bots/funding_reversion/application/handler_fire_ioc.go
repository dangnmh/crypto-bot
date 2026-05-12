package application

import (
	"context"
	"time"

	"crypto-bot/internal/bots/funding_reversion/application/events"

	"github.com/ThreeDotsLabs/watermill/message"
)

// subscribeFireIOC handles cycle.confirmed → snapshot price → fire IOC with TP+SL.
func (o *CycleOrchestrator) subscribeFireIOC(ctx context.Context, settle time.Time) {
	o.consumeTopic(ctx, events.TopicConfirmed, func(_ *message.Message) {
		o.handleFireIOC(ctx, settle)
	})
}

func (o *CycleOrchestrator) handleFireIOC(ctx context.Context, settle time.Time) {
	c := &o.candidate

	latencyMs := o.deps.Clock.LatencyMs()
	oneWayMs := latencyMs / 2
	bufferTime := time.Duration(o.global.System.Safety.BufferTime)
	fireOffset := time.Duration(oneWayMs)*time.Millisecond + bufferTime

	o.deps.Log.Info("⏱️ Firing configuration",
		"latency_rtt", latencyMs, "one_way", oneWayMs,
		"buffer", bufferTime, "total_offset", fireOffset,
	)

	// Snapshot price before chaos begins
	snapshotOffset := 50 * time.Millisecond
	if fireOffset > snapshotOffset {
		snapshotOffset = fireOffset
	}
	o.waitUntil(ctx, settle.Add(-snapshotOffset))

	if err := o.refreshPrice(ctx, c); err != nil {
		o.deps.Log.Warn("🟡 Refresh price failed, abort", "error", err)
		o.abort("fire_ioc", "refresh price failed")
		return
	}

	// Refresh volume with latest price
	c.Volume = c.CalculateVolume()

	// Fetch OB for TP wall detection
	ob, _ := o.deps.DepthStore.GetDepth(ctx, o.cfg.Symbol)

	// Wait for precise fire moment
	o.waitUntil(ctx, settle.Add(-fireOffset))
	res := FireIOC(ctx, o.deps.Client, c, o.deps.Clock, o.deps.Log, ob)
	o.results = append(o.results, res)

	if res.IsSuccess() {
		o.publishOrLog(events.TopicIOCFired, events.IOCFiredEvent{
			Symbol:    c.Symbol,
			OrderID:   res.OrderID,
			Side:      int(c.Side),
			CloseSide: int(c.CloseSide),
			Price:     c.LastPrice,
			Volume:    c.Volume,
			TPPrice:   c.Config.FundingReversion.TakeProfitPct,
			SLPrice:   c.Config.FundingReversion.StopLossPct,
			Settle:    settle,
			Timestamp: time.Now(),
		})
	} else {
		o.abort("fire_ioc", "IOC order failed")
	}
}
