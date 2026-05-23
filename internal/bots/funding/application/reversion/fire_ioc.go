package reversion

import (
	"context"
	"log/slog"
	"time"

	"crypto-bot/internal/bots/funding/application/cycle"
	"crypto-bot/internal/bots/funding/application/events"
	"crypto-bot/internal/bots/funding/application/orders"
	fundingdomain "crypto-bot/internal/bots/funding/domain"
	"crypto-bot/internal/infrastructure/exchange"
	applogger "crypto-bot/pkg/logger"

	"github.com/ThreeDotsLabs/watermill/message"
)

func subscribeFireIOC(ctx context.Context, rt *cycle.Runtime) {
	rt.Subscribe(ctx, events.TopicReversionConfirmed, func(_ *message.Message) {
		handleFireIOC(ctx, rt)
	})
}

func handleFireIOC(ctx context.Context, rt *cycle.Runtime) {
	settleTime := rt.SettleTime()
	if settleTime.IsZero() {
		applogger.WithCtx(ctx, rt.Log()).Error("Settle time not found, aborting IOC fire")
		return
	}

	cfg := rt.Config()
	reqID := rt.GetReqID()
	latencyMs := rt.Deps().Clock.LatencyMs()
	maxLatency := time.Duration(cfg.FundingReversion.MaxLatency)
	if maxLatency > 0 && time.Duration(latencyMs)*time.Millisecond > maxLatency {
		applogger.WithCtx(ctx, rt.Log()).Warn("Latency too high, aborting IOC fire",
			slog.Int64("latency_rtt", latencyMs),
			slog.Duration("max_latency", maxLatency),
		)
		rt.AbortCtx(ctx, reqID, "fire_ioc", "latency too high")
		return
	}

	oneWayMs := latencyMs / 2
	bufferTime := time.Duration(cfg.FundingReversion.BufferTime)
	fireOffset := time.Duration(oneWayMs)*time.Millisecond + bufferTime

	applogger.WithCtx(ctx, rt.Log()).Info("Firing configuration",
		slog.Int64("latency_rtt", latencyMs),
		slog.Int64("one_way", oneWayMs),
		slog.Duration("buffer", bufferTime),
		slog.Duration("total_offset", fireOffset),
	)

	snapshotOffset := 50 * time.Millisecond
	if fireOffset > snapshotOffset {
		snapshotOffset = fireOffset
	}
	if !rt.WaitUntil(ctx, settleTime.Add(-snapshotOffset)) {
		return
	}

	c := rt.CandidateCopy()
	if err := rt.RefreshPrice(ctx, &c); err != nil {
		applogger.WithCtx(ctx, rt.Log()).Warn("Refresh price failed, abort", slog.Any("error", err))
		rt.AbortCtx(ctx, reqID, "fire_ioc", "refresh price failed")
		return
	}
	c.Volume = c.CalculateVolume()
	safety := rt.Global().System.Safety
	c.SafetyResult = c.ApplySafetySizing(fundingdomain.SafetyLimits{
		MaxImpactRatio: safety.MaxImpactRatio,
		MinVol24USD:    safety.MinVol24USD,
	})
	if !c.SafetyResult.Passed {
		applogger.WithCtx(ctx, rt.Log()).Warn("Safety blocked IOC",
			slog.String("reason", c.SafetyResult.RejectReason),
			slog.Float64("desiredNotionalUSDT", c.SafetyResult.DesiredNotionalUSDT),
			slog.Float64("actualNotionalUSDT", c.SafetyResult.ActualNotionalUSDT),
			slog.Float64("maxSafeNotionalUSDT", c.SafetyResult.MaxSafeNotionalUSDT),
		)
		rt.RecordAndPublishCtx(ctx, reqID, events.TopicReversionIOCFired, events.IOCFiredEvent{
			Flow:          events.FlowReversion,
			Symbol:        c.Symbol,
			OrderID:       "",
			Side:          c.Side,
			CloseSide:     c.CloseSide,
			IntendedPrice: 0,
			FireTimestamp: rt.Deps().Clock.Now(),
			Volume:        0,
			SettleTime:    settleTime,
			Error:         c.SafetyResult.RejectReason,
		})
		rt.AbortCtx(ctx, reqID, "fire_ioc", c.SafetyResult.RejectReason)
		return
	}
	applogger.WithCtx(ctx, rt.Log()).Info("IOC sizing",
		slog.Float64("desiredNotionalUSDT", c.SafetyResult.DesiredNotionalUSDT),
		slog.Float64("actualNotionalUSDT", c.SafetyResult.ActualNotionalUSDT),
		slog.Float64("maxSafeNotionalUSDT", c.SafetyResult.MaxSafeNotionalUSDT),
		slog.Float64("avgMinuteVolumeUSDT", c.SafetyResult.AvgMinuteVolumeUSDT),
		slog.Float64("vol", c.Volume),
		slog.Bool("sizedDown", c.SafetyResult.SizedDown),
	)

	if !rt.WaitUntil(ctx, settleTime.Add(-fireOffset)) {
		return
	}
	fireTime := rt.Deps().Clock.Now()

	res := orders.FireIOC(ctx, rt.Deps().Client, &c, rt.Deps().Clock, rt.Log())
	rt.SetCandidate(c)
	rt.AppendResult(res)

	if res.IsSuccess() {
		rt.MarkReversionOrder(res.OrderID)
		rt.RecordAndPublishCtx(ctx, reqID, events.TopicReversionIOCFired, events.IOCFiredEvent{
			Flow:          events.FlowReversion,
			Symbol:        c.Symbol,
			OrderID:       res.OrderID,
			Side:          c.Side,
			CloseSide:     c.CloseSide,
			OrderType:     exchange.OrderTypeIOC,
			IntendedPrice: res.Price,
			Volume:        res.Volume,
			TPPrice:       res.TakeProfitPrice,
			SLPrice:       res.StopLossPrice,
			SettleTime:    settleTime,
			FireTimestamp: fireTime,
			LatencyRTTMs:  latencyMs,
		})
	} else {
		errText := "IOC order failed"
		if res.Error != nil {
			errText = res.Error.Error()
		}
		rt.RecordAndPublishCtx(ctx, reqID, events.TopicReversionIOCFired, events.IOCFiredEvent{
			Flow:          events.FlowReversion,
			Symbol:        c.Symbol,
			OrderID:       res.OrderID,
			Side:          c.Side,
			CloseSide:     c.CloseSide,
			IntendedPrice: res.Price,
			Volume:        res.Volume,
			SettleTime:    settleTime,
			FireTimestamp: fireTime,
			Error:         errText,
		})
		rt.AbortCtx(ctx, reqID, "fire_ioc", errText)
	}
}
