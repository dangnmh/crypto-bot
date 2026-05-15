package application

import (
	"context"
	"log/slog"
	"time"

	"crypto-bot/internal/bots/funding/application/events"
	"crypto-bot/internal/bots/funding/domain"

	"github.com/ThreeDotsLabs/watermill/message"
)

// subscribeFireIOC handles cycle.confirmed → snapshot price → fire IOC with TP+SL.
func (o *CycleOrchestrator) subscribeFireIOC(ctx context.Context, settle time.Time) {
	o.consumeTopic(ctx, events.TopicConfirmed, func(_ *message.Message) {
		o.handleFireIOC(ctx, settle)
	})
}

func (o *CycleOrchestrator) handleFireIOC(ctx context.Context, settle time.Time) {
	latencyMs := o.deps.Clock.LatencyMs()
	oneWayMs := latencyMs / 2
	bufferTime := time.Duration(o.global.System.Safety.BufferTime)
	fireOffset := time.Duration(oneWayMs)*time.Millisecond + bufferTime

	o.deps.Log.Info("⏱️ Firing configuration",
		slog.Int64("latency_rtt", latencyMs),
		slog.Int64("one_way", oneWayMs),
		slog.Duration("buffer", bufferTime),
		slog.Duration("total_offset", fireOffset),
	)

	// Snapshot price before chaos begins
	snapshotOffset := 50 * time.Millisecond
	if fireOffset > snapshotOffset {
		snapshotOffset = fireOffset
	}
	o.waitUntil(ctx, settle.Add(-snapshotOffset))

	var c domain.Candidate
	var refreshErr error
	o.withLock(func() {
		if err := o.refreshPrice(ctx, &o.candidate); err != nil {
			refreshErr = err
			return
		}

		// Refresh volume with latest price
		o.candidate.Volume = o.candidate.CalculateVolume()
		c = o.candidate
	})

	if refreshErr != nil {
		o.deps.Log.Warn("🟡 Refresh price failed, abort", slog.Any("error", refreshErr))
		o.abort("fire_ioc", "refresh price failed")
		return
	}

	// Fetch OB for TP wall detection
	ob, _ := o.deps.DepthStore.GetDepth(ctx, o.cfg.Symbol)

	// Capture pre-fire data for cycle record.
	pd, err := o.deps.PriceStore.GetPrice(ctx, o.cfg.Symbol, 5*time.Second)
	if err == nil {
		spread := calcSpreadPct(pd.BestBid, pd.BestAsk)
		o.recorder.AddSnapshot(domain.MarketSnapshot{
			Phase:     domain.PhaseFire,
			LastPrice: pd.LastPrice,
			BestBid:   pd.BestBid,
			BestAsk:   pd.BestAsk,
			Spread:    spread,
		})
		o.recorder.SetLatencyRTTMs(latencyMs)
	} else {
		o.recorder.SetLatencyRTTMs(latencyMs)
	}

	// Wait for precise fire moment
	o.waitUntil(ctx, settle.Add(-fireOffset))

	fireTime := time.Now()
	res := FireIOC(ctx, o.deps.Client, &c, o.deps.Clock, o.deps.Log, ob)

	o.withLock(func() {
		o.candidate = c
		o.results = append(o.results, res)
	})

	// Capture IOC execution data thread-safely.
	o.recorder.Mutate(func(b *domain.CycleRecordBuilder) {
		b.FireTimestamp = fireTime
		b.IOCIntended = res.Price
		b.TPPriceSubmitted = res.TakeProfitPrice
		b.SLPriceSubmitted = res.StopLossPrice
		if res.IsSuccess() {
			b.IOCOrderID = res.OrderID
		} else if res.Error != nil {
			b.IOCError = res.Error.Error()
		}
	})

	if res.IsSuccess() {
		o.publishOrLog(events.TopicIOCFired, events.IOCFiredEvent{
			Symbol:    c.Symbol,
			OrderID:   res.OrderID,
			Side:      c.Side,
			CloseSide: c.CloseSide,
			Price:     res.Price,
			Volume:    res.Volume,
			TPPrice:   res.TakeProfitPrice,
			SLPrice:   res.StopLossPrice,
			Settle:    settle,
			Timestamp: fireTime,
		})
	} else {
		o.abort("fire_ioc", "IOC order failed")
	}
}
