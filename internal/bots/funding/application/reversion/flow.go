package reversion

import (
	"context"
	"log/slog"
	"math"
	"time"

	shared "crypto-bot/internal/domain"

	"crypto-bot/internal/bots/funding/application/cycle"
	"crypto-bot/internal/bots/funding/application/events"
	"crypto-bot/internal/bots/funding/application/orders"
	"crypto-bot/internal/bots/funding/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"

	"github.com/ThreeDotsLabs/watermill/message"
)

func Register(ctx context.Context, rt *cycle.Runtime, settle time.Time) {
	subscribeArm(ctx, rt)
	subscribeWait(ctx, rt, settle)
	subscribeRecheck(ctx, rt)
	subscribeFireIOC(ctx, rt, settle)
	subscribeFillWatcher(ctx, rt)
	subscribeTrailing(ctx, rt)
	subscribeTimeoutGuard(ctx, rt)
}

func subscribeArm(ctx context.Context, rt *cycle.Runtime) {
	rt.Subscribe(ctx, events.TopicReversionCandidate, func(_ *message.Message) {
		handleArm(ctx, rt)
	})
}

func handleArm(ctx context.Context, rt *cycle.Runtime) {
	cfg := rt.Config()
	c := rt.CandidateCopy()

	if c.Config.FundingReversion.DynamicPricing.Enabled {
		rt.InitKlines(ctx)
	}

	if err := rt.SubscribeAll(ctx); err != nil {
		rt.Log().Error("🔴 Failed to subscribe WS channels", slog.Any("error", err))
		rt.Abort("arm", "WS subscribe failed")
		return
	}

	rt.Sleep(ctx, 2*time.Second)
	if err := rt.RefreshPrice(ctx, &c); err != nil {
		rt.Log().Warn("🟡 Refresh price failed", slog.Any("error", err))
		rt.UnsubscribeAll(ctx)
		rt.Abort("arm", "refresh price failed")
		return
	}

	if c.Config.FundingReversion.DynamicPricing.Enabled {
		klines := rt.Deps().KlineStore.GetKlines(ctx, cfg.Symbol)
		c.ATR = domain.CalculateATR(klines, 14)
		c.PrepareDynamicPricing()
		rt.Log().Info("📈 Dynamic Pricing",
			slog.Float64("ATR", c.ATR),
			slog.Float64("TP", c.Config.FundingReversion.TakeProfitPct),
			slog.Float64("SL", c.Config.FundingReversion.StopLossPct),
		)
	}

	ioc, err := c.CalculateIOCPrice(nil)
	if err != nil {
		rt.Log().Warn("🟡 IOC calc failed", slog.Any("error", err))
		rt.UnsubscribeAll(ctx)
		rt.Abort("arm", "IOC calc failed")
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

	c.SafetyResult = c.EvaluateSafety(rt.Global().System.Safety.MaxImpactRatio)
	if !c.SafetyResult.Passed {
		rt.MutateRecorder(func(b *domain.CycleRecordBuilder) {
			b.SafetyPassed = false
			b.SafetyRejectReason = c.SafetyResult.RejectReason
		})
		rt.Log().Warn("🔴 Safety FAIL", slog.String("reason", c.SafetyResult.RejectReason))
		rt.UnsubscribeAll(ctx)
		rt.Abort("arm", c.SafetyResult.RejectReason)
		return
	}
	rt.MutateRecorder(func(b *domain.CycleRecordBuilder) {
		b.SafetyPassed = true
		b.TrapEnabled = cfg.IsHedgeTrapEnabled()
	})

	if filter := c.Config.FundingReversion.ImbalanceFilter; filter.Enabled {
		ob, err := rt.Deps().DepthStore.GetDepth(ctx, cfg.Symbol)
		var result domain.ImbalanceFilterResult
		if err == nil {
			result = c.EvaluateImbalanceFilter(ob)
		} else {
			result = domain.ImbalanceFilterResult{
				Enabled:      true,
				Passed:       false,
				NearPct:      filter.NearPct,
				RejectReason: "imbalance ratio unavailable",
			}
		}
		rt.MutateRecorder(func(b *domain.CycleRecordBuilder) {
			b.ImbalanceFilterEnabled = result.Enabled
			b.ImbalanceFilterPassed = result.Passed
			b.ImbalanceRatio = result.Ratio
			b.ImbalanceNearPct = result.NearPct
		})
		if !result.Passed {
			rt.MutateRecorder(func(b *domain.CycleRecordBuilder) {
				b.SafetyPassed = false
				b.SafetyRejectReason = result.RejectReason
			})
			rt.Log().Warn("🔴 Imbalance filter FAIL", slog.String("reason", result.RejectReason))
			rt.UnsubscribeAll(ctx)
			rt.Abort("arm", result.RejectReason)
			return
		}
		rt.Log().Info("📊 Imbalance filter OK",
			slog.Float64("ratio", result.Ratio),
			slog.Float64("near_pct", result.NearPct*100),
		)
	}

	if c.Config.FundingReversion.DynamicPricing.Enabled {
		rt.MutateRecorder(func(b *domain.CycleRecordBuilder) {
			b.DynamicEnabled = true
			b.DynamicTPPct = c.Config.FundingReversion.TakeProfitPct
			b.DynamicSLPct = c.Config.FundingReversion.StopLossPct
			b.ATRValue = c.ATR
		})
	}

	pd, err := rt.Deps().PriceStore.GetPrice(ctx, cfg.Symbol, 5*time.Second)
	if err == nil {
		spread := cycle.CalcSpreadPct(pd.BestBid, pd.BestAsk)
		rt.Recorder().AddSnapshot(domain.MarketSnapshot{
			Topic:     events.TopicReversionArmed,
			LastPrice: pd.LastPrice,
			BestBid:   pd.BestBid,
			BestAsk:   pd.BestAsk,
			Spread:    spread,
		})
	}

	rt.SetCandidate(c)
	rt.Log().Info("🎯 Ready",
		slog.String("side", c.Side.String()),
		slog.Float64("fr", c.FundingRate*100),
		slog.Float64("ioc", ioc),
		slog.Float64("vol", c.Volume),
	)

	rt.Publish(events.TopicReversionArmed, events.ArmedEvent{
		Flow:   events.FlowReversion,
		Symbol: c.Symbol,
	})
}

func subscribeWait(ctx context.Context, rt *cycle.Runtime, settle time.Time) {
	rt.Subscribe(ctx, events.TopicReversionArmed, func(_ *message.Message) {
		rt.WaitUntil(ctx, settle.Add(-2*time.Second))
		rt.Publish(events.TopicReversionWaitComplete, events.WaitCompleteEvent{
			Flow:   events.FlowReversion,
			Symbol: rt.Config().Symbol,
			Settle: settle,
		})
	})
}

func subscribeRecheck(ctx context.Context, rt *cycle.Runtime) {
	rt.Subscribe(ctx, events.TopicReversionWaitComplete, func(_ *message.Message) {
		handleRecheck(ctx, rt)
	})
}

func handleRecheck(ctx context.Context, rt *cycle.Runtime) {
	c := rt.CandidateCopy()
	cfg := rt.Config()
	td, err := rt.Deps().TickerStore.GetTicker(ctx, c.Symbol)
	if err != nil {
		rt.Log().Warn("🟡 No ticker for recheck")
		rt.Abort("recheck", "no ticker")
		return
	}

	if (td.FundingRate > 0) != (c.FundingRate > 0) {
		rt.Log().Error("🔴 FR sign flip!",
			slog.Float64("old", c.FundingRate*100),
			slog.Float64("new", td.FundingRate*100),
		)
		rt.Abort("recheck", "FR sign flip")
		return
	}

	if math.Abs(td.FundingRate) < cfg.MinFundingRate {
		rt.Log().Warn("🟡 FR dropped below threshold",
			slog.Float64("fr", td.FundingRate*100),
			slog.Float64("min", cfg.MinFundingRate*100),
		)
		rt.Abort("recheck", "FR below threshold")
		return
	}

	rt.Log().Info("🟢 FR OK", slog.Float64("fr", td.FundingRate*100))
	rt.MutateRecorder(func(b *domain.CycleRecordBuilder) {
		b.FRAtRecheck = td.FundingRate
	})

	rt.Publish(events.TopicReversionConfirmed, events.ConfirmedEvent{
		Flow:        events.FlowReversion,
		Symbol:      c.Symbol,
		FundingRate: td.FundingRate,
		Side:        c.Side,
		CloseSide:   c.CloseSide,
	})
}

func subscribeFireIOC(ctx context.Context, rt *cycle.Runtime, settle time.Time) {
	rt.Subscribe(ctx, events.TopicReversionConfirmed, func(_ *message.Message) {
		handleFireIOC(ctx, rt, settle)
	})
}

func handleFireIOC(ctx context.Context, rt *cycle.Runtime, settle time.Time) {
	cfg := rt.Config()
	latencyMs := rt.Deps().Clock.LatencyMs()
	maxLatency := time.Duration(cfg.FundingReversion.MaxLatency)
	if maxLatency > 0 && time.Duration(latencyMs)*time.Millisecond > maxLatency {
		rt.Log().Warn("🔴 Latency too high, aborting IOC fire",
			slog.Int64("latency_rtt", latencyMs),
			slog.Duration("max_latency", maxLatency),
		)
		rt.Abort("fire_ioc", "latency too high")
		return
	}

	oneWayMs := latencyMs / 2
	bufferTime := time.Duration(cfg.FundingReversion.BufferTime)
	fireOffset := time.Duration(oneWayMs)*time.Millisecond + bufferTime

	rt.Log().Info("⏱️ Firing configuration",
		slog.Int64("latency_rtt", latencyMs),
		slog.Int64("one_way", oneWayMs),
		slog.Duration("buffer", bufferTime),
		slog.Duration("total_offset", fireOffset),
	)

	snapshotOffset := 50 * time.Millisecond
	if fireOffset > snapshotOffset {
		snapshotOffset = fireOffset
	}
	rt.WaitUntil(ctx, settle.Add(-snapshotOffset))

	c := rt.CandidateCopy()
	if err := rt.RefreshPrice(ctx, &c); err != nil {
		rt.Log().Warn("🟡 Refresh price failed, abort", slog.Any("error", err))
		rt.Abort("fire_ioc", "refresh price failed")
		return
	}
	c.Volume = c.CalculateVolume()

	ob, _ := rt.Deps().DepthStore.GetDepth(ctx, cfg.Symbol)

	pd, err := rt.Deps().PriceStore.GetPrice(ctx, cfg.Symbol, 5*time.Second)
	if err == nil {
		spread := cycle.CalcSpreadPct(pd.BestBid, pd.BestAsk)
		rt.Recorder().AddSnapshot(domain.MarketSnapshot{
			Topic:     events.TopicReversionConfirmed,
			LastPrice: pd.LastPrice,
			BestBid:   pd.BestBid,
			BestAsk:   pd.BestAsk,
			Spread:    spread,
		})
	}
	rt.Recorder().SetLatencyRTTMs(latencyMs)

	rt.WaitUntil(ctx, settle.Add(-fireOffset))
	fireTime := time.Now()
	if err := rt.CycleRiskAllowsReversion(c); err != nil {
		rt.Log().Warn("🔴 Cycle risk blocked IOC", slog.Any("error", err))
		rt.MutateRecorder(func(b *domain.CycleRecordBuilder) {
			b.SafetyPassed = false
			b.SafetyRejectReason = err.Error()
		})
		rt.Abort("fire_ioc", err.Error())
		return
	}

	res := orders.FireIOC(ctx, rt.Deps().Client, &c, rt.Deps().Clock, rt.Log(), ob)
	rt.SetCandidate(c)
	rt.AppendResult(res)

	rt.MutateRecorder(func(b *domain.CycleRecordBuilder) {
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
		rt.Publish(events.TopicReversionIOCFired, events.IOCFiredEvent{
			Flow:      events.FlowReversion,
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
		rt.Abort("fire_ioc", "IOC order failed")
	}
}

func subscribeFillWatcher(ctx context.Context, rt *cycle.Runtime) {
	rt.Subscribe(ctx, events.TopicReversionIOCFired, func(msg *message.Message) {
		evt, err := cycle.Unmarshal[events.IOCFiredEvent](msg.Payload)
		if err != nil {
			rt.Log().Error("🔴 Unmarshal IOCFiredEvent failed", slog.Any("error", err))
			return
		}
		setupFillWatcher(ctx, rt, evt.OrderID, evt.Side, evt.CloseSide)
	})
}

func setupFillWatcher(ctx context.Context, rt *cycle.Runtime, orderID string, side, closeSide shared.Side) {
	if orderID == "" {
		return
	}

	rt.Deps().OrderNotifier.OnOrderUpdate(ctx, orderID, 5*time.Second, func(deal exchange.WsOrderDeal) {
		if !exchange.IsTerminalOrderState(deal.State) {
			return
		}

		rt.Deps().OrderNotifier.RemoveOrderCallback(deal.GetOrderID())

		if deal.DealVol <= 0 {
			rt.Log().Warn("🟡 No fill", slog.String("flow", events.FlowReversion), slog.String("orderID", deal.GetOrderID()))
			return
		}

		rt.Log().Info("📊 Position opened",
			slog.String("flow", events.FlowReversion),
			slog.Float64("entry", deal.DealAvgPrice),
			slog.Float64("vol", deal.DealVol),
		)

		rt.MutateRecorder(func(b *domain.CycleRecordBuilder) {
			b.IOCFilled = true
			b.IOCFillPrice = deal.DealAvgPrice
			b.IOCFillVol = deal.DealVol
			b.IOCExcursion = domain.NewExcursionTracker(side, deal.DealAvgPrice)
			b.Excursion = b.IOCExcursion
		})
		rt.StartExcursionPriceStream(ctx)

		rt.Publish(events.TopicReversionOrderFilled, events.OrderFilledEvent{
			Flow:         events.FlowReversion,
			Symbol:       rt.Config().Symbol,
			OrderID:      deal.GetOrderID(),
			DealAvgPrice: deal.DealAvgPrice,
			DealVol:      deal.DealVol,
			Side:         side,
			CloseSide:    closeSide,
		})
	})
}

func subscribeTrailing(ctx context.Context, rt *cycle.Runtime) {
	rt.Subscribe(ctx, events.TopicReversionOrderFilled, func(msg *message.Message) {
		evt, err := cycle.Unmarshal[events.OrderFilledEvent](msg.Payload)
		if err != nil {
			rt.Log().Error("🔴 Unmarshal OrderFilledEvent failed", slog.Any("error", err))
			return
		}
		handleTrailing(ctx, rt, evt)
	})
}

func handleTrailing(ctx context.Context, rt *cycle.Runtime, evt events.OrderFilledEvent) {
	c := rt.CandidateCopy()
	trailCfg := c.Config.FundingReversion.Trailing
	if !trailCfg.Enabled {
		rt.Log().Info("⏭️ Trailing disabled, position requires manual close", slog.String("flow", evt.Flow))
		return
	}

	closeSide := evt.CloseSide
	var activePrice float64
	if trailCfg.ActivationPct > 0 {
		if closeSide == shared.SideCloseLong {
			activePrice = decmath.Mul(evt.DealAvgPrice, decmath.Add(1, trailCfg.ActivationPct))
		} else {
			activePrice = decmath.Mul(evt.DealAvgPrice, decmath.Sub(1, trailCfg.ActivationPct))
		}
	}

	req := exchange.SubmitTrackOrderRequest{
		Symbol:       evt.Symbol,
		Leverage:     c.Config.Leverage,
		Side:         int(closeSide),
		Vol:          evt.DealVol,
		OpenType:     c.Config.ParsedOpenType,
		PositionMode: c.Config.ParsedPositionMode,
		Trend:        1,
		ActivePrice:  activePrice,
		BackType:     1,
		BackValue:    trailCfg.CallbackPct,
		ReduceOnly:   true,
	}

	rt.Log().Info("🏃 Placing TrackOrder (Trailing)",
		slog.String("flow", evt.Flow),
		slog.Int("side", req.Side),
		slog.Float64("vol", req.Vol),
		slog.Float64("activePrice", activePrice),
		slog.Float64("callbackPct", decmath.Mul(req.BackValue, 100)),
	)

	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	trackID, err := rt.Deps().Client.CreateTrackOrder(reqCtx, req)
	if err != nil {
		rt.Log().Error("🔴 TrackOrder failed - fallback close", slog.Any("error", err), slog.String("flow", evt.Flow))
		fallbackCloseAfterTrailingFailure(ctx, rt, evt)
		return
	}

	rt.Log().Info("✅ TrackOrder placed successfully", slog.String("trackID", trackID), slog.String("flow", evt.Flow))
	rt.MutateRecorder(func(b *domain.CycleRecordBuilder) {
		b.TrailingActivated = true
		b.TrailingActivePrice = activePrice
		b.TrailingCallbackPct = trailCfg.CallbackPct
	})

	rt.Publish(events.TopicReversionTrailingPlaced, events.TrailingPlacedEvent{
		Flow:        events.FlowReversion,
		Symbol:      evt.Symbol,
		TrackID:     trackID,
		ActivePrice: activePrice,
		CallbackPct: trailCfg.CallbackPct,
	})
}

func fallbackCloseAfterTrailingFailure(ctx context.Context, rt *cycle.Runtime, evt events.OrderFilledEvent) {
	positionMode := rt.CandidateCopy().Config.ParsedPositionMode
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	if err := rt.Deps().Client.ClosePosition(closeCtx, evt.Symbol, evt.CloseSide, evt.DealVol, positionMode); err != nil {
		rt.Log().Error("🔴 Exact-leg close failed - fallback close all",
			slog.Any("error", err),
			slog.String("symbol", evt.Symbol),
			slog.String("flow", events.FlowReversion),
			slog.Any("closeSide", evt.CloseSide),
			slog.Float64("vol", evt.DealVol),
		)
		if allErr := rt.Deps().Client.CloseAllPositions(closeCtx, evt.Symbol); allErr != nil {
			reason := "critical_close_failed: " + allErr.Error()
			rt.MutateRecorder(func(b *domain.CycleRecordBuilder) {
				b.AbortReason = reason
				b.AbortFlow = events.FlowReversion
				b.AbortTopic = events.TopicReversionAbort
				b.ErrorFlow = events.FlowReversion
				b.ErrorTopic = events.TopicReversionError
			})
			rt.Log().Error("🔴 CRITICAL close failed after exact-leg close failure",
				slog.Any("error", allErr),
				slog.String("symbol", evt.Symbol),
				slog.String("flow", events.FlowReversion),
			)
			rt.Publish(events.TopicReversionError, events.CycleErrorEvent{
				Flow:   events.FlowReversion,
				Symbol: evt.Symbol,
				Error:  reason,
			})
			rt.Publish(events.TopicReversionAbort, events.CycleAbortEvent{
				Flow:   events.FlowReversion,
				Symbol: evt.Symbol,
				Reason: reason,
			})
			return
		}
	}

	rt.Publish(events.TopicReversionPositionClosed, events.PositionClosedEvent{
		Flow:   events.FlowReversion,
		Symbol: evt.Symbol,
		Reason: "trailing_failed_fallback",
	})
}

func subscribeTimeoutGuard(ctx context.Context, rt *cycle.Runtime) {
	rt.Subscribe(ctx, events.TopicReversionIOCFired, func(msg *message.Message) {
		evt, err := cycle.Unmarshal[events.IOCFiredEvent](msg.Payload)
		if err != nil {
			rt.Log().Error("🔴 Unmarshal IOCFiredEvent failed", slog.Any("error", err))
			return
		}
		go handleTimeout(ctx, rt, evt.Symbol)
	})
}

func handleTimeout(ctx context.Context, rt *cycle.Runtime, symbol string) {
	timeout := time.Duration(rt.Config().FundingReversion.PostSettleTimeout)
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	startedAt := time.Now()
	rt.MutateRecorder(func(b *domain.CycleRecordBuilder) {
		if b.Timeout.Triggered {
			return
		}
		b.Timeout.Flow = events.FlowReversion
		b.Timeout.Duration = timeout
		b.Timeout.DurationMs = timeout.Milliseconds()
		b.Timeout.StartedAt = startedAt
	})

	rt.Log().Info("⏱️ Timeout guard started", slog.Duration("timeout", timeout))

	if err := rt.Deps().Clock.Sleep(ctx, timeout); err != nil {
		return
	}
	rt.Log().Warn("🔴 TIMEOUT — force closing all positions", slog.Duration("timeout", timeout))
	firedAt := time.Now()
	rt.MutateRecorder(func(b *domain.CycleRecordBuilder) {
		b.Timeout.Flow = events.FlowReversion
		b.Timeout.Duration = timeout
		b.Timeout.DurationMs = timeout.Milliseconds()
		b.Timeout.StartedAt = startedAt
		b.Timeout.Triggered = true
		b.Timeout.FiredAt = firedAt
		b.Timeout.ForceCloseAttempted = true
	})

	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	if err := rt.Deps().Client.CloseAllPositions(closeCtx, symbol); err != nil {
		reason := "critical_timeout_close_failed: " + err.Error()
		rt.MutateRecorder(func(b *domain.CycleRecordBuilder) {
			b.AbortReason = reason
			b.AbortFlow = events.FlowReversion
			b.AbortTopic = events.TopicReversionAbort
			b.ErrorFlow = events.FlowReversion
			b.ErrorTopic = events.TopicReversionError
			b.Timeout.ForceCloseSucceeded = false
			b.Timeout.Error = err.Error()
		})
		rt.Log().Error("🔴 CRITICAL force close failed after timeout", slog.Any("error", err))
		rt.Publish(events.TopicReversionError, events.CycleErrorEvent{
			Flow:   events.FlowReversion,
			Symbol: symbol,
			Error:  reason,
		})
		rt.Publish(events.TopicReversionAbort, events.CycleAbortEvent{
			Flow:   events.FlowReversion,
			Symbol: symbol,
			Reason: reason,
		})
		return
	}
	rt.MutateRecorder(func(b *domain.CycleRecordBuilder) {
		b.Timeout.ForceCloseSucceeded = true
	})

	rt.Publish(events.TopicReversionTimeout, events.CycleTimeoutEvent{
		Flow:    events.FlowReversion,
		Symbol:  symbol,
		Timeout: timeout,
	})
}
