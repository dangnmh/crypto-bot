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

	"github.com/ThreeDotsLabs/watermill/message"
)

const trapSourceOBMonitor = "ob_monitor"
const trapSourceStaticLimit = "static_limit"

func subscribeFireTrap(ctx context.Context, rt *cycle.Runtime) {
	rt.Subscribe(ctx, events.TopicTrapCandidate, func(_ *message.Message) {
		cfg := rt.Config()
		if !cfg.IsHedgeTrapEnabled() {
			return
		}
		go handleFireTrap(ctx, rt)
	})
}

func handleFireTrap(ctx context.Context, rt *cycle.Runtime) {
	// Get settle time from the cycle start event
	envelopes := rt.JourneyEvents()
	var settleTime time.Time
	for i := range envelopes {
		if envelopes[i].Topic == events.TopicCycleStarted {
			if startEvent, err := cycle.Unmarshal[events.CycleStartEvent](envelopes[i].Payload); err == nil {
				settleTime = startEvent.SettleTime
				break
			}
		}
	}
	if settleTime.IsZero() {
		rt.Log().Error("Settle time not found, skipping trap fire")
		return
	}

	cfg := rt.Config()
	delay := time.Duration(cfg.FundingTrap.TrapAfterSettle)
	if delay <= 0 {
		delay = 3 * time.Second
	}
	trapTime := settleTime.Add(delay)

	rt.Log().Info("Fire Trap waiting", slog.Duration("delay", delay), slog.Time("target_time", trapTime))
	rt.WaitUntil(ctx, trapTime)
	if ctx.Err() != nil {
		return
	}

	ob, err := rt.Deps().DepthStore.GetDepth(ctx, cfg.Symbol)
	if err != nil || ob == nil {
		rt.Log().Warn("Fire Trap: failed to fetch depth, falling back to static trap", slog.Any("error", err))
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
		rt.Log().Info("Fire Trap: no suitable wall found, falling back to static trap", slog.String("side", trapSide.String()))
		fireStaticTrap(ctx, rt)
		return
	}

	wallFoundAt := time.Now()
	verifiedWall, ok := verifyTrapWall(ctx, rt, originalCandidate, wallPrice, wallFoundAt)
	if !ok {
		rt.Log().Warn("Fire Trap: wall disappeared before placement, skipping OB trap",
			slog.String("side", trapSide.String()),
			slog.Float64("initial_wall_price", wallPrice),
		)
		reqID := rt.GetReqID()
		rt.RecordAndPublish(reqID, events.TopicTrapWallVerified, events.TrapWallVerifiedEvent{
			Flow:            events.FlowTrap,
			Symbol:          cfg.Symbol,
			WallPrice:       wallPrice,
			WallVerified:    false,
			WallAgeMs:       time.Since(wallFoundAt).Milliseconds(),
			WallDistancePct: originalCandidate.TrapWallDistancePct(wallPrice),
			Side:            trapSide,
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
	reqID := rt.GetReqID()
	rt.RecordAndPublish(reqID, events.TopicTrapWallVerified, events.TrapWallVerifiedEvent{
		Flow:            events.FlowTrap,
		Symbol:          cfg.Symbol,
		WallPrice:       verifiedWall.price,
		WallVerified:    true,
		WallAgeMs:       verifiedWall.ageMs,
		WallDistancePct: verifiedWall.distancePct,
		Side:            trapSide,
	})

	rt.Log().Info("OB Monitor: wall found",
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
		rt.Log().Warn("Fire Trap: failed to verify wall", slog.Any("error", err))
		return wallVerification{}, false
	}

	freshWallPrice := c.FindTrapWallPrice(freshOB)
	if freshWallPrice <= 0 {
		return wallVerification{}, false
	}

	if c.PriceUnit > 0 && math.Abs(freshWallPrice-initialWallPrice) > c.PriceUnit {
		rt.Log().Info("Fire Trap: wall moved during verification",
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

type wallVerification struct {
	price       float64
	trapPrice   float64
	ageMs       int64
	distancePct float64
}

func fireOBTrap(ctx context.Context, rt *cycle.Runtime, c domain.Candidate, wall wallVerification) {
	trapPrice := wall.trapPrice
	c.Volume = c.CalculateTrapVolume(trapPrice)
	if c.Volume <= 0 {
		rt.Log().Warn("TRAP volume invalid, skipping", slog.String("symbol", c.Symbol))
		skipTrap(rt, domain.TrapSkipReasonInvalidVolume, trapSourceOBMonitor)
		return
	}
	if err := rt.CycleRiskAllowsTrap(c, c.NotionalForVolume(c.Volume, trapPrice)); err != nil {
		rt.Log().Warn("Cycle risk blocked Trap", slog.Any("error", err))
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

	rt.Log().Info("FIRE OB TRAP",
		slog.String("symbol", c.Symbol),
		slog.Float64("trapPrice", trapPrice),
		slog.Float64("vol", c.Volume),
		slog.String("side", c.Side.String()),
		slog.Float64("takeProfitPrice", tpPrice),
		slog.Float64("stopLossPrice", slPrice),
	)

	orderID, err := rt.Deps().Client.CreateOrder(ctx, req)
	if err != nil {
		rt.Log().Error("TRAP order failed", slog.Any("error", err))
		skipTrapWithError(rt, domain.TrapSkipReasonOrderFailed, trapSourceOBMonitor, err.Error())
		return
	}

	rt.Log().Info("TRAP submitted", slog.String("orderID", orderID))
	reqID := rt.GetReqID()
	rt.RecordAndPublish(reqID, events.TopicTrapOrderSubmitted, events.TrapOrderSubmittedEvent{
		Flow:            events.FlowTrap,
		Symbol:          c.Symbol,
		Source:          trapSourceOBMonitor,
		OrderID:         orderID,
		Side:            c.Side,
		CloseSide:       c.CloseSide,
		Price:           trapPrice,
		Volume:          c.Volume,
		TPPrice:         tpPrice,
		SLPrice:         slPrice,
		TPPct:           c.Config.FundingTrap.TakeProfitPct,
		SLPct:           c.Config.FundingTrap.StopLossPct,
		WallPrice:       wall.price,
		WallVerified:    true,
		WallAgeMs:       wall.ageMs,
		WallDistancePct: wall.distancePct,
	})

	placed := events.TrapFiredEvent{
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
	}
	rt.MarkTrapOrder(placed)
	rt.RecordAndPublish(reqID, events.TopicTrapOrderPlaced, placed)
}

func fireStaticTrap(ctx context.Context, rt *cycle.Runtime) {
	c := rt.CandidateCopy()

	trapPrice := c.CalculateTrapPrice()
	trapVolume := c.CalculateTrapVolume(trapPrice)
	if trapPrice <= 0 || trapVolume <= 0 {
		rt.Log().Warn("Static Trap invalid, skipping",
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
		rt.Log().Warn("Cycle risk blocked Trap", slog.Any("error", err))
		skipTrap(rt, domain.TrapSkipReasonCycleRiskBlocked, trapSourceStaticLimit)
		return
	}

	res := orders.FireLimitTrap(ctx, rt.Deps().Client, &c, rt.Deps().Clock, rt.Log())
	rt.AppendResult(res)

	if res.IsSuccess() {
		reqID := rt.GetReqID()
		rt.RecordAndPublish(reqID, events.TopicTrapOrderSubmitted, events.TrapOrderSubmittedEvent{
			Flow:      events.FlowTrap,
			Symbol:    res.Candidate.Symbol,
			Source:    trapSourceStaticLimit,
			OrderID:   res.OrderID,
			Side:      res.Candidate.Side,
			CloseSide: res.Candidate.CloseSide,
			Price:     res.Price,
			Volume:    res.Volume,
			TPPrice:   res.TakeProfitPrice,
			SLPrice:   res.StopLossPrice,
			TPPct:     res.Candidate.Config.FundingTrap.TakeProfitPct,
			SLPct:     res.Candidate.Config.FundingTrap.StopLossPct,
		})

		placed := events.TrapFiredEvent{
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
		}
		rt.MarkTrapOrder(placed)
		rt.RecordAndPublish(reqID, events.TopicTrapOrderPlaced, placed)
	} else if res.Error != nil {
		reason := domain.TrapSkipReasonOrderFailed
		if res.Error.Error() == "trap price <= 0" {
			reason = domain.TrapSkipReasonInvalidPrice
		}
		if res.Error.Error() == "trap volume <= 0" {
			reason = domain.TrapSkipReasonInvalidVolume
		}
		skipTrapWithError(rt, reason, trapSourceStaticLimit, res.Error.Error())
	}
}

func skipTrap(rt *cycle.Runtime, reason domain.TrapSkipReason, source string) {
	skipTrapWithError(rt, reason, source, "")
}

func skipTrapWithError(rt *cycle.Runtime, reason domain.TrapSkipReason, source, errText string) {
	if !rt.TryMarkFlowTerminal(events.FlowTrap) {
		return
	}
	rt.MarkTrapTerminal()
	reqID := rt.GetReqID()
	rt.RecordAndPublish(reqID, events.TopicTrapSkipped, events.TrapSkippedEvent{
		Flow:      events.FlowTrap,
		Symbol:    rt.Config().Symbol,
		Reason:    string(reason),
		Source:    source,
		Error:     errText,
		Timestamp: time.Now(),
	})
}
