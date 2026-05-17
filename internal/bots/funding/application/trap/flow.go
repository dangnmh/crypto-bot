package trap

import (
	"context"
	"fmt"
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

const trapSourceOBMonitor = "ob_monitor"
const trapSourceStaticLimit = "static_limit"

type wallVerification struct {
	price       float64
	trapPrice   float64
	ageMs       int64
	distancePct float64
}

func Register(ctx context.Context, rt *cycle.Runtime, settle time.Time) {
	subscribeFireTrap(ctx, rt, settle)
	subscribeFillWatcher(ctx, rt)
	subscribeTrailing(ctx, rt)
	subscribeTrapOrderTimeoutGuard(ctx, rt)
	watchTrapBranchTerminal(ctx, rt)
}

func subscribeFireTrap(ctx context.Context, rt *cycle.Runtime, settle time.Time) {
	rt.Subscribe(ctx, events.TopicTrapCandidate, func(_ *message.Message) {
		cfg := rt.Config()
		if !cfg.IsHedgeTrapEnabled() {
			return
		}
		go handleFireTrap(ctx, rt, settle)
	})
}

func handleFireTrap(ctx context.Context, rt *cycle.Runtime, settle time.Time) {
	cfg := rt.Config()
	delay := time.Duration(cfg.FundingTrap.TrapAfterSettle)
	if delay <= 0 {
		delay = 3 * time.Second
	}
	trapTime := settle.Add(delay)

	rt.Log().Info("⏱️ Fire Trap waiting", slog.Duration("delay", delay), slog.Time("target_time", trapTime))
	rt.WaitUntil(ctx, trapTime)
	if ctx.Err() != nil {
		return
	}

	ob, err := rt.Deps().DepthStore.GetDepth(ctx, cfg.Symbol)
	if err != nil || ob == nil {
		rt.Log().Warn("🟡 Fire Trap: failed to fetch depth, falling back to static trap", slog.Any("error", err))
		fireStaticTrap(ctx, rt)
		return
	}

	originalCandidate := rt.CandidateCopy()
	trapSide := originalCandidate.Side.Opposite()
	wallPrice := originalCandidate.FindTrapWallPrice(ob)
	trapCandidate := originalCandidate
	trapCandidate.Side = trapSide
	trapCandidate.CloseSide = shared.CloseSideFor(trapSide)

	if wallPrice <= 0 {
		rt.Log().Info("🤷 Fire Trap: no suitable wall found, falling back to static trap", slog.String("side", trapSide.String()))
		fireStaticTrap(ctx, rt)
		return
	}

	wallFoundAt := time.Now()
	verifiedWall, ok := verifyTrapWall(ctx, rt, originalCandidate, wallPrice, wallFoundAt)
	if !ok {
		rt.Log().Warn("🟡 Fire Trap: wall disappeared before placement, skipping OB trap",
			slog.String("side", trapSide.String()),
			slog.Float64("initial_wall_price", wallPrice),
		)
		rt.MutateRecorder(func(b *domain.CycleRecordBuilder) {
			b.TrapWallPrice = wallPrice
			b.TrapWallOK = false
			b.TrapWallAgeMs = time.Since(wallFoundAt).Milliseconds()
			b.TrapWallDist = originalCandidate.TrapWallDistancePct(wallPrice)
		})
		skipTrap(rt, domain.TrapSkipReasonWallNotVerified, trapSourceOBMonitor)
		return
	}

	rt.Publish(events.TopicTrapOBWallFound, events.OBWallFoundEvent{
		Flow:            events.FlowTrap,
		Symbol:          cfg.Symbol,
		WallPrice:       verifiedWall.price,
		WallVol:         0,
		WallVerified:    true,
		WallAgeMs:       verifiedWall.ageMs,
		WallDistancePct: verifiedWall.distancePct,
		Side:            trapSide,
	})

	rt.Log().Info("🧱 OB Monitor: wall found",
		slog.String("side", trapSide.String()),
		slog.Float64("wallPrice", verifiedWall.price),
		slog.Float64("trapPrice", verifiedWall.trapPrice),
		slog.Int64("wallAgeMs", verifiedWall.ageMs),
		slog.Float64("wallDistancePct", verifiedWall.distancePct),
	)

	fireOBTrap(ctx, rt, trapCandidate, verifiedWall)
}

func verifyTrapWall(
	ctx context.Context,
	rt *cycle.Runtime,
	c domain.Candidate,
	initialWallPrice float64,
	wallFoundAt time.Time,
) (wallVerification, bool) {
	freshOB, err := rt.Deps().DepthStore.GetDepth(ctx, c.Symbol)
	if err != nil || freshOB == nil {
		rt.Log().Warn("🟡 Fire Trap: failed to verify wall", slog.Any("error", err))
		return wallVerification{}, false
	}

	freshWallPrice := c.FindTrapWallPrice(freshOB)
	if freshWallPrice <= 0 {
		return wallVerification{}, false
	}

	if c.PriceUnit > 0 && math.Abs(freshWallPrice-initialWallPrice) > c.PriceUnit {
		rt.Log().Info("🧱 Fire Trap: wall moved during verification",
			slog.Float64("initial_wall_price", initialWallPrice),
			slog.Float64("fresh_wall_price", freshWallPrice),
		)
	}

	trapPrice := c.CalculateOBTrapPrice(freshWallPrice)
	if trapPrice <= 0 {
		return wallVerification{}, false
	}

	return wallVerification{
		price:       freshWallPrice,
		trapPrice:   trapPrice,
		ageMs:       time.Since(wallFoundAt).Milliseconds(),
		distancePct: c.TrapWallDistancePct(freshWallPrice),
	}, true
}

func fireOBTrap(ctx context.Context, rt *cycle.Runtime, c domain.Candidate, wall wallVerification) {
	trapPrice := wall.trapPrice
	c.Volume = c.CalculateTrapVolume(trapPrice)
	if c.Volume <= 0 {
		rt.Log().Warn("🟡 TRAP volume invalid, skipping", slog.String("symbol", c.Symbol))
		skipTrap(rt, domain.TrapSkipReasonInvalidVolume, trapSourceOBMonitor)
		return
	}
	if err := rt.CycleRiskAllowsTrap(c, c.NotionalForVolume(c.Volume, trapPrice)); err != nil {
		rt.Log().Warn("🟡 Cycle risk blocked Trap", slog.Any("error", err))
		skipTrap(rt, domain.TrapSkipReasonCycleRiskBlocked, trapSourceOBMonitor)
		return
	}

	tpPrice := c.CalculateTrapTPPrice(trapPrice)
	slPrice := c.CalculateTrapSLPrice(trapPrice)

	extOID := fmt.Sprintf("trp_ob_%s_%d", c.Symbol, time.Now().UnixMilli())
	req := exchange.SubmitOrderRequest{
		Symbol:          c.Symbol,
		Price:           trapPrice,
		Vol:             c.Volume,
		Side:            int(c.Side),
		Type:            exchange.OrderTypePostOnly,
		OpenType:        c.Config.ParsedOpenType,
		PositionMode:    c.Config.ParsedPositionMode,
		Leverage:        c.Config.Leverage,
		ExternalOID:     extOID,
		TakeProfitPrice: tpPrice,
		StopLossPrice:   slPrice,
	}

	rt.Log().Info("🩤 FIRE OB TRAP",
		slog.String("symbol", c.Symbol),
		slog.Float64("trapPrice", trapPrice),
		slog.Float64("vol", c.Volume),
		slog.String("side", c.Side.String()),
		slog.Float64("takeProfitPrice", tpPrice),
		slog.Float64("stopLossPrice", slPrice),
	)

	orderID, err := rt.Deps().Client.CreateOrder(ctx, req)
	if err != nil {
		rt.Log().Error("🔴 TRAP order failed", slog.Any("error", err))
		skipTrap(rt, domain.TrapSkipReasonOrderFailed, trapSourceOBMonitor)
		rt.MutateRecorder(func(b *domain.CycleRecordBuilder) {
			b.TrapError = err.Error()
		})
		return
	}

	rt.Log().Info("📨 TRAP submitted", slog.String("orderID", orderID))
	rt.MutateRecorder(func(b *domain.CycleRecordBuilder) {
		b.TrapSource = trapSourceOBMonitor
		b.TrapOutcome = domain.TrapOutcomePlaced
		b.TrapWallPrice = wall.price
		b.TrapWallOK = true
		b.TrapWallAgeMs = wall.ageMs
		b.TrapWallDist = wall.distancePct
		b.TrapPrice = trapPrice
		b.TrapOrderID = orderID
		b.TrapTPPct = c.Config.FundingTrap.TakeProfitPct
		b.TrapSLPct = c.Config.FundingTrap.StopLossPct
		b.TrapTPPrice = tpPrice
		b.TrapSLPrice = slPrice
	})

	rt.Publish(events.TopicTrapOrderPlaced, events.TrapFiredEvent{
		Flow:      events.FlowTrap,
		Symbol:    c.Symbol,
		OrderID:   orderID,
		Side:      c.Side,
		CloseSide: c.CloseSide,
		Price:     trapPrice,
		Volume:    c.Volume,
		TPPrice:   tpPrice,
		SLPrice:   slPrice,
		Source:    trapSourceOBMonitor,
		Timestamp: time.Now(),
	})
}

func fireStaticTrap(ctx context.Context, rt *cycle.Runtime) {
	c := rt.CandidateCopy()

	trapPrice := c.CalculateTrapPrice()
	trapVolume := c.CalculateTrapVolume(trapPrice)
	if trapPrice <= 0 || trapVolume <= 0 {
		rt.Log().Warn("🟡 Static Trap invalid, skipping",
			slog.Float64("trapPrice", trapPrice),
			slog.Float64("trapVolume", trapVolume),
		)
		reason := domain.TrapSkipReasonInvalidVolume
		if trapPrice <= 0 {
			reason = domain.TrapSkipReasonInvalidPrice
		}
		skipTrap(rt, reason, trapSourceStaticLimit)
		return
	}
	if err := rt.CycleRiskAllowsTrap(c, c.NotionalForVolume(trapVolume, trapPrice)); err != nil {
		rt.Log().Warn("🟡 Cycle risk blocked Trap", slog.Any("error", err))
		skipTrap(rt, domain.TrapSkipReasonCycleRiskBlocked, trapSourceStaticLimit)
		return
	}

	res := orders.FireLimitTrap(ctx, rt.Deps().Client, &c, rt.Deps().Clock, rt.Log())
	rt.AppendResult(res)

	if res.IsSuccess() {
		rt.MutateRecorder(func(b *domain.CycleRecordBuilder) {
			b.TrapSource = trapSourceStaticLimit
			b.TrapOutcome = domain.TrapOutcomePlaced
			b.TrapPrice = res.Price
			b.TrapOrderID = res.OrderID
			b.TrapTPPct = res.Candidate.Config.FundingTrap.TakeProfitPct
			b.TrapSLPct = res.Candidate.Config.FundingTrap.StopLossPct
			b.TrapTPPrice = res.TakeProfitPrice
			b.TrapSLPrice = res.StopLossPrice
		})

		rt.Publish(events.TopicTrapOrderPlaced, events.TrapFiredEvent{
			Flow:      events.FlowTrap,
			Symbol:    res.Candidate.Symbol,
			OrderID:   res.OrderID,
			Side:      res.Candidate.Side,
			CloseSide: res.Candidate.CloseSide,
			Price:     res.Price,
			Volume:    res.Volume,
			TPPrice:   res.TakeProfitPrice,
			SLPrice:   res.StopLossPrice,
			Source:    trapSourceStaticLimit,
			Timestamp: time.Now(),
		})
	} else if res.Error != nil {
		reason := domain.TrapSkipReasonOrderFailed
		if res.Error.Error() == "trap price <= 0" {
			reason = domain.TrapSkipReasonInvalidPrice
		}
		if res.Error.Error() == "trap volume <= 0" {
			reason = domain.TrapSkipReasonInvalidVolume
		}
		skipTrap(rt, reason, trapSourceStaticLimit)
		rt.MutateRecorder(func(b *domain.CycleRecordBuilder) {
			b.TrapError = res.Error.Error()
		})
	}
}

func skipTrap(rt *cycle.Runtime, reason domain.TrapSkipReason, source string) {
	rt.MutateRecorder(func(b *domain.CycleRecordBuilder) {
		b.TrapEnabled = true
		b.TrapOutcome = domain.TrapOutcomeSkipped
		b.TrapSkip = reason
		if source != "" {
			b.TrapSource = source
		}
	})
	rt.Publish(events.TopicTrapSkipped, events.TrapSkippedEvent{
		Flow:      events.FlowTrap,
		Symbol:    rt.Config().Symbol,
		Reason:    string(reason),
		Source:    source,
		Timestamp: time.Now(),
	})
}

func subscribeFillWatcher(ctx context.Context, rt *cycle.Runtime) {
	rt.Subscribe(ctx, events.TopicTrapOrderPlaced, func(msg *message.Message) {
		evt, err := cycle.Unmarshal[events.TrapFiredEvent](msg.Payload)
		if err != nil {
			rt.Log().Error("🔴 Unmarshal TrapFiredEvent failed", slog.Any("error", err))
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
			rt.Log().Warn("🟡 No fill", slog.String("flow", events.FlowTrap), slog.String("orderID", deal.GetOrderID()))
			return
		}

		rt.Log().Info("📊 Position opened",
			slog.String("flow", events.FlowTrap),
			slog.Float64("entry", deal.DealAvgPrice),
			slog.Float64("vol", deal.DealVol),
		)

		rt.MutateRecorder(func(b *domain.CycleRecordBuilder) {
			b.TrapFilled = true
			b.TrapOutcome = domain.TrapOutcomeFilled
			b.TrapFillPrice = deal.DealAvgPrice
			b.TrapFillVol = deal.DealVol
			b.TrapExcursion = domain.NewExcursionTracker(side, deal.DealAvgPrice)
		})
		rt.StartExcursionPriceStream(ctx)

		rt.Publish(events.TopicTrapOrderFilled, events.OrderFilledEvent{
			Flow:         events.FlowTrap,
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
	rt.Subscribe(ctx, events.TopicTrapOrderFilled, func(msg *message.Message) {
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
	trailCfg := c.Config.FundingTrap.Trailing
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

	rt.Publish(events.TopicTrapTrailingPlaced, events.TrailingPlacedEvent{
		Flow:        events.FlowTrap,
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
			slog.String("flow", events.FlowTrap),
			slog.Any("closeSide", evt.CloseSide),
			slog.Float64("vol", evt.DealVol),
		)
		if allErr := rt.Deps().Client.CloseAllPositions(closeCtx, evt.Symbol); allErr != nil {
			reason := "critical_close_failed: " + allErr.Error()
			rt.MutateRecorder(func(b *domain.CycleRecordBuilder) {
				b.AbortReason = reason
				b.AbortFlow = events.FlowTrap
				b.AbortTopic = events.TopicTrapAbort
				b.ErrorFlow = events.FlowTrap
				b.ErrorTopic = events.TopicTrapError
			})
			rt.Log().Error("🔴 CRITICAL close failed after exact-leg close failure",
				slog.Any("error", allErr),
				slog.String("symbol", evt.Symbol),
				slog.String("flow", events.FlowTrap),
			)
			rt.Publish(events.TopicTrapError, events.CycleErrorEvent{
				Flow:   events.FlowTrap,
				Symbol: evt.Symbol,
				Error:  reason,
			})
			rt.Publish(events.TopicTrapAbort, events.CycleAbortEvent{
				Flow:   events.FlowTrap,
				Symbol: evt.Symbol,
				Reason: reason,
			})
			return
		}
	}

	rt.Publish(events.TopicTrapPositionClosed, events.PositionClosedEvent{
		Flow:   events.FlowTrap,
		Symbol: evt.Symbol,
		Reason: "trailing_failed_fallback",
	})
}

func subscribeTrapOrderTimeoutGuard(ctx context.Context, rt *cycle.Runtime) {
	rt.Subscribe(ctx, events.TopicTrapOrderPlaced, func(msg *message.Message) {
		evt, err := cycle.Unmarshal[events.TrapFiredEvent](msg.Payload)
		if err != nil {
			rt.Log().Error("🔴 Unmarshal TrapFiredEvent failed", slog.Any("error", err))
			return
		}
		go handleTrapOrderTimeout(ctx, rt, evt)
	})
}

func handleTrapOrderTimeout(ctx context.Context, rt *cycle.Runtime, evt events.TrapFiredEvent) {
	timeout := time.Duration(rt.Config().FundingTrap.PostSettleTimeout)
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	startedAt := time.Now()
	rt.MutateRecorder(func(b *domain.CycleRecordBuilder) {
		b.Timeout.Flow = events.FlowTrap
		b.Timeout.Duration = timeout
		b.Timeout.DurationMs = timeout.Milliseconds()
		b.Timeout.StartedAt = startedAt
	})

	rt.Log().Info("⏱️ Trap order timeout guard started",
		slog.String("orderID", evt.OrderID),
		slog.Duration("timeout", timeout),
	)

	if err := rt.Deps().Clock.Sleep(ctx, timeout); err != nil {
		return
	}
	trapFilled := false
	trapDone := false
	rt.MutateRecorder(func(b *domain.CycleRecordBuilder) {
		trapFilled = b.TrapFilled && b.TrapOrderID == evt.OrderID
		trapDone = OutcomeTerminal(b.TrapOutcome)
	})
	if trapFilled || trapDone {
		return
	}

	firedAt := time.Now()
	rt.MutateRecorder(func(b *domain.CycleRecordBuilder) {
		b.Timeout.Flow = events.FlowTrap
		b.Timeout.Triggered = true
		b.Timeout.FiredAt = firedAt
	})

	cancelCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	if err := rt.Deps().Client.CancelOrder(cancelCtx, evt.Symbol, evt.OrderID); err != nil {
		rt.Log().Error("🔴 Trap order cancel failed - canceling all open orders", slog.Any("error", err))
		if allErr := rt.Deps().Client.CancelAllOpenOrders(cancelCtx, evt.Symbol); allErr != nil {
			reason := "critical_trap_cancel_failed: " + allErr.Error()
			rt.MutateRecorder(func(b *domain.CycleRecordBuilder) {
				b.AbortReason = reason
				b.AbortFlow = events.FlowTrap
				b.AbortTopic = events.TopicTrapAbort
				b.ErrorFlow = events.FlowTrap
				b.ErrorTopic = events.TopicTrapError
				b.TrapOutcome = domain.TrapOutcomeAborted
				b.Timeout.Error = allErr.Error()
			})
			rt.Publish(events.TopicTrapError, events.CycleErrorEvent{
				Flow:   events.FlowTrap,
				Symbol: evt.Symbol,
				Error:  reason,
			})
			rt.Publish(events.TopicTrapAbort, events.CycleAbortEvent{
				Flow:   events.FlowTrap,
				Symbol: evt.Symbol,
				Reason: reason,
			})
			return
		}
	}

	rt.MutateRecorder(func(b *domain.CycleRecordBuilder) {
		b.TrapOutcome = domain.TrapOutcomeTimeout
	})
	rt.Publish(events.TopicTrapTimeout, events.CycleTimeoutEvent{
		Flow:    events.FlowTrap,
		Symbol:  evt.Symbol,
		Timeout: timeout,
	})
}

func watchTrapBranchTerminal(ctx context.Context, rt *cycle.Runtime) {
	rt.Subscribe(ctx, events.TopicTrapPositionClosed, func(msg *message.Message) {
		if evt, parseErr := cycle.Unmarshal[events.PositionClosedEvent](msg.Payload); parseErr == nil && evt.Flow != events.FlowTrap {
			return
		}
		rt.MutateRecorder(func(b *domain.CycleRecordBuilder) {
			b.TrapOutcome = domain.TrapOutcomeClosed
		})
	})
	rt.Subscribe(ctx, events.TopicTrapTimeout, func(msg *message.Message) {
		if evt, parseErr := cycle.Unmarshal[events.CycleTimeoutEvent](msg.Payload); parseErr == nil && evt.Flow != events.FlowTrap {
			return
		}
		rt.MutateRecorder(func(b *domain.CycleRecordBuilder) {
			b.TrapOutcome = domain.TrapOutcomeTimeout
		})
	})
}

func OutcomeTerminal(outcome domain.TrapOutcome) bool {
	switch outcome {
	case domain.TrapOutcomeClosed, domain.TrapOutcomeTimeout, domain.TrapOutcomeAborted, domain.TrapOutcomeSkipped:
		return true
	default:
		return false
	}
}
